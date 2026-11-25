package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"espcrypt/internal/espcrypt"
)

type MirrorConfig struct {
	Main   string `json:"main"`
	Backup string `json:"backup"`

	Rev        uint32 `json:"rev"`
	PasskeyRev uint32 `json:"passkeyRev"`

	Hash string `json:"hash"`

	PasskeysPending bool   `json:"passkeysPending"`
	PasskeyFrom     string `json:"passkeyFrom"`
	LastSync        string `json:"lastSync"`
}

type MirrorBoard struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Connected  bool   `json:"connected"`
	Unlocked   bool   `json:"unlocked"`
	Rev        uint32 `json:"rev"`
	PasskeyRev uint32 `json:"passkeyRev"`
	Passkey    bool   `json:"passkey"`

	Transport string `json:"transport"`
	Hash      string `json:"hash"`
}

type MirrorStatus struct {
	Configured bool        `json:"configured"`
	Main       MirrorBoard `json:"main"`
	Backup     MirrorBoard `json:"backup"`

	State    string `json:"state"`
	Message  string `json:"message"`
	LastSync string `json:"lastSync"`

	Error string `json:"error"`
}

var mirrorErr struct {
	sync.Mutex
	msg string
}

func (a *App) setMirrorError(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	mirrorErr.Lock()
	changed := mirrorErr.msg != msg
	mirrorErr.msg = msg
	mirrorErr.Unlock()
	if changed && msg != "" {
		a.logf("error", "[Mirror] %s", msg)
	}
}

var mirrorMu sync.Mutex

func containerSide(c MirrorConfig, mRev, bRev uint32, mHash, bHash string) string {
	if mHash != "" && bHash != "" {
		if mHash == bHash {
			return ""
		}
		mc, bc := mHash != c.Hash, bHash != c.Hash
		switch {
		case c.Hash == "":
		case mc && !bc:
			return "main"
		case bc && !mc:
			return "backup"
		default:
			return "both"
		}
	}
	if mRev == bRev {
		if mHash != "" && bHash != "" && mHash != bHash {
			return "both"
		}
		return ""
	}
	switch {
	case mRev != c.Rev && bRev == c.Rev:
		return "main"
	case bRev != c.Rev && mRev == c.Rev:
		return "backup"
	}
	return "both"
}

func (a *App) mirrorConfig() MirrorConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings.Mirror
}

func (a *App) setMirrorConfig(c MirrorConfig) {
	a.mu.Lock()
	a.settings.Mirror = c
	a.mu.Unlock()
	_ = a.persistSettings()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "mirror:changed", nil)
	}
}

func (a *App) vaultEntry(id string) *vaultDeviceConnection {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.vaultdevs[id]
}

func boardStatus(id string, e *vaultDeviceConnection) (MirrorBoard, espcrypt.DeviceStatus, error) {
	b := MirrorBoard{ID: id, Label: id}
	if e == nil {
		return b, espcrypt.DeviceStatus{}, errors.New("not connected")
	}
	b.Label = e.Info.Label
	b.Transport = transportOf(e.Info.Port)
	st, err := e.Device.CachedStatus(5 * time.Second)
	if err != nil {
		return b, st, err
	}
	b.Connected, b.Unlocked, b.Rev, b.PasskeyRev, b.Passkey = true, st.Unlocked, st.Rev, st.PasskeyRev, st.Passkey
	if !st.Busy {
		b.Hash = st.ContainerHash
	}
	return b, st, nil
}

