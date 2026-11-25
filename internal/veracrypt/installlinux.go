package veracrypt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"espcrypt/internal/linuxsys"
)

const (
	linuxKeyURL         = "https://www.idrix.fr/VeraCrypt/VeraCrypt_PGP_public_key.asc"
	linuxKeyFingerprint = "5069A233D55A0EEB174A5FC3821ACD02680D16DE"
)

func linuxPackageNames(r linuxsys.OSRelease, goarch string) []string {
	deb, rpm := map[string]string{"amd64": "amd64", "arm64": "arm64"}[goarch], map[string]string{"amd64": "x86_64"}[goarch]
	var out []string
	major := strings.SplitN(r.VersionID, ".", 2)[0]
	switch {
	case r.ID == "ubuntu" && deb != "":
		out = append(out, "Ubuntu-"+r.VersionID+"-"+deb+".deb")
	case r.ID == "debian" && deb != "":
		out = append(out, "Debian-"+major+"-"+deb+".deb")
	case r.ID == "fedora" && rpm != "":
		out = append(out, "Fedora-"+major+"-"+rpm+".rpm")
	case strings.HasPrefix(r.ID, "opensuse") && rpm != "":
		out = append(out, "openSUSE-15-"+rpm+".rpm")
	}
	if r.Is("ubuntu") && deb != "" {
		out = append(out, "Ubuntu-24.04-"+deb+".deb")
	}
	if r.Is("debian") && deb != "" {
		out = append(out, "Debian-12-"+deb+".deb", "Debian-13-"+deb+".deb")
	}
	if (r.Is("fedora") || r.Is("rhel")) && rpm != "" {
		out = append(out, "CentOS-8-"+rpm+".rpm")
	}
	return out
}

func installLinux(ctx context.Context, dir string, progress func(string, int, int)) error {
	r := linuxsys.ReadOSRelease()
	pm, err := linuxsys.DetectPackageManager()
	if err != nil {
		return err
	}
	if Detect().Installed {
		if hasExec("mkfs.exfat") {
			return nil
		}
		progress("Installing exFAT tools…", 0, 0)
		return pm.Install(ctx, "exfatprogs")
	}
	if r.Is("arch") {
		progress("Installing VeraCrypt from the Arch repository…", 0, 0)
		return pm.Install(ctx, "veracrypt", "exfatprogs")
	}
	names := linuxPackageNames(r, runtime.GOARCH)
	if len(names) == 0 {
		return fmt.Errorf("no VeraCrypt package for %s %s (%s); get it from https://veracrypt.io/en/Downloads.html", r.ID, r.VersionID, runtime.GOARCH)
	}
	progress("Looking up the latest VeraCrypt version…", 0, 0)
	var url string
	for _, n := range names {
		suffix := n
		if url, err = latestAsset(ctx, "veracrypt/VeraCrypt", func(a string) bool {
			return strings.HasPrefix(a, "veracrypt-console-") && strings.HasSuffix(a, "-"+suffix)
		}); err == nil {
			break
		}
	}
	if url == "" {
		return fmt.Errorf("no VeraCrypt console package for %s %s (%s)", r.ID, r.VersionID, runtime.GOARCH)
	}
	pkg, err := download(ctx, url, dir, progress)
	if err != nil {
		return err
	}
	sig, err := download(ctx, url+".sig", dir, progress)
	if err != nil {
		return err
	}
	key, err := download(ctx, linuxKeyURL, dir, progress)
	if err != nil {
		return err
	}
	progress("Checking the signature…", 0, 0)
	if err := verifyPGP(ctx, dir, key, sig, pkg); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o755); err == nil {
		_ = os.Chmod(pkg, 0o644)
	}
	progress("Installing "+filepath.Base(pkg)+"…", 0, 0)
	switch {
	case strings.HasSuffix(pkg, ".deb"):
		_, err = linuxsys.Admin(ctx, []string{"DEBIAN_FRONTEND=noninteractive"}, "apt-get", "install", "-y", "-q", pkg)
	case pm.Name == "dnf":
		_, err = linuxsys.Admin(ctx, nil, "dnf", "install", "-y", "-q", pkg)
	case pm.Name == "zypper":
		_, err = linuxsys.Admin(ctx, nil, "zypper", "--non-interactive", "--no-gpg-checks", "install", pkg)
	default:
		_, err = linuxsys.Admin(ctx, nil, "rpm", "-U", "--replacepkgs", pkg)
	}
	if err != nil {
		return err
	}
	if !hasExec("mkfs.exfat") {
		progress("Installing exFAT tools…", 0, 0)
		_ = pm.Install(ctx, "exfatprogs")
	}
	InvalidateDetect()
	if !Detect().Installed {
		return errors.New("installation finished but veracrypt was not found")
	}
	return nil
}

func verifyPGP(ctx context.Context, dir, key, sig, file string) error {
	gpg, err := exec.LookPath("gpg")
	if err != nil {
		if gpg, err = exec.LookPath("gpg2"); err != nil {
			return errors.New("gpg is needed to check the VeraCrypt signature (install gnupg)")
		}
	}
	home := filepath.Join(dir, "gnupg")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	env := append(os.Environ(), "GNUPGHOME="+home)
	imp := exec.CommandContext(ctx, gpg, "--batch", "--quiet", "--import", key)
	imp.Env = env
	if out, err := imp.CombinedOutput(); err != nil {
		return fmt.Errorf("importing the VeraCrypt key failed: %s", strings.TrimSpace(string(out)))
	}
	ver := exec.CommandContext(ctx, gpg, "--batch", "--status-fd", "1", "--verify", sig, file)
	ver.Env = env
	out, _ := ver.Output()
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "[GNUPG:]" && f[1] == "VALIDSIG" {
			if strings.EqualFold(f[2], linuxKeyFingerprint) || strings.EqualFold(f[len(f)-1], linuxKeyFingerprint) {
				return nil
			}
		}
	}
	return errors.New("VeraCrypt signature invalid or not made by the VeraCrypt team key; installation aborted")
}
