package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	// Windows/Linux file associations arrive via argv; also a dev convenience
	// on macOS (`./md-view path/to/file.md`).
	if initial := initialFileFromArgs(os.Args[1:]); initial != "" {
		app.openPath(initial)
	}

	err := wails.Run(&options.App{
		Title:  "md-view",
		Width:  980,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets:     assets,
			Middleware: app.assetMiddleware,
		},
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "com.wails.md-view",
			OnSecondInstanceLaunch: app.onSecondInstanceLaunch,
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		Mac: &mac.Options{
			// macOS file associations: fired at launch (possibly before the
			// frontend exists — OpenPath buffers) and while running.
			OnFileOpen: func(filePath string) {
				app.openPath(filePath)
			},
			About: &mac.AboutInfo{
				Title:   "md-view",
				Message: "A fast markdown viewer",
			},
		},
	})
	if err != nil {
		log.Fatalf("md-view: %v", err)
	}
}
