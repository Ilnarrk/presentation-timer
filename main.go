package main

import (
	"embed"
	"errors"
	"fmt"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"timer/internal/buildinfo"
	"timer/internal/singleinstance"
)

//go:embed all:frontend/dist
var assets embed.FS

// Project sounds are compiled into the application. Add WAV, MP3, or OGG
// files to sounds/ before building; names can also select cue defaults.
//
//go:embed sounds
var projectSounds embed.FS

func main() {
	instanceLock, err := singleinstance.Acquire()
	if errors.Is(err, singleinstance.ErrAlreadyRunning) {
		return
	}
	if err != nil {
		println("Error:", err.Error())
		return
	}
	defer instanceLock.Close()

	app := NewApp(projectSounds)
	appInfo := buildinfo.Get()
	windowTitle := fmt.Sprintf("%s v%s", appInfo.Name, appInfo.Version)

	err = wails.Run(&options.App{
		Title:       windowTitle,
		AlwaysOnTop: true,
		Width:       normalWindowDefaultWidth,
		Height:      normalWindowDefaultHeight,
		MinWidth:    normalWindowMinWidth,
		MinHeight:   normalWindowMinHeight,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 18, B: 24, A: 0},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			Theme:                windows.Dark,
			CustomTheme: &windows.ThemeSettings{
				DarkModeTitleBar:           windows.RGB(15, 18, 24),
				DarkModeTitleBarInactive:   windows.RGB(15, 18, 24),
				DarkModeTitleText:          windows.RGB(255, 255, 255),
				DarkModeTitleTextInactive:  windows.RGB(184, 168, 160),
				DarkModeBorder:             windows.RGB(32, 40, 48),
				DarkModeBorderInactive:     windows.RGB(32, 40, 48),
				LightModeTitleBar:          windows.RGB(255, 255, 255),
				LightModeTitleBarInactive:  windows.RGB(245, 247, 250),
				LightModeTitleText:         windows.RGB(16, 24, 39),
				LightModeTitleTextInactive: windows.RGB(95, 107, 128),
				LightModeBorder:            windows.RGB(214, 220, 229),
				LightModeBorderInactive:    windows.RGB(226, 232, 240),
			},
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
