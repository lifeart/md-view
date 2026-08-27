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
	tracef("main entry")

	// --prewarm on|off|status: manage the login agent from a script and exit,
	// the terminal equivalent of the "Keep ready" toggle. SMAppService checks
	// the code signature of the *calling bundle* against the plist it is asked
	// to register, so this can only work from the installed app — which is also
	// why it is a flag on the app rather than a separate helper.
	if mode, ok := prewarmFlag(os.Args[1:]); ok {
		os.Exit(runPrewarmCommand(mode))
	}

	app := NewApp()

	// --hidden: prewarm launch (login item) — boot fully but keep the window
	// off screen until the first document open. Makes the next double-click a
	// warm open (no process spawn, no WebKit init).
	hidden := false
	for _, arg := range os.Args[1:] {
		if arg == "--hidden" {
			hidden = true
		}
	}
	app.hiddenUntilOpen = hidden

	// Windows/Linux file associations arrive via argv; also a dev convenience
	// on macOS (`./md-view path/to/file.md`).
	if initial := initialFileFromArgs(os.Args[1:]); initial != "" {
		app.openPath(initial)
	}

	err := wails.Run(&options.App{
		Title:  "MDv",
		Width:  980,
		Height: 800,
		// macOS convention: closing the window hides the app instead of
		// quitting (Cmd+Q quits). The resident instance makes every subsequent
		// double-click a warm open — tens of ms instead of a full cold launch.
		HideWindowOnClose: true,
		StartHidden:       hidden,
		AssetServer: &assetserver.Options{
			Assets:     assets,
			Middleware: app.assetMiddleware,
		},
		BackgroundColour: &defaultBackground,
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
				tracef("OnFileOpen %s", filePath)
				app.openPath(filePath)
			},
			About: &mac.AboutInfo{
				Title:   "MDv",
				Message: "A fast markdown viewer",
			},
		},
	})
	if err != nil {
		log.Fatalf("md-view: %v", err)
	}
}
