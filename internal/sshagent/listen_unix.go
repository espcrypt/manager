//go:build !windows

package sshagent

import (
	"net"
	"os"
	"path/filepath"
)

func SocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "espcrypt-agent.sock")
}

func ConfigPath() string { return "~/.ssh/espcrypt-agent.sock" }

func listen(path string) (net.Listener, error) {
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return l, nil
}

func cleanup(path string) { _ = os.Remove(path) }
