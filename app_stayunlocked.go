package main

import (
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var pins = struct {
	sync.Mutex
	m      map[string]string
	failed map[string]bool
}{m: map[string]string{}, failed: map[string]bool{}}

func (a *App) rememberPIN(deviceID, pin string) {
	if deviceID == "" || !a.GetSettings().RememberPIN {
		return
	}
	pins.Lock()
	pins.m[strings.ToUpper(deviceID)] = pin
	delete(pins.failed, strings.ToUpper(deviceID))
	pins.Unlock()
}

func (a *App) forgetPIN(deviceID string) {
	pins.Lock()
	delete(pins.m, strings.ToUpper(deviceID))
	pins.Unlock()
}

func (a *App) keepUnlocked(e *vaultDeviceConnection) {
	id := strings.ToUpper(e.Info.Serial)
	pins.Lock()
	pin, ok := pins.m[id]
	failed := pins.failed[id]
	pins.Unlock()
	if !ok || failed {
		return
	}
	st, err := e.Device.CachedStatus(2 * time.Second)
	if err != nil || st.Busy || st.Unlocked || !st.Initialized {
		return
	}
	if err := e.Device.Unlock(pin); err != nil {

		pins.Lock()
		pins.failed[id] = true
		pins.Unlock()
		a.logf("warn", "%s locked itself and the remembered passphrase no longer opens it: %v", e.Info.Label, err)
		return
	}
	why := "restart, sleep or USB reset"
	if st.ResetReason != "" {
		why = "restarted " + time.Duration(st.Uptime*float64(time.Second)).Round(time.Second).String() + " ago, reason: " + st.ResetReason
	}
	a.logf("info", "%s had locked itself (%s); unlocked it again", e.Info.Label, why)
	runtime.EventsEmit(a.ctx, "devices:changed", nil)
	runtime.EventsEmit(a.ctx, "device:changed", true)
}
