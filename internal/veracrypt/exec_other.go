//go:build !windows

package veracrypt

import (
	"os"
	"os/exec"
	"syscall"
)

func hideWindow(*exec.Cmd) {}

func listWindows() []MountedVolume { return nil }

func ownedByMe(st os.FileInfo) bool {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return int(s.Uid) == os.Getuid()
	}
	return true
}
