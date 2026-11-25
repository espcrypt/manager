//go:build windows

package sshagent

import (
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const pipeName = `\\.\pipe\espcrypt-ssh-agent`

func SocketPath() string { return pipeName }

func ConfigPath() string { return "//./pipe/espcrypt-ssh-agent" }

func listen(path string) (net.Listener, error) {
	sddl := "D:P(A;;GA;;;SY)"
	if tok := windows.GetCurrentProcessToken(); tok != 0 {
		if u, err := tok.GetTokenUser(); err == nil {
			sddl = "D:P(A;;GA;;;SY)(A;;GA;;;" + u.User.Sid.String() + ")"
		}
	}
	return winio.ListenPipe(path, &winio.PipeConfig{SecurityDescriptor: sddl})
}

func cleanup(string) {}
