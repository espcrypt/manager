package main

import (
	"errors"
	"sort"
	"strings"

	"espcrypt/internal/esp"
	"espcrypt/internal/passkey"
)

type PasskeyDeviceInfo struct {
	Port   string         `json:"port"`
	Status passkey.Status `json:"status"`
	Error  string         `json:"error,omitempty"`
}

func (a *App) ListPasskeyDevices() []PasskeyDeviceInfo {
	out := []PasskeyDeviceInfo{}
	if a.romBusy.Load() != 0 {
		return out
	}
	ports, err := esp.ListPorts()
	if err != nil {
		return out
	}
	for _, p := range ports {
		if !p.Passkey {
			continue
		}
		info := PasskeyDeviceInfo{Port: p.Name}
		st, err := a.passkeyOn(p.Name).Status()
		if err != nil {
			info.Error = err.Error()
		}
		info.Status = st
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

func (a *App) passkeyOn(port string) *passkey.Device {
	if id, ok := a.vaultDeviceForPort(port); ok {
		a.mu.Lock()
		entry := a.vaultdevs[id]
		a.mu.Unlock()
		if entry != nil {
			return &passkey.Device{Port: port, Via: entry.Device.Command}
		}
	}
	return &passkey.Device{Port: port}
}

func (a *App) passkeyDevice(port string) (*passkey.Device, error) {
	if strings.TrimSpace(port) == "" {
		return nil, errors.New("choose a security key")
	}
	return a.passkeyOn(port), nil
}

func (a *App) ListPasskeys(port string) ([]passkey.Credential, error) {
	d, err := a.passkeyDevice(port)
	if err != nil {
		return nil, err
	}
	return d.List()
}

func (a *App) DeletePasskey(port, id string) error {
	d, err := a.passkeyDevice(port)
	if err != nil {
		return err
	}
	if err := d.Delete(id); err != nil {
		return err
	}
	a.logf("warn", "[Passkey] Passkey deleted from the device on %s", port)
	return nil
}

func (a *App) SetPasskeyPIN(port, pin string) error {
	d, err := a.passkeyDevice(port)
	if err != nil {
		return err
	}
	if err := d.SetPIN(pin); err != nil {
		return err
	}
	a.logf("info", "[Passkey] PIN set on %s", port)
	return nil
}

func (a *App) ChangePasskeyPIN(port, oldPIN, newPIN string) error {
	d, err := a.passkeyDevice(port)
	if err != nil {
		return err
	}
	if err := d.ChangePIN(oldPIN, newPIN); err != nil {
		return err
	}
	a.logf("info", "[Passkey] PIN changed on %s", port)
	return nil
}

func (a *App) SetPasskeyTouch(port, mode string) error {
	d, err := a.passkeyDevice(port)
	if err != nil {
		return err
	}
	if err := d.SetTouch(mode); err != nil {
		return err
	}
	a.logf("info", "[Passkey] Touch confirmation on %s: %s", port, mode)
	return nil
}

func (a *App) ResetPasskeyDevice(port string) error {
	d, err := a.passkeyDevice(port)
	if err != nil {
		return err
	}
	if err := d.Reset(); err != nil {
		return err
	}
	a.logf("warn", "[Passkey] All passkeys and the PIN were erased on %s", port)
	return nil
}

func (a *App) WinkPasskeyDevice(port string) error {
	d, err := a.passkeyDevice(port)
	if err != nil {
		return err
	}
	return d.Wink()
}
