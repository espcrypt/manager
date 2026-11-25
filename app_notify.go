package main

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var notifySeq atomic.Int64

func (a *App) initNotifications() {
	if err := runtime.InitializeNotifications(a.ctx); err != nil {
		a.logf("debug", "Notifications unavailable: %v", err)
		return
	}
	if !runtime.IsNotificationAvailable(a.ctx) {
		a.logf("debug", "Notifications are not available for this build")
		return
	}
	ok, err := runtime.CheckNotificationAuthorization(a.ctx)
	if err == nil && !ok {
		ok, err = runtime.RequestNotificationAuthorization(a.ctx)
	}
	if err != nil || !ok {
		a.logf("warn", "Notifications are not allowed (enable them in System Settings > Notifications)")
	} else {
		a.logf("info", "Notifications enabled")
	}
	runtime.OnNotificationResponse(a.ctx, func(r runtime.NotificationResult) {
		if r.Error != nil {
			return
		}
		page, _ := r.Response.UserInfo["page"].(string)
		a.ShowWindow(page)
	})
	a.notifyReady.Store(true)
}

func (a *App) NotificationsStatus() string {
	if !a.notifyReady.Load() {
		return "unavailable"
	}
	if ok, err := runtime.CheckNotificationAuthorization(a.ctx); err != nil || !ok {
		return "not allowed"
	}
	return "allowed"
}

func (a *App) TestNotification() error {
	if a.NotificationsStatus() != "allowed" {
		return fmt.Errorf("notifications are %s — allow them in your system settings", a.NotificationsStatus())
	}
	a.notifyForce("ESP Crypt connected", "Click to manage your encrypted vaults.", "dashboard")
	return nil
}

func (a *App) notifyForce(title, body, page string) {
	if !a.notifyReady.Load() {
		return
	}
	id := fmt.Sprintf("phm-%d-%d", time.Now().Unix(), notifySeq.Add(1))
	err := runtime.SendNotification(a.ctx, runtime.NotificationOptions{
		ID: id, Title: title, Body: body, Data: map[string]interface{}{"page": page},
	})
	if err != nil {
		a.logf("warn", "Notification failed: %v", err)
	} else {
		a.logf("debug", "Notification shown: %s", title)
	}
}

func (a *App) ShowWindow(page string) {
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
	if page != "" {
		runtime.EventsEmit(a.ctx, "app:navigate", page)
	}
}

func (a *App) QuitApp() {
	runtime.Quit(a.ctx)
}

func (a *App) appMenu() *menu.Menu {
	m := menu.NewMenu()
	app := m.AddSubmenu("ESP Crypt")
	app.AddText("Show window", keys.CmdOrCtrl("0"), func(*menu.CallbackData) { a.ShowWindow("") })
	app.AddSeparator()
	app.AddText("Quit ESP Crypt", keys.CmdOrCtrl("q"), func(*menu.CallbackData) { a.QuitApp() })
	m.Append(menu.EditMenu())
	m.Append(menu.WindowMenu())
	return m
}
