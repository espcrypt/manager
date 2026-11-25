package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"espcrypt/internal/espcrypt"
	"espcrypt/internal/sshagent"
	"espcrypt/internal/vault"
	"espcrypt/internal/veracrypt"
)

type Settings struct {
	RememberPIN    bool   `json:"rememberPin"`
	ConfirmSign    bool   `json:"confirmSign"`
	AgentAutostart bool   `json:"agentAutostart"`
	AutoConnect    bool   `json:"autoConnect"`
	LastReader     string `json:"lastReader"`
	ActiveVaultID  string `json:"activeVaultDevice"`

	KeyMode         int           `json:"keyMode"`
	SecondVaultID   string        `json:"secondVaultDevice"`
	MountBase       string        `json:"mountBase"`
	Vaults          []vault.Vault `json:"vaults,omitempty"`
	LockOnRemove    bool          `json:"lockOnRemove"`
	OpenAfterUnlock bool          `json:"openAfterUnlock"`
	NotifyOnConnect bool          `json:"notifyOnConnect"`
	RunInBackground bool          `json:"runInBackground"`
	NoAutoUpdate    bool          `json:"noAutoUpdate"`

	Mirror MirrorConfig `json:"mirror"`

	SSHKeys map[string][]cachedSSHKey `json:"sshKeys,omitempty"`

	Wireless map[string]WirelessPairing `json:"wireless,omitempty"`
}

