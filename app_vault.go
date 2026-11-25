package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"espcrypt/internal/vault"
)

const VaultExt = vault.Ext

func (a *App) newVaultManager() *vault.Manager {
	m := vault.NewManager(a.keyStore)
	m.Devices = a.allKeyStores
	m.Progress = func(s string) { a.progress("vault", 0, 0, s) }
	m.Log = a.logf
	m.SameData = a.mirrorPartners
	m.OnChange = func() {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "vaults:changed", nil)
		}
	}
	return m
}

func (a *App) ctxOrBg() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) reveal(v vault.View) {
	if a.GetSettings().OpenAfterUnlock && v.MountPoint != "" {
		a.OpenPath(v.MountPoint)
	}
}

func (a *App) ListVaults() []vault.View { return a.vm.List() }

func (a *App) CreateVault(r vault.CreateRequest) (vault.View, error) {
	if err := a.resolveSecondDevice(&r); err != nil {
		return vault.View{}, err
	}
	v, err := a.vm.Create(a.ctxOrBg(), r)
	if err == nil {
		a.reveal(v)
	}
	return v, err
}

func (a *App) UnlockVault(id, password string) (vault.View, error) {
	v, err := a.vm.Unlock(a.ctxOrBg(), id, password)
	if err == nil {
		a.reveal(v)
	}
	return v, err
}

func (a *App) LockVault(id string) error { return a.vm.Lock(a.ctxOrBg(), id) }

func (a *App) LockAllVaults() error {
	_, err := a.vm.LockAll(a.ctxOrBg())
	return err
}

func (a *App) RenameVault(id, name string) error { return a.vm.Rename(id, name) }

func (a *App) ForgetVault(id string) error { return a.vm.Forget(id) }

func (a *App) ChangeVaultPassword(id, oldPassword, newPassword string) error {
	return a.vm.ChangePassword(a.ctxOrBg(), id, oldPassword, newPassword)
}

func (a *App) OpenVaultFile(path, password string, keyfileID int, hasPassword bool) (vault.OpenResult, error) {
	r, err := a.vm.OpenFile(a.ctxOrBg(), path, password, keyfileID, hasPassword)
	if err == nil && r.Vault != nil {
		a.reveal(*r.Vault)
	}
	return r, err
}

func (a *App) RevealVault(id string) error {
	v, err := a.vm.Find(id)
	if err != nil {
		return err
	}
	a.RevealPath(v.Path)
	return nil
}

func (a *App) PickVaultFile(create bool, name string) (string, error) {
	var p string
	var err error
	_ = os.MkdirAll(vault.DefaultDir(), 0o700)
	if create {
		p, err = runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Where should the vault be stored?",
			DefaultDirectory: vault.DefaultDir(), DefaultFilename: filepath.Base(vault.DefaultPath(name)), CanCreateDirectories: true})
		if err == nil && p != "" && !strings.EqualFold(filepath.Ext(p), VaultExt) {
			p += VaultExt
		}
	} else {
		p, err = runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Open a vault", DefaultDirectory: vault.DefaultDir()})
	}
	if err != nil {
		return "", fmt.Errorf("could not open the file dialog: %w", err)
	}
	if p == "" {
		return "", errors.New("cancelled")
	}
	return p, nil
}

func (a *App) handleArgs(args []string) {
	for _, arg := range args {
		if strings.EqualFold(filepath.Ext(arg), VaultExt) || strings.EqualFold(filepath.Ext(arg), ".hc") {
			a.openVaultFromOS(arg)
			return
		}
	}
}

func (a *App) openVaultFromOS(path string) {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	a.mu.Lock()
	ready := a.ctx != nil
	a.mu.Unlock()
	if ready {
		if v, ok := a.vm.FindByPath(path); ok {
			if vv, err := a.vm.Get(v.ID); err == nil && vv.Mounted {
				a.OpenPath(vv.MountPoint)
				return
			}
		}
	}
	a.mu.Lock()
	a.pendingOpen = path
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil {
		runtime.WindowUnminimise(ctx)
		runtime.WindowShow(ctx)
		runtime.EventsEmit(ctx, "vault:open-file", path)
	}
}

func (a *App) TakePendingVaultOpen() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.pendingOpen
	a.pendingOpen = ""
	return p
}

func (a *App) mirrorPartners(x, y string) bool {
	c := a.mirrorConfig()
	if c.Main == "" || c.Backup == "" {
		return false
	}
	norm := func(s string) string { return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), ":", " ")) }
	x, y = norm(x), norm(y)
	m, b := norm(c.Main), norm(c.Backup)
	return (x == m && y == b) || (x == b && y == m)
}
