package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const updateRepo = "espcrypt/manager"

var appVersion = "dev"

type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	Notes     string `json:"notes"`
	URL       string `json:"url"`
	Asset     string `json:"asset"`
	assetURL  string
	assetSize int64
}

var updateState struct {
	sync.Mutex
	last UpdateInfo
	busy bool
}

type ghRelease struct {
	Tag     string `json:"tag_name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	Draft   bool   `json:"draft"`
	Pre     bool   `json:"prerelease"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func newer(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func assetSuffix() string {
	switch runtime.GOOS {
	case "darwin":
		return "-macOS-universal.zip"
	case "windows":
		return "-windows-x64-setup.exe"
	}
	return ""
}

func (a *App) CheckForUpdate() (UpdateInfo, error) {
	info := UpdateInfo{Current: appVersion}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+updateRepo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "espcrypt-app")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return info, fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return info, nil
	}
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return info, err
	}
	info.Latest, info.Notes, info.URL = strings.TrimPrefix(rel.Tag, "v"), rel.Body, rel.HTMLURL
	if suffix := assetSuffix(); suffix != "" {
		for _, as := range rel.Assets {
			if strings.HasSuffix(as.Name, suffix) && strings.HasPrefix(as.URL, "https://github.com/"+updateRepo+"/releases/download/") {
				info.Asset, info.assetURL, info.assetSize = as.Name, as.URL, as.Size
			}
		}
	}
	info.Available = appVersion != "dev" && info.assetURL != "" && newer(info.Latest, appVersion)
	updateState.Lock()
	updateState.last = info
	updateState.Unlock()
	return info, nil
}

func (a *App) updateLoop() {
	time.Sleep(15 * time.Second)
	for {
		if !a.GetSettings().NoAutoUpdate {
			if info, err := a.CheckForUpdate(); err == nil && info.Available {
				a.logf("info", "[Update] ESP Crypt %s is available (this is %s)", info.Latest, info.Current)
				if a.ctx != nil {
					wruntime.EventsEmit(a.ctx, "update:available", info)
				}
			}
		}
		time.Sleep(6 * time.Hour)
	}
}

func (a *App) InstallUpdate() error {
	updateState.Lock()
	if updateState.busy {
		updateState.Unlock()
		return errors.New("an update is already being installed")
	}
	updateState.busy = true
	updateState.Unlock()
	defer func() { updateState.Lock(); updateState.busy = false; updateState.Unlock() }()

	info, err := a.CheckForUpdate()
	if err != nil {
		return err
	}
	if !info.Available {
		return errors.New("ESP Crypt is up to date")
	}
	dir, err := os.MkdirTemp("", "espcrypt-update-")
	if err != nil {
		return err
	}
	file := filepath.Join(dir, info.Asset)
	if err := a.download(info, file); err != nil {
		return err
	}
	a.logf("info", "[Update] Downloaded %s; installing", info.Asset)
	if err := a.LockAllVaults(); err != nil {
		return fmt.Errorf("close the open vaults first: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return a.installMac(dir, file)
	case "windows":
		return a.installWindows(file)
	}
	return errors.New("automatic updates are available on macOS and Windows; download the new version from the releases page")
}

func (a *App) download(info UpdateInfo, path string) error {
	req, _ := http.NewRequest("GET", info.assetURL, nil)
	req.Header.Set("User-Agent", "espcrypt-app")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	total := info.assetSize
	if total <= 0 {
		total = resp.ContentLength
	}
	var done int64
	buf := make([]byte, 256*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			done += int64(n)
			a.progress("update", int(done), int(total), "Downloading ESP Crypt "+info.Latest+"…")
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("download failed: %w", rerr)
		}
	}
	if total > 0 && done != total {
		return errors.New("download incomplete")
	}
	return nil
}

func (a *App) installMac(dir, zip string) error {
	if out, err := exec.Command("ditto", "-x", "-k", zip, dir).CombinedOutput(); err != nil {
		return fmt.Errorf("unpacking the update failed: %v %s", err, out)
	}
	newApp := filepath.Join(dir, "ESP Crypt.app")
	if _, err := os.Stat(newApp); err != nil {
		return errors.New("the update does not contain ESP Crypt.app")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if !strings.HasSuffix(bundle, ".app") {
		return errors.New("updates work for the installed app, not for a development build")
	}
	script := fmt.Sprintf(`while kill -0 %d 2>/dev/null; do sleep 0.3; done
rm -rf %q && mv %q %q && xattr -dr com.apple.quarantine %q 2>/dev/null; open %q`,
		os.Getpid(), bundle, newApp, bundle, bundle, bundle)
	cmd := exec.Command("/bin/sh", "-c", script)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	a.logf("info", "[Update] Restarting into the new version")
	go func() { time.Sleep(500 * time.Millisecond); a.QuitApp() }()
	return nil
}

func (a *App) installWindows(setup string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	script := fmt.Sprintf(`timeout /t 2 /nobreak >nul & "%s" /S & start "" "%s"`, setup, exe)
	cmd := exec.Command("cmd", "/C", script)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	a.logf("info", "[Update] Installing; the app restarts when done")
	go func() { time.Sleep(500 * time.Millisecond); a.QuitApp() }()
	return nil
}

func (a *App) AppVersion() string { return appVersion }
