//go:build !windows

package serialx

import "go.bug.st/serial"

func cancelIO(serial.Port) {}
