package main

import (
	"embed"
	"flag"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend
var assets embed.FS

// Wails v2 runs one window per process, so a console in its own OS window —
// resizable, movable, on a second monitor — has to be a second process. The
// binary launches itself with these flags; see OpenConsoleWindow.
var (
	consoleServer = flag.String("console", "", "run as a console window for this server id")
	consolePanel  = flag.String("panel", "", "the panel that server is on")
	consoleLabel  = flag.String("label", "", "what to put in the title bar")
)

func main() {
	flag.Parse()

	app := NewApp()

	title := "Pterodactyl Manager"
	width, height := 1024, 768

	if *consoleServer != "" {
		// Console mode. The frontend asks for WindowMode at boot and hides
		// everything that is not the console; this side just points the app at
		// the right server and gives the window a sensible shape for a log.
		app.consoleOnly = true
		app.consoleServerID = *consoleServer
		app.consolePanelName = *consolePanel

		title = "Console"
		if *consoleLabel != "" {
			title = "Console — " + *consoleLabel
		}
		width, height = 960, 600
	}

	err := wails.Run(&options.App{
		Title:  title,
		Width:  width,
		Height: height,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 8, G: 12, B: 22, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
