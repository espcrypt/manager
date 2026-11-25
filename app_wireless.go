package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"espcrypt/internal/espcrypt"
)

type WirelessPairing struct {
	espcrypt.Pairing
	Chip   string `json:"chip"`
	LastIP string `json:"lastIp,omitempty"`

	WiFi bool `json:"wifi"`
}

type WirelessInfo struct {
	DeviceID  string                `json:"deviceId"`
	Label     string                `json:"label"`
	Transport string                `json:"transport"`
	Supported bool                  `json:"supported"`
	Radio     *espcrypt.RadioStatus `json:"radio"`
	Paired    bool                  `json:"paired"`
	PairedAs  string                `json:"pairedAs"`
	Name      string                `json:"name"`
}

type WirelessConfig struct {
	Features map[string]bool `json:"features"`
	SSID     *string         `json:"ssid"`
	Password *string         `json:"password"`
	NewName  bool            `json:"newName"`
}

const (
	remoteScanInterval = 5 * time.Second

	remoteLostAfter  = 30 * time.Second
	remotePrefixWiFi = "wifi://"
)

func isRemotePort(port string) bool {
	return strings.HasPrefix(port, remotePrefixWiFi)
}

func transportOf(port string) string {
	switch {
	case strings.HasPrefix(port, remotePrefixWiFi):
		return "wifi"
	}
	return "usb"
}

func (a *App) entryByDeviceID(deviceID string) *vaultDeviceConnection {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range a.vaultdevs {
		if strings.EqualFold(e.Info.Serial, deviceID) {
			return e
		}
	}
	return nil
}

func (a *App) pairing(deviceID string) (WirelessPairing, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.settings.Wireless[strings.ToUpper(deviceID)]
	return p, ok
}

func (a *App) savePairing(p WirelessPairing) {
	a.mu.Lock()
	if a.settings.Wireless == nil {
		a.settings.Wireless = map[string]WirelessPairing{}
	}
	a.settings.Wireless[strings.ToUpper(p.DeviceID)] = p
	a.mu.Unlock()
	_ = a.persistSettings()
}

func (a *App) usbEntry(id string) (*vaultDeviceConnection, error) {
	a.mu.Lock()
	e := a.vaultdevs[id]
	a.mu.Unlock()
	if e == nil {
		return nil, errors.New("connect this ESP Crypt first")
	}
	if isRemotePort(e.Info.Port) {
		return nil, errors.New("wireless settings can only be changed over USB: plug the ESP32 in")
	}
	return e, nil
}

func (a *App) GetWireless(id string) (WirelessInfo, error) {
	a.mu.Lock()
	e := a.vaultdevs[id]
	a.mu.Unlock()
	if e == nil {
		return WirelessInfo{}, errors.New("connect this ESP Crypt first")
	}
	info := WirelessInfo{DeviceID: e.Info.Serial, Label: e.Info.Label, Transport: transportOf(e.Info.Port)}
	if p, ok := a.pairing(e.Info.Serial); ok {
		info.Paired, info.PairedAs, info.Name = true, p.KeyID, p.Name
	}
	if info.Transport != "usb" {
		st, err := e.Device.CachedStatus(5 * time.Second)
		if err != nil {
			return info, err
		}
		info.Supported, info.Radio = st.Radio != nil, st.Radio
		return info, nil
	}
	var r espcrypt.RadioStatus
	if err := e.Device.Command("radio_get", "", "", "", "", &r); err != nil {
		if strings.Contains(err.Error(), "unknown command") {
			return info, nil
		}
		return info, err
	}
	info.Supported, info.Radio, info.Name = true, &r, r.Name

	if info.Paired {
		found := false
		for _, c := range r.Clients {
			found = found || c.KeyID == info.PairedAs
		}
		if !found {
			a.forgetPairing(e.Info.Serial)
			info.Paired, info.PairedAs = false, ""
		} else if p, ok := a.pairing(e.Info.Serial); ok {
			p.Name, p.WiFi = r.Name, r.Features["wifiMirror"]
			if r.IP != "" {
				p.LastIP = r.IP
			}
			a.savePairing(p)
		}
	}
	return info, nil
}

func (a *App) SetWireless(id string, cfg WirelessConfig) error {
	e, err := a.usbEntry(id)
	if err != nil {
		return err
	}
	body := map[string]any{"features": cfg.Features}
	if cfg.SSID != nil {
		body["ssid"] = strings.TrimSpace(*cfg.SSID)
	}
	if cfg.Password != nil {
		body["password"] = *cfg.Password
	}
	if cfg.NewName {
		body["newName"] = true
	}
	b, _ := json.Marshal(body)
	runtime.EventsEmit(a.ctx, "wireless:confirm", true)
	defer runtime.EventsEmit(a.ctx, "wireless:confirm", false)
	a.logf("info", "[Wireless] Saving settings on %s; press BOOT on the board if it blinks twice", e.Info.Label)
	if err := e.Device.Command("radio_set", "", string(b), "", "", nil); err != nil {
		return err
	}
	a.logf("info", "[Wireless] %s saved its wireless settings and restarts", e.Info.Label)

	a.dropEntry(id, e)
	return nil
}

func (a *App) PairWireless(id string) error {
	e, err := a.usbEntry(id)
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	label := "ESP Crypt"
	if host != "" {
		label = strings.TrimSuffix(host, ".local")
		if len(label) > 31 {
			label = label[:31]
		}
	}

	if old, ok := a.pairing(e.Info.Serial); ok {
		_ = e.Device.Command("radio_unpair", old.KeyID, "", "", "", nil)
	}
	runtime.EventsEmit(a.ctx, "wireless:confirm", true)
	defer runtime.EventsEmit(a.ctx, "wireless:confirm", false)
	var p espcrypt.Pairing
	if err := e.Device.Command("radio_pair", label, "", "", "", &p); err != nil {
		return err
	}
	wp := WirelessPairing{Pairing: p, Chip: e.Info.Chip}
	a.savePairing(wp)
	a.logf("info", "[Wireless] This computer is paired with %s (%s)", e.Info.Label, p.Name)
	return nil
}