func (a *App) GetMirrorStatus() MirrorStatus {
	c := a.mirrorConfig()
	s := MirrorStatus{Configured: c.Main != "" && c.Backup != "", LastSync: c.LastSync}
	mirrorErr.Lock()
	s.Error = mirrorErr.msg
	mirrorErr.Unlock()
	if !s.Configured {
		return s
	}
	var mainErr, backupErr error
	s.Main, _, mainErr = boardStatus(c.Main, a.vaultEntry(c.Main))
	s.Backup, _, backupErr = boardStatus(c.Backup, a.vaultEntry(c.Backup))
	switch {
	case mainErr != nil || backupErr != nil:
		s.State = "waiting"
		missing := "the backup ESP32"
		if mainErr != nil {
			missing = "the main ESP32"
		}
		s.Message = "Connect " + missing + " by USB, or put it on the same Wi-Fi with Mirror over Wi-Fi turned on. Changes are copied as soon as both are reachable."
	case containerSide(c, s.Main.Rev, s.Backup.Rev, s.Main.Hash, s.Backup.Hash) == "both":
		s.State, s.Message = "conflict", "Both ESP32s changed since the last sync. Choose which one to keep."
	case containerSide(c, s.Main.Rev, s.Backup.Rev, s.Main.Hash, s.Backup.Hash) != "":
		s.State, s.Message = "sync", "Syncing…"
	case s.Main.Passkey && s.Backup.Passkey && (c.PasskeysPending || s.Main.PasskeyRev != s.Backup.PasskeyRev) &&
		!(s.Main.Unlocked && s.Backup.Unlocked):
		s.State, s.Message = "unlock", "Unlock both ESP32s (same passphrase) to sync the passkeys."
	case s.Main.Passkey && s.Backup.Passkey && s.Main.PasskeyRev != s.Backup.PasskeyRev &&
		s.Main.PasskeyRev != c.PasskeyRev && s.Backup.PasskeyRev != c.PasskeyRev && !c.PasskeysPending:
		s.State, s.Message = "conflict", "The passkeys changed on both ESP32s since the last sync. Choose which one to keep."
	default:
		s.State, s.Message = "sync", "In sync"
	}
	if s.Error != "" && s.State != "waiting" && s.Message != "In sync" {
		s.State, s.Message = "error", "Sync failed: "+s.Error
	}
	return s
}

func (a *App) copyContainer(src, dst *vaultDeviceConnection, closeVaults bool) (uint32, error) {
	data, rev, err := src.Device.ExportContainer()
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", src.Info.Label, err)
	}
	if !closeVaults {
		if err := dst.Device.ImportContainer(data, rev); err != nil {
			return 0, fmt.Errorf("writing %s: %w", dst.Info.Label, err)
		}
		a.logf("info", "[Mirror] Vault data copied from %s to %s (revision %d)", src.Info.Label, dst.Info.Label, rev)
		return rev, nil
	}
	if _, err := a.vm.LockForDevice(a.ctxOrBg(), dst.Info.serialKey()); err != nil {
		return 0, fmt.Errorf("close the vaults that use %s first: %w", dst.Info.Label, err)
	}
	a.mu.Lock()
	dstActive := a.activeVaultID == dst.Info.ID
	a.mu.Unlock()
	if dstActive {
		if err := a.LockAllVaults(); err != nil {
			return 0, err
		}
	}
	if err := dst.Device.ImportContainer(data, rev); err != nil {
		return 0, fmt.Errorf("writing %s: %w", dst.Info.Label, err)
	}
	a.logf("info", "[Mirror] Vault data copied from %s to %s (revision %d)", src.Info.Label, dst.Info.Label, rev)
	return rev, nil
}

func (a *App) copyPasskeys(src, dst *vaultDeviceConnection) (uint32, error) {
	data, rev, err := src.Device.ExportPasskeys()
	if err != nil {
		return 0, fmt.Errorf("reading passkeys from %s: %w", src.Info.Label, err)
	}
	if err := dst.Device.ImportPasskeys(data); err != nil {
		return 0, fmt.Errorf("writing passkeys to %s: %w", dst.Info.Label, err)
	}
	a.logf("info", "[Mirror] Passkeys copied from %s to %s", src.Info.Label, dst.Info.Label)
	return rev, nil
}

func (a *App) mirrorTick() {
	if !mirrorMu.TryLock() {
		return
	}
	defer mirrorMu.Unlock()
	if a.romBusy.Load() != 0 {
		return
	}
	c := a.mirrorConfig()
	if c.Main == "" || c.Backup == "" {
		return
	}
	m, b := a.vaultEntry(c.Main), a.vaultEntry(c.Backup)
	if m == nil || b == nil {
		return
	}
	ms, err1 := m.Device.Status()
	bs, err2 := b.Device.Status()
	if err1 != nil {
		a.setMirrorError(fmt.Errorf("%s does not answer: %w", m.Info.Label, err1))
		return
	}
	if err2 != nil {
		a.setMirrorError(fmt.Errorf("%s does not answer: %w", b.Info.Label, err2))
		return
	}
	changed := false

	side := containerSide(c, ms.Rev, bs.Rev, ms.ContainerHash, bs.ContainerHash)
	if side == "" && ms.ContainerHash != "" && c.Hash != ms.ContainerHash {
		c.Hash, c.Rev = ms.ContainerHash, ms.Rev
		changed = true
	}
	if side != "" {
		var src, dst *vaultDeviceConnection
		switch side {
		case "main":
			src, dst = m, b
		case "backup":
			src, dst = b, m
		}
		if src == nil {
			a.setMirrorError(nil)
			return
		}
		rev, err := a.copyContainer(src, dst, false)
		if err != nil {
			a.setMirrorError(err)
			return
		}
		a.setMirrorError(nil)
		c.Rev = rev
		c.LastSync = time.Now().Format("2006-01-02 15:04:05")
		changed = true
		ms, _ = m.Device.Status()
		bs, _ = b.Device.Status()
		c.Hash = ms.ContainerHash
		if bs.ContainerHash != ms.ContainerHash {
			c.Hash = ""
		}
	}

	if ms.Passkey && bs.Passkey && ms.Unlocked && bs.Unlocked && (c.PasskeysPending || ms.PasskeyRev != bs.PasskeyRev) {
		var src, dst *vaultDeviceConnection
		switch {
		case c.PasskeysPending && c.PasskeyFrom == "backup":
			src, dst = b, m
		case c.PasskeysPending:
			src, dst = m, b
		case ms.PasskeyRev != c.PasskeyRev && bs.PasskeyRev == c.PasskeyRev:
			src, dst = m, b
		case bs.PasskeyRev != c.PasskeyRev && ms.PasskeyRev == c.PasskeyRev:
			src, dst = b, m
		}
		if src != nil {
			rev, err := a.copyPasskeys(src, dst)
			if err != nil {
				a.setMirrorError(err)
			} else {
				c.PasskeyRev, c.PasskeysPending = rev, false
				c.LastSync = time.Now().Format("2006-01-02 15:04:05")
				changed = true
			}
		}
	}
	if changed {
		a.setMirrorConfig(c)
		runtime.EventsEmit(a.ctx, "devices:changed", nil)
	}
}

