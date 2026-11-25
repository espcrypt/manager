package veracrypt

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
	"strings"
	"time"
)

const (
	fallbackWinVersion = "1.26.29"
	fallbackMacVersion = "1.26.30"
	fallbackFuseT      = "https://github.com/macos-fuse-t/fuse-t/releases/download/1.2.9/fuse-t-macos-installer-1.2.9.pkg"
)

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func latestAsset(ctx context.Context, repo string, match func(string) bool) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+repo+"/releases?per_page=8", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub: %s", resp.Status)
	}
	var rels []ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return "", err
	}
	for _, r := range rels {
		for _, a := range r.Assets {
			if match(a.Name) {
				return a.URL, nil
			}
		}
	}
	return "", errors.New("no suitable installer found")
}

func download(ctx context.Context, url, dir string, progress func(string, int, int)) (string, error) {
	if !strings.HasPrefix(url, "https://github.com/veracrypt/") && !strings.HasPrefix(url, "https://github.com/macos-fuse-t/") && url != linuxKeyURL {
		return "", errors.New("unexpected download URL")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download %s: %s", filepath.Base(url), resp.Status)
	}
	path := filepath.Join(dir, filepath.Base(url))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, 64*1024)
	done := 0
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return "", err
			}
			done += n
			progress(filepath.Base(url), done, int(resp.ContentLength))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	return path, nil
}

