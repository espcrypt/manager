package main

import (
	"os/exec"
	"path/filepath"
	goruntime "runtime"

	"espcrypt/internal/veracrypt"
)

func (a *App) VCInfo() veracrypt.Info { return veracrypt.Detect() }

func (a *App) VCInstall() error {
	a.logf("info", "[VeraCrypt] Installation started")
	err := veracrypt.Install(a.ctx, func(step string, done, total int) {
		a.progress("vcinstall", done, total, step)
	})
	if err != nil {
		a.logf("error", "[VeraCrypt] %v", err)
		return err
	}
	a.logf("info", "[VeraCrypt] Installed")
	return nil
}

func (a *App) OpenPath(p string) {
	switch goruntime.GOOS {
	case "windows":
		if len(p) <= 3 {
			p = string(p[0]) + `:\`
		}
		_ = exec.Command("explorer.exe", p).Start()
	case "darwin":
		_ = exec.Command("open", p).Start()
	default:
		_ = exec.Command("xdg-open", p).Start()
	}
}

func (a *App) RevealPath(p string) {
	switch goruntime.GOOS {
	case "windows":
		_ = exec.Command("explorer.exe", "/select,", p).Start()
	case "darwin":
		_ = exec.Command("open", "-R", p).Start()
	default:
		_ = exec.Command("xdg-open", filepath.Dir(p)).Start()
	}
}
