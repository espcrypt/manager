package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"espcrypt/internal/espcrypt"
)

func (a *App) ConnectVaultDevice(port string) error {
	a.deviceSwitchMu.Lock()
	defer a.deviceSwitchMu.Unlock()
	if strings.TrimSpace(port) == "" {
		return errors.New("choose a serial port")
	}
	a.mu.Lock()
	delete(a.userDisconnected, strings.ToUpper(port))
	a.mu.Unlock()
	if _, registered := a.vaultDeviceForPort(port); registered {
		return nil
	}
	dev := &espcrypt.Device{Port: port}
	status, err := dev.Status()
	if err != nil {
		dev.Close()
		a.logf("error", "Custom Vault connection failed: %v", err)
		return err
	}
	if !espcrypt.SupportedChip(status.Chip) {
		dev.Close()
		return errors.New("the connected firmware is not the custom ESP Crypt firmware")
	}
	return a.registerVaultDevice(dev, status)
}

func (a *App) registerVaultDevice(dev *espcrypt.Device, status espcrypt.DeviceStatus) error {
	port := dev.Port
	id := vaultDeviceID(status, port)
	label := status.Chip
	serial := status.DeviceID
	if serial == "" {
		serial = status.Serial
	}
	if serial != "" {
		label += " · " + serial
	} else {
		label += " · " + port
	}
	a.mu.Lock()
	if a.vaultdevs == nil {
		a.vaultdevs = make(map[string]*vaultDeviceConnection)
	}
	if existing := a.vaultdevs[id]; existing != nil {

		if isRemotePort(existing.Info.Port) && !isRemotePort(port) {
			delete(a.vaultdevs, id)
			a.mu.Unlock()
			existing.Device.Retire()
			a.logf("info", "%s is on USB again; using USB instead of %s", label, transportOf(existing.Info.Port))
			a.mu.Lock()
		} else {
			a.mu.Unlock()
			dev.Close()
			if existing.Info.Port != port {
				return fmt.Errorf("this ESP Crypt is already registered on %s", existing.Info.Port)
			}
			return nil
		}
	}
	entry := &vaultDeviceConnection{Info: VaultDeviceInfo{ID: id, Label: label, Port: port, Chip: status.Chip, Serial: serial,
		Transport: transportOf(port)}, Device: dev}
	a.vaultdevs[id] = entry

	selected := false

	if a.activeVaultID == "" && !isRemotePort(port) {
		a.activeVaultID = id
		if a.settings.ActiveVaultID == "" || a.settings.ActiveVaultID == id {
			a.settings.ActiveVaultID = id
		}
		selected = true
	}
	a.mu.Unlock()
	if selected {
		_ = a.persistSettings()
	}
	a.logf("info", "Registered %s on %s", label, port)
	runtime.EventsEmit(a.ctx, "devices:changed", nil)
	runtime.EventsEmit(a.ctx, "device:changed", selected)
	return nil
}

func vaultDeviceID(status espcrypt.DeviceStatus, port string) string {
	chip := strings.ToUpper(strings.TrimSpace(status.Chip))
	serial := strings.ToUpper(strings.TrimSpace(status.DeviceID))
	if serial == "" {
		serial = strings.ToUpper(strings.TrimSpace(status.Serial))
	}
	if serial == "" {
		return "port:" + strings.ToUpper(strings.TrimSpace(port))
	}
	return chip + ":" + serial
}

func (a *App) ListVaultDevices() []VaultDeviceInfo {
	a.mu.Lock()
	devices := make([]VaultDeviceInfo, 0, len(a.vaultdevs))
	entries := make([]*vaultDeviceConnection, 0, len(a.vaultdevs))
	secondID := ""
	if a.settings.KeyMode == 2 {
		if s := a.secondVaultLocked(); s != nil {
			secondID = s.Info.ID
		}
	}
	for id, entry := range a.vaultdevs {
		info := entry.Info
		info.Active = id == a.activeVaultID
		info.Second = id == secondID
		info.Key = info.serialKey()
		devices = append(devices, info)
		entries = append(entries, entry)
	}
	a.mu.Unlock()
	for i, entry := range entries {
		if st, err := entry.Device.CachedStatus(5 * time.Second); err == nil {
			devices[i].Unlocked = st.Unlocked
		}
	}
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].Active != devices[j].Active {
			return devices[i].Active
		}
		return devices[i].Label < devices[j].Label
	})
	return devices
}

