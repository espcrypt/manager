package linuxsys

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var ErrNeedsPassword = errors.New("administrator password needed: run the action again and enter your password, or run it as root")

const (
	PicoVendor  = "0x2E8A"
	PicoProduct = "0x10FD"
	PicoName    = "Pico Key"
)

func IsLinux() bool { return runtime.GOOS == "linux" }

func IsRoot() bool { return os.Geteuid() == 0 }

func HasAdmin() bool {
	if IsRoot() {
		return true
	}
	return exec.Command("sudo", "-n", "true").Run() == nil
}

func Admin(ctx context.Context, env []string, name string, args ...string) (string, error) {
	var cmd *exec.Cmd
	if IsRoot() {
		cmd = exec.CommandContext(ctx, name, args...)
		cmd.Env = append(os.Environ(), env...)
	} else {
		if _, err := exec.LookPath("sudo"); err != nil {
			return "", errors.New("this needs root: run espcrypt as root or install sudo")
		}
		full := append([]string{"-n", "env"}, env...)
		full = append(full, name)
		full = append(full, args...)
		cmd = exec.CommandContext(ctx, "sudo", full...)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if err != nil {
		if strings.Contains(text, "password is required") || strings.Contains(text, "a terminal is required") {
			return text, ErrNeedsPassword
		}
		if text == "" {
			text = err.Error()
		}
		return text, fmt.Errorf("%s: %s", filepath.Base(name), lastLines(text, 4))
	}
	return text, nil
}

func lastLines(s string, n int) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}

type OSRelease struct{ ID, VersionID, Like string }

func ReadOSRelease() OSRelease {
	var r OSRelease
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return r
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			r.ID = v
		case "VERSION_ID":
			r.VersionID = v
		case "ID_LIKE":
			r.Like = v
		}
	}
	return r
}

func (r OSRelease) Is(id string) bool {
	return r.ID == id || strings.Contains(" "+r.Like+" ", " "+id+" ")
}

type PackageManager struct {
	Name    string
	install []string
	env     []string
}

func DetectPackageManager() (PackageManager, error) {
	for _, pm := range []PackageManager{
		{Name: "apt-get", install: []string{"install", "-y", "-q"}, env: []string{"DEBIAN_FRONTEND=noninteractive"}},
		{Name: "dnf", install: []string{"install", "-y", "-q"}},
		{Name: "pacman", install: []string{"-S", "--needed", "--noconfirm"}},
		{Name: "zypper", install: []string{"--non-interactive", "install"}},
		{Name: "apk", install: []string{"add"}},
	} {
		if _, err := exec.LookPath(pm.Name); err == nil {
			return pm, nil
		}
	}
	return PackageManager{}, errors.New("no supported package manager found (apt, dnf, pacman, zypper, apk)")
}

func (pm PackageManager) Install(ctx context.Context, pkgs ...string) error {
	if pm.Name == "apt-get" {
		_, _ = Admin(ctx, pm.env, "apt-get", "update", "-q")
	}
	_, err := Admin(ctx, pm.env, pm.Name, append(pm.install, pkgs...)...)
	return err
}

const (
	pcscGroup      = "espcrypt"
	pcscPolicy     = "/usr/share/polkit-1/actions/org.debian.pcsc-lite.policy"
	pcscRuleFile   = "/etc/polkit-1/rules.d/49-espcrypt-pcsc.rules"
	pcscRuleSource = `polkit.addRule(function(action, subject) {
    if ((action.id == "org.debian.pcsc-lite.access_pcsc" || action.id == "org.debian.pcsc-lite.access_card") &&
        subject.isInGroup("espcrypt")) {
        return polkit.Result.YES;
    }
});
`
)

func targetUser() string {
	if IsRoot() {
		return os.Getenv("SUDO_USER")
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

func inGroup(username, group string) bool {
	u, err := user.Lookup(username)
	if err != nil {
		return false
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		return false
	}
	ids, _ := u.GroupIds()
	for _, id := range ids {
		if id == g.Gid {
			return true
		}
	}
	return false
}

var reVendor = regexp.MustCompile(`(?s)<key>ifdVendorID</key>\s*<array>(.*?)</array>`)
var reProduct = regexp.MustCompile(`(?s)<key>ifdProductID</key>\s*<array>(.*?)</array>`)
var reString = regexp.MustCompile(`<string>([^<]*)</string>`)
