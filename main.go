package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

// Project sounds are compiled into the application. Add WAV, MP3, or OGG
// files to sounds/ before building; names can also select cue defaults.
//
//go:embed sounds
var projectSounds embed.FS

func main() {
	app := NewApp(projectSounds)

	err := wails.Run(&options.App{
		Title:     "Таймер докладов",
		Width:     960,
		Height:    720,
		MinWidth:  820,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 18, B: 24, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