func (a *App) StartMirror(mainID, backupID string) error {
	mirrorMu.Lock()
	defer mirrorMu.Unlock()
	if mainID == "" || backupID == "" || mainID == backupID {
		return errors.New("choose two different ESP Crypts")
	}
	m, b := a.vaultEntry(mainID), a.vaultEntry(backupID)
	if m == nil || b == nil {
		return errors.New("connect both ESP Crypts first")
	}
	ms, err := m.Device.Status()
	if err != nil {
		return err
	}
	if !ms.Initialized {
		return errors.New("set up a Vault passphrase on the main ESP32 first")
	}
	bs, err := b.Device.Status()
	if err != nil {
		return err
	}
	if !bs.StorageReady {
		if err := b.Device.InitializeStorage(); err != nil {
			return fmt.Errorf("preparing %s: %w", b.Info.Label, err)
		}
	}
	rev, err := a.copyContainer(m, b, true)
	if err != nil {
		return err
	}
	hash := ""
	if st, err := b.Device.Status(); err == nil {
		hash = st.ContainerHash
	}
	c := MirrorConfig{Main: mainID, Backup: backupID, Rev: rev, Hash: hash, PasskeysPending: ms.Passkey && bs.Passkey,
		PasskeyFrom: "main", LastSync: time.Now().Format("2006-01-02 15:04:05")}
	a.setMirrorConfig(c)
	a.logf("info", "[Mirror] %s now mirrors %s", b.Info.Label, m.Info.Label)
	runtime.EventsEmit(a.ctx, "devices:changed", nil)
	return nil
}

func (a *App) StopMirror() {
	a.setMirrorConfig(MirrorConfig{})
	a.logf("info", "[Mirror] Mirror mode stopped; both ESP32s keep their data")
}

func (a *App) SwapMirror() {
	c := a.mirrorConfig()
	c.Main, c.Backup = c.Backup, c.Main
	a.setMirrorConfig(c)
}

func (a *App) SyncMirrorNow() MirrorStatus {
	a.mirrorTick()
	return a.GetMirrorStatus()
}

func (a *App) ResolveMirrorConflict(keep string) error {
	mirrorMu.Lock()
	defer mirrorMu.Unlock()
	c := a.mirrorConfig()
	m, b := a.vaultEntry(c.Main), a.vaultEntry(c.Backup)
	if m == nil || b == nil {
		return errors.New("connect both ESP Crypts first")
	}
	if keep != "main" && keep != "backup" {
		return errors.New("choose main or backup")
	}
	src, dst := m, b
	if keep == "backup" {
		src, dst = b, m
	}
	ss, err := src.Device.Status()
	if err != nil {
		return err
	}
	ds, err := dst.Device.Status()
	if err != nil {
		return err
	}
	rev, err := a.copyContainer(src, dst, true)
	if err != nil {
		return err
	}
	c.Rev = rev
	if st, err := dst.Device.Status(); err == nil {
		c.Hash = st.ContainerHash
	}
	if ss.Passkey && ds.Passkey {

		c.PasskeysPending, c.PasskeyFrom = true, keep
	}
	c.LastSync = time.Now().Format("2006-01-02 15:04:05")
	a.setMirrorConfig(c)
	return nil
}

