//go:build windows

package veracrypt

import (
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

func listWindows() []MountedVolume {
	vols := []MountedVolume{}
	for c := 'A'; c <= 'Z'; c++ {
		drive := string(c) + ":"
		if _, err := os.Stat(drive + `\`); err != nil {
			continue
		}
		name, _ := windows.UTF16PtrFromString(drive)
		buf := make([]uint16, 1024)
		n, err := windows.QueryDosDevice(name, &buf[0], uint32(len(buf)))
		if err != nil || n == 0 {
			continue
		}
		dev := windows.UTF16ToString(buf)
		if strings.Contains(strings.ToLower(dev), "veracrypt") {
			vols = append(vols, MountedVolume{Slot: string(c), Target: drive, Device: dev})
		}
	}
	return vols
}

func ownedByMe(os.FileInfo) bool { return true }
