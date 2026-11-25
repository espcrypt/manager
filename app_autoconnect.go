package main

import (
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"espcrypt/internal/esp"
	"espcrypt/internal/espcrypt"
)

const autoConnectInterval = 3 * time.Second

const vanishScans = 3

var missingScans = map[string]int{}

func (a *App) autoConnectLoop() {
	failed := map[string]time.Time{}
	for {
		if a.ctx != nil {
			select {
			case <-a.ctx.Done():
				return
			default:
			}
		}
		if a.GetSettings().AutoConnect && a.romBusy.Load() == 0 {
			a.autoConnectOnce(failed)
		}

		if a.romBusy.Load() == 0 {
			a.vm.EnforceTwoKey(a.ctxOrBg())
			a.restorePendingPasskeys()
			a.mirrorTick()
		}
		time.Sleep(autoConnectInterval)
	}
}

func (a *App) autoConnectOnce(failed map[string]time.Time) {
	ports, err := esp.ListPorts()
	if err != nil {
		return
	}
	present := map[string]bool{}
	for _, p := range ports {
		present[strings.ToUpper(p.Name)] = true
	}
	a.dropVanishedDevices(present)
	a.mu.Lock()
	for port := range a.userDisconnected {
		if !present[port] {
			delete(a.userDisconnected, port)
		}
	}
	a.mu.Unlock()

	type found struct {
		dev *espcrypt.Device
		st  espcrypt.DeviceStatus
	}
	var answered []found
	for _, p := range ports {
		if !p.USBJTAG && !p.Vault {
			continue
		}
		a.mu.Lock()
		skip := a.userDisconnected[strings.ToUpper(p.Name)]
		a.mu.Unlock()
		if skip {
			continue
		}
		if _, registered := a.vaultDeviceForPort(p.Name); registered {
			delete(failed, p.Name)
			continue
		}

		if t, ok := failed[p.Name]; ok && time.Since(t) < 4*time.Second {
			continue
		}
		if a.romBusy.Load() != 0 {
			break
		}

		probe := &espcrypt.Device{Port: p.Name}
		st, err := probe.Status()
		if err != nil || (!espcrypt.SupportedChip(st.Chip)) {
			probe.Close()
			failed[p.Name] = time.Now()
			continue
		}
		delete(failed, p.Name)
		answered = append(answered, found{probe, st})
	}

	preferred := a.GetSettings().ActiveVaultID
	sort.SliceStable(answered, func(i, j int) bool {
		return vaultDeviceID(answered[i].st, answered[i].dev.Port) == preferred
	})
	for _, f := range answered {
		if a.romBusy.Load() != 0 {
			f.dev.Close()
			continue
		}
		a.deviceSwitchMu.Lock()
		err := a.registerVaultDevice(f.dev, f.st)
		a.deviceSwitchMu.Unlock()
		if err != nil {
			a.logf("warn", "Auto-connect on %s failed: %v", f.dev.Port, err)
		}
	}
}

func (a *App) dropVanishedDevices(present map[string]bool) {
	type gone struct {
		id    string
		entry *vaultDeviceConnection
	}
	var removed []gone
	activeGone := false
	a.mu.Lock()
	for id, entry := range a.vaultdevs {
		port := strings.ToUpper(entry.Info.Port)
		if present[port] || isRemotePort(entry.Info.Port) {
			delete(missingScans, port)
			continue
		}
		missingScans[port]++
		if missingScans[port] < vanishScans {
			continue
		}
		delete(missingScans, port)
		delete(a.vaultdevs, id)
		if a.activeVaultID == id {
			a.activeVaultID = ""
			activeGone = true
		}
		removed = append(removed, gone{id, entry})
	}
	a.mu.Unlock()
	if len(removed) == 0 {
		return
	}
	for _, g := range removed {
		a.lockVaultsOnRemoval(g.entry.Info)
	}
	for _, g := range removed {
		g.entry.Device.Retire()
		a.logf("warn", "%s was unplugged from %s", g.entry.Info.Label, g.entry.Info.Port)
	}
	runtime.EventsEmit(a.ctx, "devices:changed", nil)
	runtime.EventsEmit(a.ctx, "device:changed", !activeGone)
}
