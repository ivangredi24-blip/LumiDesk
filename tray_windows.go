//go:build windows

package main

import (
	_ "embed"

	"github.com/getlantern/systray"
)

//go:embed assets/lumidesk.ico
var trayIcon []byte

func runTray(app *App) {
	systray.Run(func() {
		systray.SetIcon(trayIcon)
		systray.SetTitle("LumiDesk")
		systray.SetTooltip("LumiDesk — Bluetooth lighting")
		open := systray.AddMenuItem("Open LumiDesk", "Show the LumiDesk dashboard")
		systray.AddSeparator()
		turnOn := systray.AddMenuItem("Turn LEDs on", "Restore the saved colour and brightness")
		turnOff := systray.AddMenuItem("Turn LEDs off", "Turn the LED strip off")
		favorite := systray.AddMenuItem("Apply favourite preset", "Apply your favourite saved colour")
		systray.AddSeparator()
		reconnect := systray.AddMenuItem("Reconnect Bluetooth", "Reconnect to the preferred controller")
		settings := systray.AddMenuItem("Settings", "Open LumiDesk settings")
		systray.AddSeparator()
		exit := systray.AddMenuItem("Exit LumiDesk", "Close LumiDesk")
		go func() {
			for {
				select {
				case <-open.ClickedCh:
					app.OpenMainWindow()
				case <-turnOn.ClickedCh:
					go app.TrayPower(true)
				case <-turnOff.ClickedCh:
					go app.TrayPower(false)
				case <-favorite.ClickedCh:
					go func() {
						if err := app.ApplyFavoritePreset(); err != nil {
							app.log("tray favourite preset failed: %v", err)
						}
					}()
				case <-reconnect.ClickedCh:
					go app.TrayReconnect()
				case <-settings.ClickedCh:
					app.NavigateToSettings()
				case <-exit.ClickedCh:
					app.ExitApplication()
					return
				}
			}
		}()
	}, func() {
		// systray removes the shell icon and native menu resources on return.
	})
}

func stopTray() { systray.Quit() }