func (a *App) UnpairWireless(id, keyID string) error {
	a.mu.Lock()
	e := a.vaultdevs[id]
	a.mu.Unlock()
	if e == nil {
		return errors.New("connect this ESP Crypt first")
	}
	own, hasOwn := a.pairing(e.Info.Serial)
	if keyID == "" && hasOwn {
		keyID = own.KeyID
	}
	if !isRemotePort(e.Info.Port) && keyID != "" {
		if err := e.Device.Command("radio_unpair", keyID, "", "", "", nil); err != nil && !strings.Contains(err.Error(), "unknown pairing") {
			return err
		}
		a.dropEntry(id, e)
	}
	if hasOwn && own.KeyID == keyID {
		a.forgetPairing(e.Info.Serial)
	}
	return nil
}

func (a *App) forgetPairing(deviceID string) {
	a.mu.Lock()
	delete(a.settings.Wireless, strings.ToUpper(deviceID))
	a.mu.Unlock()
	_ = a.persistSettings()
}

func (a *App) dropEntry(id string, e *vaultDeviceConnection) {
	a.mu.Lock()
	if a.vaultdevs[id] != e {
		a.mu.Unlock()
		return
	}
	delete(a.vaultdevs, id)
	wasActive := a.activeVaultID == id
	if wasActive {
		a.activeVaultID = ""
	}
	a.mu.Unlock()
	if !isRemotePort(e.Info.Port) {
		a.lockVaultsOnRemoval(e.Info)
	}
	e.Device.Retire()
	runtime.EventsEmit(a.ctx, "devices:changed", nil)
	runtime.EventsEmit(a.ctx, "device:changed", false)
}

func (a *App) statusLoop() {
	for {
		if a.ctx != nil {
			select {
			case <-a.ctx.Done():
				return
			default:
			}
		}
		if a.romBusy.Load() == 0 {
			a.mu.Lock()
			entries := make([]*vaultDeviceConnection, 0, len(a.vaultdevs))
			for _, e := range a.vaultdevs {
				entries = append(entries, e)
			}
			a.mu.Unlock()
			for _, e := range entries {
				_, _ = e.Device.CachedStatus(2 * time.Second)
				a.keepUnlocked(e)
			}
		}
		time.Sleep(3 * time.Second)
	}
}

func (a *App) remoteLoop() {
	failures := map[string]int{}
	for {
		if a.ctx != nil {
			select {
			case <-a.ctx.Done():
				return
			default:
			}
		}
		if a.romBusy.Load() == 0 && a.GetSettings().AutoConnect {
			a.remoteOnce(failures)
		}
		time.Sleep(remoteScanInterval)
	}
}

func (a *App) remoteOnce(failures map[string]int) {

	a.mu.Lock()
	var remote []struct {
		id string
		e  *vaultDeviceConnection
	}
	for id, e := range a.vaultdevs {
		if isRemotePort(e.Info.Port) {
			remote = append(remote, struct {
				id string
				e  *vaultDeviceConnection
			}{id, e})
		}
	}
	pairings := make([]WirelessPairing, 0, len(a.settings.Wireless))
	for _, p := range a.settings.Wireless {
		pairings = append(pairings, p)
	}
	a.mu.Unlock()
	for _, r := range remote {
		if _, err := r.e.Device.CachedStatus(4 * time.Second); err == nil {
			delete(failures, r.id)
			continue
		}
		if last := r.e.Device.LastStatusAt(); !last.IsZero() && time.Since(last) < remoteLostAfter {
			continue
		}
		failures[r.id]++
		if failures[r.id] >= 2 {
			a.logf("warn", "[Wireless] Lost %s (no answer for %s)", r.e.Info.Label, remoteLostAfter)
			a.dropEntry(r.id, r.e)
			delete(failures, r.id)
		}
	}

	var wifiBoards []espcrypt.WiFiBoard
	scanned := false
	for _, p := range pairings {

		if a.entryByDeviceID(p.DeviceID) != nil {
			continue
		}
		if !scanned {
			wifiBoards, _ = espcrypt.DiscoverWiFi(1500 * time.Millisecond)
			scanned = true
		}
		var dev *espcrypt.Device
		for _, b := range wifiBoards {
			if strings.EqualFold(b.ID, p.DeviceID) {
				dev = &espcrypt.Device{Port: remotePrefixWiFi + b.IP, Link: &espcrypt.WiFiDialer{Pairing: p.Pairing, Host: b.IP}}
			}
		}
		if dev == nil {
			continue
		}
		st, err := dev.Status()
		if err != nil || !espcrypt.SupportedChip(st.Chip) {
			dev.Close()
			if errors.Is(err, espcrypt.ErrUnknownClient) {
				a.logf("warn", "[Wireless] %s no longer knows this computer; pair it again over USB", p.Name)
				a.forgetPairing(p.DeviceID)
			}
			continue
		}
		if a.entryByDeviceID(p.DeviceID) != nil {
			dev.Close()
			continue
		}
		a.deviceSwitchMu.Lock()
		err = a.registerVaultDevice(dev, st)
		a.deviceSwitchMu.Unlock()
		if err != nil {
			a.logf("warn", "[Wireless] %s: %v", p.Name, err)
		} else {
			a.logf("info", "[Wireless] Connected to %s over Wi-Fi (mirror only)", p.Name)
		}
	}
}
