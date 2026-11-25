package veracrypt

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"espcrypt/internal/linuxsys"
)

func vc(ctx context.Context, info Info, stdin []byte, args ...string) (string, error) {
	if runtime.GOOS != "linux" || linuxsys.IsRoot() {
		return run(ctx, info.Path, stdin, args...)
	}
	out, err := runRaw(ctx, "sudo", stdin, append([]string{"-n", info.Path}, args...)...)
	if err != nil && (strings.Contains(err.Error(), "password is required") || strings.Contains(err.Error(), "a terminal is required")) {
		return out, linuxsys.ErrNeedsPassword
	}
	return out, err
}

func hasExec(name string) bool {
	if _, err := exec.LookPath(name); err == nil {
		return true
	}
	for _, d := range []string{"/usr/sbin/", "/sbin/", "/usr/local/sbin/"} {
		if _, err := os.Stat(d + name); err == nil {
			return true
		}
	}
	return false
}

func procMounts() []MountedVolume {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return []MountedVolume{}
	}
	defer f.Close()
	vols := []MountedVolume{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) < 3 || !strings.HasPrefix(fl[0], "/dev/mapper/veracrypt") {
			continue
		}
		target := strings.ReplaceAll(fl[1], `\040`, " ")
		vols = append(vols, MountedVolume{Slot: strings.TrimPrefix(fl[0], "/dev/mapper/veracrypt"), Device: fl[0], Target: target})
	}
	return vols
}

var ErrNeedsPassword = linuxsys.ErrNeedsPassword

func IsNeedsPassword(err error) bool { return errors.Is(err, linuxsys.ErrNeedsPassword) }

func CanFormat(fs string) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	tool := map[string]string{"exfat": "mkfs.exfat", "ext4": "mkfs.ext4", "fat": "mkfs.vfat", "ntfs": "mkfs.ntfs"}[strings.ToLower(fs)]
	if tool != "" && !hasExec(tool) {
		pkg := map[string]string{"mkfs.exfat": "exfatprogs", "mkfs.ext4": "e2fsprogs", "mkfs.vfat": "dosfstools", "mkfs.ntfs": "ntfs-3g"}[tool]
		return errors.New(fs + " vaults need " + tool + ": run Installers → VeraCrypt (or espcrypt install veracrypt) to add " + pkg)
	}
	return nil
}
