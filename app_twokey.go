package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"espcrypt/internal/vault"
)

func (a *App) twoKeyMode() bool { return a.GetSettings().KeyMode == 2 }

func (a *App) allKeyStores() []vault.KeyStore {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]vault.KeyStore, 0, len(a.vaultdevs))
	for _, entry := range a.vaultdevs {
		out = append(out, entry.Device)
	}
	return out
}

func (a *App) secondVaultLocked() *vaultDeviceConnection {
	if e := a.vaultdevs[a.settings.SecondVaultID]; e != nil && a.settings.SecondVaultID != a.activeVaultID {
		return e
	}
	ids := make([]string, 0, len(a.vaultdevs))
	for id := range a.vaultdevs {
		if id != a.activeVaultID {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Slice(ids, func(i, j int) bool { return a.vaultdevs[ids[i]].Info.Label < a.vaultdevs[ids[j]].Info.Label })
	return a.vaultdevs[ids[0]]
}

func (a *App) secondVault() *vaultDeviceConnection {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.secondVaultLocked()
}

func (a *App) requireSecondVault() (*vaultDeviceConnection, error) {
	second := a.secondVault()
	if second == nil {
		return nil, errors.New(vault.ErrDeviceMissing + ": 2-ESP mode needs a second ESP Crypt; connect and unlock another chip too")
	}
	st, err := second.Device.Status()
	if err != nil {
		return nil, fmt.Errorf("%s: connect and unlock %s too", vault.ErrDeviceMissing, vault.ChipLabel(second.Info.serialKey()))
	}
	if !st.Unlocked {
		return nil, fmt.Errorf("%s[%s]: 2-ESP mode needs both chips; unlock %s too", vault.ErrDeviceLocked, second.Info.serialKey(), vault.ChipLabel(second.Info.serialKey()))
	}
	return second, nil
}

func (a *App) SetKeyMode(mode int) error {
	if mode != 1 && mode != 2 {
		return errors.New("choose 1 or 2 ESP Crypts")
	}
	a.mu.Lock()
	a.settings.KeyMode = mode
	a.mu.Unlock()
	if err := a.persistSettings(); err != nil {
		return err
	}
	if mode == 2 {
		a.logf("info", "2-ESP mode on: new vaults and SSH keys need two ESP Crypts")
	} else {
		a.logf("info", "1-ESP mode: new vaults and SSH keys use only the active ESP Crypt")
	}
	runtime.EventsEmit(a.ctx, "devices:changed", nil)
	return nil
}

func (a *App) SetSecondVaultDevice(id string) error {
	a.deviceSwitchMu.Lock()
	defer a.deviceSwitchMu.Unlock()
	a.mu.Lock()
	if id != "" && a.vaultdevs[id] == nil {
		a.mu.Unlock()
		return errors.New("select a connected ESP Crypt")
	}
	if id != "" && id == a.activeVaultID {
		a.mu.Unlock()
		return errors.New("the second chip must be a different ESP Crypt than the active one")
	}
	old := a.secondVaultLocked()
	a.mu.Unlock()
	if old != nil && old.Info.ID != id {
		if _, err := a.vm.LockForDevice(a.ctxOrBg(), old.Info.serialKey()); err != nil {
			return fmt.Errorf("close the vaults that use %s first: %w", vault.ChipLabel(old.Info.serialKey()), err)
		}
		_ = old.Device.Lock()
	}
	a.mu.Lock()
	a.settings.SecondVaultID = id
	a.mu.Unlock()
	if err := a.persistSettings(); err != nil {
		return err
	}
	runtime.EventsEmit(a.ctx, "devices:changed", nil)
	return nil
}

func (a *App) UnlockVaultDevice(id, pin string) error {
	a.mu.Lock()
	entry := a.vaultdevs[id]
	a.mu.Unlock()
	if entry == nil {
		return errors.New("that ESP Crypt is no longer connected")
	}
	if err := entry.Device.Unlock(pin); err != nil {
		a.logf("error", "Unlock of %s failed: %v", entry.Info.Label, err)
		return err
	}
	a.rememberPIN(entry.Info.Serial, pin)
	a.logf("info", "%s unlocked", entry.Info.Label)
	runtime.EventsEmit(a.ctx, "devices:changed", nil)
	runtime.EventsEmit(a.ctx, "device:changed", true)
	return nil
}

func (a *App) LockVaultDevice(id string) error {
	a.mu.Lock()
	entry := a.vaultdevs[id]
	active := id == a.activeVaultID
	a.mu.Unlock()
	if entry == nil {
		return nil
	}
	if active {
		return a.Logout()
	}
	if _, err := a.vm.LockForDevice(a.ctxOrBg(), entry.Info.serialKey()); err != nil {
		return fmt.Errorf("close the vaults that use %s first (files may be in use): %w", vault.ChipLabel(entry.Info.serialKey()), err)
	}
	a.forgetPIN(entry.Info.Serial)
	if err := entry.Device.Lock(); err != nil {
		return err
	}
	a.logf("info", "%s locked", entry.Info.Label)
	runtime.EventsEmit(a.ctx, "devices:changed", nil)
	return nil
}

func (a *App) lockVaultsOnRemoval(info VaultDeviceInfo) {
	if !a.GetSettings().LockOnRemove {
		return
	}
	if p := a.unlockedMirrorPartner(info); p != "" {
		a.logf("info", "%s is gone; vaults stay open because %s holds the same keys", info.Label, p)
		return
	}
	a.lockVaultsForDevice(info)
}

func (a *App) unlockedMirrorPartner(info VaultDeviceInfo) string {
	a.mu.Lock()
	entries := make([]*vaultDeviceConnection, 0, len(a.vaultdevs))
	for _, e := range a.vaultdevs {
		entries = append(entries, e)
	}
	a.mu.Unlock()
	for _, e := range entries {
		if isRemotePort(e.Info.Port) || e.Info.ID == info.ID || !a.mirrorPartners(e.Info.ID, info.ID) {
			continue
		}
		if st, err := e.Device.CachedStatus(2 * time.Second); err == nil && st.Unlocked {
			return e.Info.Label
		}
	}
	return ""
}

func (a *App) lockVaultsForDevice(info VaultDeviceInfo) {
	if n, err := a.vm.LockForDevice(a.ctxOrBg(), info.serialKey()); err != nil {
		a.logf("error", "Could not lock the vaults that use %s: %v", info.Label, err)
	} else if n > 0 {
		a.logf("info", "Locked %d vault(s) that need %s", n, info.Label)
	}
}

func (a *App) resolveSecondDevice(r *vault.CreateRequest) error {
	if r.KeyCount == 1 || (r.KeyCount == 0 && r.SecondDevice == "" && !a.twoKeyMode()) {
		r.SecondDevice = ""
		return nil
	}
	if r.SecondDevice != "" {
		a.mu.Lock()
		entry := a.vaultdevs[r.SecondDevice]
		a.mu.Unlock()
		if entry != nil {
			r.SecondDevice = entry.Info.serialKey()
		}
		return nil
	}
	second, err := a.requireSecondVault()
	if err != nil {
		return err
	}
	r.SecondDevice = second.Info.serialKey()
	if r.SecondDevice == "" {
		return errors.New("the second ESP Crypt reports no serial number; update its firmware")
	}
	return nil
}

func isDeviceLockedErr(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), vault.ErrDeviceLocked)
}
