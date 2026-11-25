package sshagent

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	beginMarker = "# >>> espcrypt >>>"
	endMarker   = "# <<< espcrypt <<<"

	legacyBeginMarker = "# >>> picohsm-manager >>>"
	legacyEndMarker   = "# <<< picohsm-manager <<<"
)

func sshConfigFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "config")
}

func ConfigInstalled() bool {
	b, err := os.ReadFile(sshConfigFile())
	return err == nil && strings.Contains(string(b), beginMarker)
}

func stripBlock(s, begin, end string) string {
	for {
		i := strings.Index(s, begin)
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], end)
		if j < 0 {
			return s[:i]
		}
		stop := i + j + len(end)
		if stop < len(s) && s[stop] == '\n' {
			stop++
		}
		s = s[:i] + s[stop:]
	}
}

func SetConfig(enable bool) error {
	p := sshConfigFile()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	old, _ := os.ReadFile(p)
	s := stripBlock(stripBlock(string(old), beginMarker, endMarker), legacyBeginMarker, legacyEndMarker)
	if enable {
		block := beginMarker + "\n# Added by ESP Crypt: ssh uses the SSH keys on your ESP Crypt.\nHost *\n    IdentityAgent \"" + ConfigPath() + "\"\n" + endMarker + "\n\n"
		s = block + s
	}
	if len(old) > 0 {
		_ = os.WriteFile(p+".espcrypt.bak", old, 0o600)
	}
	return os.WriteFile(p, []byte(s), 0o600)
}