type backupFile struct {
	Format     string `json:"format"`
	Version    int    `json:"version"`
	Chip       string `json:"chip"`
	DeviceID   string `json:"deviceId"`
	Created    string `json:"created"`
	Rev        uint32 `json:"rev"`
	Container  string `json:"container"`
	Passkeys   string `json:"passkeys,omitempty"`
	PasskeyRev uint32 `json:"passkeyRev,omitempty"`
}

func (a *App) BackupToFile() (string, error) {
	e, err := a.activeVault()
	if err != nil {
		return "", err
	}
	st, err := e.Device.Status()
	if err != nil {
		return "", err
	}
	data, rev, err := e.Device.ExportContainer()
	if err != nil {
		return "", err
	}
	f := backupFile{Format: "espcrypt-backup", Version: 1, Chip: st.Chip, DeviceID: st.DeviceID,
		Created: time.Now().Format(time.RFC3339), Rev: rev, Container: base64.StdEncoding.EncodeToString(data)}
	note := ""
	if st.Passkey {
		if st.Unlocked {
			f.Passkeys, f.PasskeyRev, err = e.Device.ExportPasskeys()
			if err != nil {
				return "", err
			}
		} else {
			note = " (without passkeys: unlock the ESP32 to include them)"
		}
	}
	b, _ := json.MarshalIndent(f, "", "  ")
	name := "espcrypt-backup-" + strings.ToLower(strings.ReplaceAll(st.Chip, "-", "")) + "-" + time.Now().Format("2006-01-02") + ".espcrypt"
	path, err := a.saveFile("Save ESP Crypt backup", name, b, runtime.FileFilter{DisplayName: "ESP Crypt backup (*.espcrypt)", Pattern: "*.espcrypt"})
	if err != nil {
		return "", err
	}
	a.logf("info", "[Backup] %s saved to %s%s", e.Info.Label, path, note)
	return path + note, nil
}

var pendingPasskeyRestore = map[string]string{}

func (a *App) RestoreFromFile() (string, error) {
	e, err := a.activeVault()
	if err != nil {
		return "", err
	}
	_, raw, err := a.openFile("Restore ESP Crypt backup", runtime.FileFilter{DisplayName: "ESP Crypt backup (*.espcrypt, *.espvault)", Pattern: "*.espcrypt;*.espvault"})
	if err != nil {
		return "", err
	}
	var f backupFile
	if json.Unmarshal(raw, &f) != nil || f.Format != "espcrypt-backup" || f.Container == "" {
		return "", errors.New("this is not an ESP Crypt backup file")
	}
	data, err := base64.StdEncoding.DecodeString(f.Container)
	if err != nil {
		return "", errors.New("the backup file is damaged")
	}
	st, err := e.Device.Status()
	if err != nil {
		return "", err
	}
	if !st.StorageReady {
		if err := e.Device.InitializeStorage(); err != nil {
			return "", err
		}
	}
	if _, err := a.vm.LockForDevice(a.ctxOrBg(), e.Info.serialKey()); err != nil {
		return "", err
	}
	if err := a.LockAllVaults(); err != nil {
		return "", err
	}
	if err := e.Device.ImportContainer(data, f.Rev); err != nil {
		return "", err
	}
	msg := "Backup restored. Unlock the ESP32 with the passphrase of the backup."
	if f.Passkeys != "" {
		if st.Passkey {
			mirrorMu.Lock()
			pendingPasskeyRestore[e.Info.ID] = f.Passkeys
			mirrorMu.Unlock()
			msg += " The passkeys are restored once it is unlocked."
		} else {
			msg += " The passkeys in the backup need the Vault + Passkey firmware on this board."
		}
	}
	a.logf("info", "[Backup] Backup from %s (%s) restored to %s", f.Created, f.Chip, e.Info.Label)
	runtime.EventsEmit(a.ctx, "devices:changed", nil)
	return msg, nil
}

func (a *App) restorePendingPasskeys() {
	mirrorMu.Lock()
	defer mirrorMu.Unlock()
	for id, data := range pendingPasskeyRestore {
		e := a.vaultEntry(id)
		if e == nil {
			continue
		}
		st, err := e.Device.Status()
		if err != nil || !st.Unlocked {
			continue
		}
		if err := e.Device.ImportPasskeys(data); err != nil {
			a.logf("error", "[Backup] Restoring the passkeys on %s failed: %v", e.Info.Label, err)
		} else {
			a.logf("info", "[Backup] Passkeys restored on %s", e.Info.Label)
		}
		delete(pendingPasskeyRestore, id)
	}
}