func Install(ctx context.Context, progress func(step string, done, total int)) error {
	dir, err := os.MkdirTemp("", "phm-vc-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	defer func() { CleanupStaleMounts(); InvalidateDetect() }()
	switch runtime.GOOS {
	case "windows":
		return installWindows(ctx, dir, progress)
	case "darwin":
		return installMac(ctx, dir, progress)
	case "linux":
		return installLinux(ctx, dir, progress)
	}
	return errors.New("operating system not supported")
}

func installWindows(ctx context.Context, dir string, progress func(string, int, int)) error {
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	progress("Looking up the latest VeraCrypt version…", 0, 0)
	url, err := latestAsset(ctx, "veracrypt/VeraCrypt", func(n string) bool {
		return strings.HasPrefix(n, "VeraCrypt_Setup_"+arch+"_") && strings.HasSuffix(n, ".msi")
	})
	if err != nil {
		v := fallbackWinVersion
		url = fmt.Sprintf("https://github.com/veracrypt/VeraCrypt/releases/download/VeraCrypt_%s/VeraCrypt_Setup_%s_%s.msi", v, arch, v)
	}
	msi, err := download(ctx, url, dir, progress)
	if err != nil {
		return err
	}
	progress("Checking digital signature…", 0, 0)
	sig, _ := run(ctx, "powershell", nil, "-NoProfile", "-NonInteractive", "-Command",
		fmt.Sprintf("$s=Get-AuthenticodeSignature -LiteralPath '%s'; \"$($s.Status)|$($s.SignerCertificate.Subject)\"", strings.ReplaceAll(msi, "'", "''")))
	if !strings.HasPrefix(sig, "Valid|") || !strings.Contains(strings.ToUpper(sig), "IDRIX") {
		return fmt.Errorf("installer signature invalid (%s); installation aborted", sig)
	}
	progress("Installing (confirm the Windows security prompt)…", 0, 0)
	ps := fmt.Sprintf("$p=Start-Process msiexec.exe -ArgumentList @('/i','\"%s\"','/qn','/norestart','ACCEPTLICENSE=YES') -Verb RunAs -Wait -PassThru; exit $p.ExitCode",
		strings.ReplaceAll(msi, "'", "''"))
	out, err := run(ctx, "powershell", nil, "-NoProfile", "-NonInteractive", "-Command", ps)
	if err != nil {
		if strings.Contains(out, "canceled") || strings.Contains(out, "cancelled") {
			return errors.New("installation cancelled")
		}
		return fmt.Errorf("installation failed: %v", err)
	}
	if !Detect().Installed {
		return errors.New("installation finished but VeraCrypt was not found; a restart may be required")
	}
	return nil
}

func installMac(ctx context.Context, dir string, progress func(string, int, int)) error {
	progress("Looking up the latest VeraCrypt (FUSE-T)…", 0, 0)
	vcURL, err := latestAsset(ctx, "veracrypt/VeraCrypt", func(n string) bool {
		return strings.HasPrefix(n, "VeraCrypt_FUSE-T_") && strings.HasSuffix(n, ".dmg")
	})
	if err != nil {
		v := fallbackMacVersion
		vcURL = fmt.Sprintf("https://github.com/veracrypt/VeraCrypt/releases/download/VeraCrypt_%s/VeraCrypt_FUSE-T_%s.dmg", v, v)
	}
	fuseURL, err := latestAsset(ctx, "macos-fuse-t/fuse-t", func(n string) bool {
		return strings.HasPrefix(n, "fuse-t-macos-installer") && strings.HasSuffix(n, ".pkg")
	})
	if err != nil {
		fuseURL = fallbackFuseT
	}
	var fusePkg string
	if !fuseTInstalled() {
		if fusePkg, err = download(ctx, fuseURL, dir, progress); err != nil {
			return err
		}
	}
	dmg, err := download(ctx, vcURL, dir, progress)
	if err != nil {
		return err
	}
	mnt := filepath.Join(dir, "mnt")
	_ = os.MkdirAll(mnt, 0o700)
	progress("Attaching installer disk…", 0, 0)
	if out, err := run(ctx, "hdiutil", nil, "attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mnt, dmg); err != nil {
		return fmt.Errorf("attaching dmg failed: %s", out)
	}
	defer func() { _, _ = run(context.Background(), "hdiutil", nil, "detach", "-force", mnt) }()
	vcPkg := filepath.Join(mnt, "VeraCrypt_Installer.pkg")
	if _, err := os.Stat(vcPkg); err != nil {
		matches, _ := filepath.Glob(filepath.Join(mnt, "*.pkg"))
		if len(matches) == 0 {
			return errors.New("installer package not found in dmg")
		}
		vcPkg = matches[0]
	}
	progress("Checking signatures…", 0, 0)
	pkgs := []string{vcPkg}
	signers := map[string]string{vcPkg: "Mounir Joseph IDRASSI (HLTN5GLG99)"}
	if fusePkg != "" {
		pkgs = append([]string{fusePkg}, pkgs...)
		signers[fusePkg] = "alex fishman (6DY7Z4SVDZ)"
	}
	for _, p := range pkgs {

		raw, _ := exec.CommandContext(ctx, "spctl", "-a", "-vv", "-t", "install", p).CombinedOutput()
		out := string(raw)
		if !strings.Contains(out, ": accepted") || !strings.Contains(out, "Notarized Developer ID") || !strings.Contains(out, signers[p]) {
			return fmt.Errorf("signature of %s invalid (%s); installation aborted", filepath.Base(p), strings.TrimSpace(out))
		}
	}

	if fusePkg != "" {
		progress("Installing FUSE-T (enter your Mac password)…", 0, 0)
		script := fmt.Sprintf("do shell script %q with administrator privileges", "/usr/sbin/installer -pkg "+shQuote(fusePkg)+" -target /")
		if out, err := runRaw(ctx, "osascript", nil, "-e", script); err != nil {
			if strings.Contains(err.Error(), "-128") || strings.Contains(out, "-128") {
				return errors.New("installation cancelled")
			}
			return fmt.Errorf("installing FUSE-T failed: %v %s", err, out)
		}
	}

	progress("Installing VeraCrypt…", 0, 0)
	exp := filepath.Join(dir, "expanded")
	if out, err := runRaw(ctx, "pkgutil", nil, "--expand-full", vcPkg, exp); err != nil {
		return fmt.Errorf("unpacking VeraCrypt failed: %v %s", err, out)
	}
	var appSrc string
	_ = filepath.WalkDir(exp, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && d.Name() == "VeraCrypt.app" && appSrc == "" {
			appSrc = p
			return filepath.SkipDir
		}
		return nil
	})
	if appSrc == "" {
		return errors.New("VeraCrypt.app not found in the package")
	}
	home, _ := os.UserHomeDir()
	userApps := filepath.Join(home, "Applications")
	_ = os.MkdirAll(userApps, 0o755)
	dst := filepath.Join(userApps, "VeraCrypt.app")
	if _, err := os.Stat(dst); err == nil {
		_ = os.Rename(dst, dst+".old-"+fmt.Sprint(time.Now().Unix()))
	}
	if out, err := runRaw(ctx, "ditto", nil, appSrc, dst); err != nil {
		return fmt.Errorf("copying VeraCrypt failed: %v %s", err, out)
	}
	InvalidateDetect()
	if !Detect().Installed {
		return errors.New("installation finished but VeraCrypt was not found")
	}
	return nil
}

func fuseTInstalled() bool {
	for _, p := range []string{"/Library/Application Support/fuse-t", "/usr/local/lib/libfuse-t.dylib", "/Library/Filesystems/fuse-t.fs"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	_, err := exec.LookPath("go-nfsv4")
	return err == nil
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
