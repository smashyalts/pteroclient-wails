package main

import (
	"embed"
	"flag"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// What the window actually loads, rather than the whole frontend directory.
//
// `all:frontend` swept in frontend/node_modules whenever it happened to be on
// disk — and `wails build` puts it there itself, by running npm install when
// package.json changes. That is 19 MB of dev dependencies, esbuild's own
// executable among them, baked into a binary that is shipped to people. The
// difference showed up as a release asset that was 31 MB one time and 14 MB
// the next, for the same code.
//
// index.html loads ./*.js and ./src/style.css and nothing else local, so this
// is the whole of what is served. Anything new that the window fetches has to
// be added here, which is the cost of not shipping a build tool inside the
// app.
//
//go:embed frontend/index.html
//go:embed frontend/*.js
//go:embed all:frontend/src
//go:embed all:frontend/wailsjs
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
		Windows: &windows.Options{
			// Named after the app, not after the executable file. Wails
			// defaults this to %APPDATA%\<exe name>, so the browser's data
			// landed in a folder literally called "pteroclient-wails.exe" —
			// and renaming or re-downloading the binary made a second one
			// beside it, with none of the first one's state.
			WebviewUserDataPath: webviewDataPath(),
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

// webviewDataPath keeps the embedded browser's own storage beside the app's
// config rather than in a folder named after the exe. Empty on failure, which
// leaves Wails to its default rather than failing to start.
func webviewDataPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pteroclient", "webview")
}
