package main

import (
	"embed"
	"io/fs"
	"log"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:assets
var assets embed.FS

func main() {
	frontend, err := fs.Sub(assets, "assets/dist")
	if err != nil {
		log.Fatal(err)
	}
	app := newApp()
	appMenu := menu.NewMenu()
	if runtime.GOOS == "darwin" {
		appMenu.Append(menu.AppMenu())
	}
	appMenu.Append(menu.EditMenu())
	if err := wails.Run(&options.App{
		Title: "ModbusMaster — Go", Width: 1280, Height: 800,
		MinWidth: 900, MinHeight: 600, Menu: appMenu,
		AssetServer: &assetserver.Options{Assets: frontend},
		OnStartup:   app.startup, OnShutdown: app.shutdown, Bind: []interface{}{app},
	}); err != nil {
		log.Fatal(err)
	}
}
