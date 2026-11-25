package main

import (
	"embed"
	"os"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()
	app.loadSettings()
	app.handleArgs(os.Args[1:])

	var appMenu *menu.Menu
	if goruntime.GOOS == "darwin" {
		appMenu = app.appMenu()
	}

	err := wails.Run(&options.App{
		Title:     "ESP Crypt",
		Width:     1280,
		Height:    820,
		MinWidth:  980,
		MinHeight: 640,
		Menu:      appMenu,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 26, G: 26, B: 26, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,

		HideWindowOnClose: app.GetSettings().RunInBackground,

		Mac: &mac.Options{OnFileOpen: app.openVaultFromOS},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: instanceID,
			OnSecondInstanceLaunch: func(d options.SecondInstanceData) {
				app.ShowWindow("")
				app.handleArgs(d.Args)
			},
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