func (a *App) SetActiveVaultDevice(id string) error {
	a.deviceSwitchMu.Lock()
	defer a.deviceSwitchMu.Unlock()
	a.mu.Lock()
	oldID := a.activeVaultID
	var target *vaultDeviceConnection
	if id != "" {
		target = a.vaultdevs[id]
		if target == nil {
			a.mu.Unlock()
			return errors.New("select a registered ESP Crypt")
		}
		if isRemotePort(target.Info.Port) {
			a.mu.Unlock()
			return errors.New("this ESP32 is connected over Wi-Fi for mirror sync only; plug it in by USB to use it")
		}
	}
	a.mu.Unlock()
	if oldID == id {
		return nil
	}
	if target != nil {
		status, err := target.Device.CachedStatus(5 * time.Second)
		if err != nil {
			return fmt.Errorf("selected ESP Crypt is not responding: %w", err)
		}
		if !espcrypt.SupportedChip(status.Chip) {
			return errors.New("selected port is no longer running custom ESP Crypt firmware")
		}
	}

	a.mu.Lock()
	if a.activeVaultID != oldID || (id != "" && a.vaultdevs[id] != target) {
		a.mu.Unlock()
		return errors.New("the selected ESP Crypt changed; retry the switch")
	}
	a.activeVaultID = id
	a.settings.ActiveVaultID = id
	a.mu.Unlock()
	if err := a.persistSettings(); err != nil {
		a.logf("error", "Could not save the active ESP Crypt: %v", err)
	}
	if target == nil {
		a.logf("info", "No active ESP Crypt selected")
	} else {
		a.logf("info", "Active ESP Crypt is now %s on %s", target.Info.Label, target.Info.Port)
	}
	runtime.EventsEmit(a.ctx, "devices:changed", nil)
	runtime.EventsEmit(a.ctx, "device:changed", target != nil)
	return nil
}

func (a *App) DisconnectVaultDevice(id string) error {
	a.mu.Lock()
	entry := a.vaultdevs[id]
	a.mu.Unlock()
	if err := a.disconnectVaultDevice(id); err != nil {
		return err
	}
	if entry != nil {
		a.mu.Lock()
		a.userDisconnected[strings.ToUpper(entry.Info.Port)] = true
		a.mu.Unlock()
	}
	return nil
}

func (a *App) disconnectVaultDevice(id string) error {
	a.deviceSwitchMu.Lock()
	defer a.deviceSwitchMu.Unlock()
	a.mu.Lock()
	entry := a.vaultdevs[id]
	active := id == a.activeVaultID
	a.mu.Unlock()
	if entry == nil {
		return nil
	}
	if active {
		if err := a.LockAllVaults(); err != nil {
			return fmt.Errorf("close mounted vaults before disconnecting: %w", err)
		}
	} else if _, err := a.vm.LockForDevice(a.ctxOrBg(), entry.Info.serialKey()); err != nil {
		return fmt.Errorf("close the vaults that use %s before disconnecting: %w", entry.Info.Label, err)
	}
	if err := entry.Device.Lock(); err != nil {
		return fmt.Errorf("could not lock ESP Crypt: %w", err)
	}
	a.mu.Lock()
	if a.vaultdevs[id] == entry {
		delete(a.vaultdevs, id)
		if a.activeVaultID == id {
			a.activeVaultID = ""
		}
	}
	a.mu.Unlock()
	entry.Device.Retire()
	a.logf("info", "Disconnected %s", entry.Info.Label)
	runtime.EventsEmit(a.ctx, "devices:changed", nil)
	runtime.EventsEmit(a.ctx, "device:changed", false)
	return nil
}

func (a *App) vaultDeviceForPort(port string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, entry := range a.vaultdevs {
		if strings.EqualFold(entry.Info.Port, port) {
			return id, true
		}
	}
	return "", false
}

func (a *App) InitializeVaultStorage() error {
	a.mu.Lock()
	entry := a.vaultdevs[a.activeVaultID]
	a.mu.Unlock()
	if entry == nil {
		return errors.New("select a connected custom ESP Crypt first")
	}
	if err := entry.Device.InitializeStorage(); err != nil {
		return err
	}
	a.logf("info", "ESP Crypt NVS is ready")
	runtime.EventsEmit(a.ctx, "device:changed", true)
	return nil
}

func (a *App) SetupVaultPIN(pin string) error {
	if len(pin) < 4 || len(pin) > 64 {
		return errors.New("Vault PIN or passphrase must be 4 to 64 bytes")
	}
	a.mu.Lock()
	entry := a.vaultdevs[a.activeVaultID]
	a.mu.Unlock()
	if entry == nil {
		return errors.New("select a connected custom ESP Crypt first")
	}
	status, err := entry.Device.Status()
	if err != nil {
		return err
	}
	if !status.StorageReady {
		return errors.New("initialize Vault storage first")
	}
	if err := entry.Device.SetupPIN(pin); err != nil {
		a.logf("error", "Vault passphrase setup failed: %v", err)
		return err
	}
	a.logf("warn", "Custom Vault passphrase set; keyfile storage is initialized")
	runtime.EventsEmit(a.ctx, "device:changed", true)
	return nil
}
