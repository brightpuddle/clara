package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	appMenu := menu.NewMenu()
	fileMenu := appMenu.AddSubmenu("File")
	fileMenu.AddText("Close Window", keys.CmdOrCtrl("w"), func(_ *menu.CallbackData) {
		// window hide is handled by the Wails runtime
	})
	editMenu := appMenu.AddSubmenu("Edit")
	editMenu.AddText("Select All", keys.CmdOrCtrl("a"), nil)
	editMenu.AddText("Copy", keys.CmdOrCtrl("c"), nil)
	editMenu.AddText("Paste", keys.CmdOrCtrl("v"), nil)

	err := wails.Run(&options.App{
		Title:             "Clara",
		Width:             1280,
		Height:            820,
		MinWidth:          800,
		MinHeight:         600,
		HideWindowOnClose: true,
		Menu:              appMenu,
		AssetServer:       &assetserver.Options{Assets: assets},
		BackgroundColour:  &options.RGBA{R: 30, G: 30, B: 30, A: 1},
		OnStartup:         app.startup,
		OnShutdown:        app.shutdown,
		Bind:              []interface{}{app},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHiddenInset(),
			Appearance:           mac.NSAppearanceNameDarkAqua,
			WebviewIsTransparent: true,
			WindowIsTranslucent:  false,
			About: &mac.AboutInfo{
				Title:   "Clara",
				Message: "A local-first productivity HUD",
			},
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
