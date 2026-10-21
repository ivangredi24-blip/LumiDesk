package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log"
	"os"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"lumidesk/internal/logging"
	"lumidesk/internal/persistence"
	"lumidesk/internal/platform"
)

//go:embed all:frontend/dist
var embeddedFrontend embed.FS

func main() {
	if configDir, requested := preLoginStartupConfig(os.Args[1:]); requested {
		if err := runPreLoginStartup(configDir); err != nil {
			log.Printf("pre-login LED startup failed: %v", err)
			os.Exit(1)
		}
		return
	}
	releaseDesktop, err := platform.MarkDesktopRunning()
	if err != nil {
		log.Printf("could not create cross-session desktop marker: %v", err)
	} else {
		defer releaseDesktop()
	}
	releaseMutex, alreadyRunning, err := platform.AcquireSingleInstance(`Local\LumiDesk.SingleInstance`)
	if err != nil {
		log.Fatalf("LumiDesk could not acquire its single-instance lock: %v", err)
	}
	if alreadyRunning {
		platform.NotifyAlreadyRunning()
		return
	}
	defer releaseMutex()

	store, err := persistence.InUserConfig()
	if err != nil {
		log.Fatalf("LumiDesk could not open its settings folder: %v", err)
	}
	logger, err := logging.New(userLogDir())
	if err != nil {
		log.Printf("Could not open LumiDesk diagnostic log: %v", err)
	}
	app, err := NewApp(store, logger, platform.WindowsStartup{})
	if err != nil {
		log.Fatalf("LumiDesk could not load its local data: %v", err)
	}
	if app.settingsCopy().PreLoginLights {
		waitCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		if err := platform.WaitForPreLoginAgent(waitCtx); err != nil {
			app.log("pre-login LED task did not finish before the desktop app started: %v", err)
		}
		cancel()
	}

	stopShutdownMonitor, err := platform.StartSessionMonitor(app.HandleShutdownQuery, app.HandleShutdownEnd)
	if err != nil {
		app.log("Windows shutdown notifications are unavailable: %v", err)
	} else {
		defer stopShutdownMonitor()
	}
	go runTray(app)

	assets, err := fs.Sub(embeddedFrontend, "frontend/dist")
	if err != nil {
		log.Fatalf("LumiDesk UI assets are missing: %v", err)
	}
	settings := app.settingsCopy()
	startHidden := isStartupLaunch() && settings.MinimizeToTray
	appOptions := &options.App{
		Title:             "LumiDesk",
		Width:             settings.WindowWidth,
		Height:            settings.WindowHeight,
		MinWidth:          900,
		MinHeight:         660,
		StartHidden:       startHidden,
		HideWindowOnClose: false,
		BackgroundColour:  &options.RGBA{R: 13, G: 16, B: 22, A: 1},
		AssetServer:       &assetserver.Options{Assets: assets},
		OnStartup:         app.startupApp,
		OnShutdown:        app.shutdownApp,
		OnBeforeClose:     app.beforeClose,
		Bind:              []interface{}{app},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
			Theme:                windows.Dark,
		},
	}
	if err := wails.Run(appOptions); err != nil && !errors.Is(err, os.ErrClosed) {
		app.log("Wails terminated with an error: %v", err)
		log.Printf("LumiDesk could not start: %v", err)
	}
}