type LogEntry struct {
	Time  string `json:"time"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

type Status struct {
	Connected         bool               `json:"connected"`
	DeviceType        string             `json:"deviceType"`
	SecureBoot        bool               `json:"secureBoot"`
	FlashEncrypted    bool               `json:"flashEncrypted"`
	VaultStorageReady bool               `json:"vaultStorageReady"`
	Port              string             `json:"port"`
	Serial            string             `json:"serial"`
	PIN               espcrypt.PinStatus `json:"pin"`
	PINCached         bool               `json:"pinCached"`
	Agent             AgentStatus        `json:"agent"`

	Passkey bool             `json:"passkey"`
	Storage espcrypt.Storage `json:"storage"`

	FirmwareVersion string `json:"firmwareVersion"`
	FirmwareBuilt   string `json:"firmwareBuilt"`

	Transport string `json:"transport"`
}

type VaultDeviceInfo struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Port   string `json:"port"`
	Chip   string `json:"chip"`
	Serial string `json:"serial"`
	Active bool   `json:"active"`

	Second   bool `json:"second"`
	Unlocked bool `json:"unlocked"`

	Key string `json:"key"`

	Transport string `json:"transport"`
}

func (i VaultDeviceInfo) serialKey() string {
	if i.Serial == "" {
		return ""
	}
	return i.Chip + " " + i.Serial
}

type vaultDeviceConnection struct {
	Info   VaultDeviceInfo
	Device *espcrypt.Device
}

type App struct {
	ctx context.Context

	mu             sync.Mutex
	deviceSwitchMu sync.Mutex
	vaultdevs      map[string]*vaultDeviceConnection
	activeVaultID  string

	userDisconnected map[string]bool
	settings         Settings

	logMu sync.Mutex
	logs  []LogEntry

	vm          *vault.Manager
	pendingOpen string
	agent       *sshagent.Server

	notifyReady atomic.Bool
	romBusy     atomic.Int32
}

func NewApp() *App {
	a := &App{
		vaultdevs:        make(map[string]*vaultDeviceConnection),
		userDisconnected: make(map[string]bool),
		settings: Settings{ConfirmSign: false, AutoConnect: true, RememberPIN: true, LockOnRemove: true, OpenAfterUnlock: true,
			NotifyOnConnect: true, RunInBackground: true}}
	a.vm = a.newVaultManager()
	return a
}

func configDir() string { return vault.ConfigDir() }

func (a *App) loadSettings() {
	paths := []string{
		filepath.Join(configDir(), "settings.json"),
		filepath.Join(filepath.Dir(configDir()), "PicoHSM"+" Manager", "settings.json"),
	}
	for _, path := range paths {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &a.settings)
			return
		}
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if len(a.settings.Vaults) > 0 {
		a.vm.Migrate(a.settings.Vaults)
		a.mu.Lock()
		a.settings.Vaults = nil
		a.mu.Unlock()
		_ = a.persistSettings()
	}
	a.logf("info", "ESP Crypt started")
	go func() {
		if n := veracrypt.CleanupStaleMounts(); n > 0 {
			a.logf("info", "[Vault] Cleaned up %d stale VeraCrypt helper mount(s)", n)
		}
	}()
	go a.initNotifications()
	go a.autoConnectLoop()
	go a.remoteLoop()
	go a.statusLoop()
	go a.updateLoop()
	go func() { _, _, _ = firmwareDir() }()
	if a.GetSettings().AgentAutostart {
		if err := a.StartAgent(); err != nil {
			a.logf("error", "SSH agent could not start: %v", err)
		}
	}
}

func (a *App) shutdown(context.Context) {
	if err := a.LockAllVaults(); err != nil {
		a.logf("error", "Could not lock open vaults during shutdown: %v", err)
	}
	a.StopAgent()
	a.mu.Lock()
	devices := make([]*espcrypt.Device, 0, len(a.vaultdevs))
	for _, entry := range a.vaultdevs {
		devices = append(devices, entry.Device)
	}
	a.vaultdevs = make(map[string]*vaultDeviceConnection)
	a.activeVaultID = ""
	a.mu.Unlock()
	for _, device := range devices {
		_ = device.Lock()
		device.Close()
	}
}

func (a *App) logf(level, format string, args ...any) {
	e := LogEntry{Time: time.Now().Format("15:04:05"), Level: level, Msg: fmt.Sprintf(format, args...)}
	a.logMu.Lock()
	a.logs = append(a.logs, e)
	if len(a.logs) > 1000 {
		a.logs = a.logs[len(a.logs)-1000:]
	}
	a.logMu.Unlock()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "log", e)
	}
}

func (a *App) progress(task string, done, total int, text string) {
	runtime.EventsEmit(a.ctx, "progress", map[string]any{"task": task, "done": done, "total": total, "text": text})
}

func (a *App) GetLogs() []LogEntry {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	return append([]LogEntry(nil), a.logs...)
}

func (a *App) ClearLogs() {
	a.logMu.Lock()
	a.logs = nil
	a.logMu.Unlock()
}

func (a *App) GetSettings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

func (a *App) SaveSettings(s Settings) error {
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
	return a.persistSettings()
}

func (a *App) persistSettings() error {
	a.mu.Lock()
	b, _ := json.MarshalIndent(a.settings, "", "  ")
	a.mu.Unlock()
	_ = os.MkdirAll(configDir(), 0o700)
	return os.WriteFile(filepath.Join(configDir(), "settings.json"), b, 0o600)
}

func (a *App) CopyText(s string) error {
	return runtime.ClipboardSetText(a.ctx, s)
}

func (a *App) keyStore() (vault.KeyStore, error) {
	a.mu.Lock()
	entry := a.vaultdevs[a.activeVaultID]
	a.mu.Unlock()
	if entry == nil {
		return nil, errors.New("select a connected custom ESP Crypt")
	}
	status, err := entry.Device.Status()
	if err != nil {
		return nil, err
	}
	if !status.StorageReady {
		return nil, errors.New("Vault storage is not initialized")
	}
	return entry.Device, nil
}

func (a *App) Disconnect() {
	a.mu.Lock()
	id := a.activeVaultID
	a.mu.Unlock()
	if id != "" {
		if err := a.DisconnectVaultDevice(id); err != nil {
			a.logf("error", "Could not disconnect ESP Crypt: %v", err)
		}
	}
}

func (a *App) GetStatus() Status {
	st := Status{Agent: a.AgentStatus()}
	a.mu.Lock()
	id := a.activeVaultID
	entry := a.vaultdevs[id]
	port := ""
	if entry != nil {
		port = entry.Info.Port
	}
	a.mu.Unlock()
	if entry != nil && a.romBusy.Load() != 0 {

		st.Connected, st.Port = true, port
		return st
	}
	if entry != nil {

		cs, err := entry.Device.CachedStatus(5 * time.Second)
		if err != nil {
			st.Connected, st.Port, st.Transport = true, port, transportOf(port)
			return st
		}
		st.Connected = true
		st.DeviceType = strings.ToLower(strings.ReplaceAll(cs.Chip, "-", "")) + "-vault"
		st.Port = port
		st.Transport = transportOf(port)
		serial := cs.DeviceID
		if serial == "" {
			serial = cs.Serial
		}
		st.Serial = cs.Chip
		if serial != "" {
			st.Serial += " " + serial
		}
		st.SecureBoot = cs.SecureBoot
		st.FlashEncrypted = cs.FlashEncrypted
		st.VaultStorageReady = cs.StorageReady
		st.Passkey = cs.Passkey
		st.Storage = cs.Storage
		st.FirmwareVersion, st.FirmwareBuilt = cs.Version, cs.Built
		st.PIN = espcrypt.PinStatusFrom(cs, nil)
		st.PINCached = false
		a.mu.Lock()
		if a.vaultdevs[id] == entry {
			entry.Info.Chip = cs.Chip
			entry.Info.Serial = serial
			entry.Info.Label = cs.Chip + " · " + serial
		}
		a.mu.Unlock()
		return st
	}
	return st
}

func (a *App) Login(pin string) error {
	a.mu.Lock()
	entry := a.vaultdevs[a.activeVaultID]
	a.mu.Unlock()
	if entry != nil {
		status, err := entry.Device.Status()
		if err != nil {
			return err
		}
		if !status.StorageReady {
			return errors.New("initialize Vault storage before setting or checking the passphrase")
		}
		if ps := espcrypt.PinStatusFrom(status, nil); ps.NotInitialized {
			return errors.New("set a passphrase for the custom ESP Crypt first")
		}
		if err := entry.Device.Unlock(pin); err != nil {
			a.logf("error", "Custom Vault unlock failed: %v", err)
			return err
		}
		a.rememberPIN(entry.Info.Serial, pin)
		a.logf("info", "Custom Vault unlocked")
		return nil
	}
	return errors.New("connect the custom ESP Crypt firmware first")
}

func (a *App) Logout() error {
	a.mu.Lock()
	entry := a.vaultdevs[a.activeVaultID]
	a.mu.Unlock()
	if entry != nil {

		if err := a.LockAllVaults(); err != nil {
			return fmt.Errorf("close the open vaults first (files may be in use): %w", err)
		}
		a.forgetPIN(entry.Info.Serial)
		if err := entry.Device.Lock(); err != nil {
			return err
		}
		a.logf("info", "Custom Vault locked")
		return nil
	}
	return errors.New("no custom ESP Crypt connected")
}

func (a *App) ChangePIN(oldPIN, newPIN string) error {
	a.mu.Lock()
	entry := a.vaultdevs[a.activeVaultID]
	a.mu.Unlock()
	if entry != nil {
		if len(newPIN) < 4 || len(newPIN) > 64 {
			return errors.New("Vault PIN or passphrase must be 4 to 64 bytes")
		}
		if err := entry.Device.ChangePIN(oldPIN, newPIN); err != nil {
			return err
		}
		a.logf("info", "Custom Vault passphrase changed")
		return nil
	}
	return errors.New("no custom ESP Crypt connected")
}
