package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lumidesk/internal/control"
	"lumidesk/internal/domain"
	"lumidesk/internal/persistence"
	"lumidesk/internal/platform"
)

func preLoginStartupConfig(args []string) (string, bool) {
	var configDir string
	requested := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--prelogin-lights":
			requested = true
		case args[i] == "--config-dir" && i+1 < len(args):
			i++
			configDir = args[i]
		case strings.HasPrefix(args[i], "--config-dir="):
			configDir = strings.TrimPrefix(args[i], "--config-dir=")
		}
	}
	return configDir, requested
}

func runPreLoginStartup(configDir string) error {
	release, alreadyRunning, err := platform.AcquirePreLoginAgent()
	if err != nil {
		return fmt.Errorf("acquire pre-login BLE task lock: %w", err)
	}
	if alreadyRunning {
		return nil
	}
	defer release()
	if platform.DesktopIsRunning() {
		return nil
	}

	if strings.TrimSpace(configDir) == "" {
		root, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf("locate the user settings folder: %w", err)
		}
		configDir = filepath.Join(root, "LumiDesk")
	}
	store, err := persistence.New(configDir)
	if err != nil {
		return err
	}
	settings, err := store.LoadSettings()
	if err != nil {
		return err
	}
	if !settings.PreLoginLights {
		return nil
	}
	presets, err := store.LoadPresets()
	if err != nil {
		return err
	}
	targets := preLoginTargets(settings)
	if len(targets) == 0 {
		return errors.New("no controllers are selected for pre-login lighting")
	}

	logFile, err := os.OpenFile(filepath.Join(configDir, "prelogin-startup.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open pre-login startup log: %w", err)
	}
	defer logFile.Close()
	logger := log.New(logFile, "", log.LstdFlags)

	// Run only the chosen controllers in this short-lived headless process. The
	// normal desktop app reconnects after sign-in and remains the long-lived BLE
	// owner. AutoReconnect is forced on here so a slow Bluetooth stack can settle
	// during boot, without changing the user's saved preference.
	bootSettings := cloneBootSettings(settings, targets)
	bootSettings.StartupLightsOn = true
	bootSettings.AutoReconnect = true
	addresses := make(map[string]bool, len(targets))
	for _, device := range targets {
		addresses[domain.NormalizeAddress(device.Address)] = true
	}
	manager := control.NewFleetManager(&control.LotusTransport{}, bootSettings, presets, control.Callbacks{
		Log: func(format string, args ...any) { logger.Printf(format, args...) },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	manager.Start(ctx)
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer closeCancel()
		if err := manager.Close(closeCtx); err != nil {
			logger.Printf("BLE cleanup after pre-login startup: %v", err)
		}
	}()
	logger.Printf("pre-login light startup started for %d selected controller(s)", len(targets))

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		connected := manager.Snapshot().ConnectedDevices
		seen := make(map[string]bool, len(connected))
		for _, device := range connected {
			seen[domain.NormalizeAddress(device.Address)] = true
		}
		allConnected := true
		for address := range addresses {
			if !seen[address] {
				allConnected = false
				break
			}
		}
		if allConnected {
			// The manager marks the GATT link connected immediately before it sends
			// the restore color, brightness, and power commands.
			settle := time.Duration(len(targets))*500*time.Millisecond + 500*time.Millisecond
			timer := time.NewTimer(settle)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("wait for startup light commands: %w", ctx.Err())
			case <-timer.C:
			}
			settings.LastColor, settings.LastBrightness = preLoginStartupColor(settings, presets)
			settings.LastPowerOn = true
			if err := store.SaveSettings(settings); err != nil {
				logger.Printf("could not persist the pre-login light state: %v", err)
			}
			logger.Printf("pre-login light startup completed; closing BLE links for the desktop app")
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for selected controllers during boot: %w (last error: %s)", ctx.Err(), manager.Snapshot().LastError)
		case <-ticker.C:
		}
	}
}

func preLoginTargets(settings domain.Settings) []domain.Device {
	wanted := make(map[string]bool)
	if settings.SyncDevices {
		for _, device := range settings.SavedDevices {
			wanted[domain.NormalizeAddress(device.Address)] = true
		}
	} else {
		for _, address := range settings.SelectedDevices {
			wanted[domain.NormalizeAddress(address)] = true
		}
	}
	targets := make([]domain.Device, 0, len(wanted))
	for _, device := range settings.SavedDevices {
		if wanted[domain.NormalizeAddress(device.Address)] {
			targets = append(targets, device)
		}
	}
	return targets
}

func cloneBootSettings(settings domain.Settings, targets []domain.Device) domain.Settings {
	settings.SavedDevices = append([]domain.Device{}, targets...)
	settings.SelectedDevices = make([]string, 0, len(targets))
	for _, device := range targets {
		settings.SelectedDevices = append(settings.SelectedDevices, domain.NormalizeAddress(device.Address))
	}
	settings.SyncDevices = true
	if len(targets) > 0 {
		settings.PreferredDevice = targets[0]
	}
	return settings
}

func preLoginStartupColor(settings domain.Settings, presets []domain.Preset) (domain.Color, int) {
	if settings.StartupColorMode == "last" {
		if settings.RestoreLastColor {
			return settings.LastColor, settings.LastBrightness
		}
		return domain.Color{R: 255, G: 255, B: 255}, settings.LastBrightness
	}
	for _, preset := range presets {
		if (settings.StartupColorMode == "favorite" && preset.ID == settings.FavoritePresetID) ||
			(settings.StartupColorMode == "preset" && preset.ID == settings.StartupPresetID) {
			return preset.Color, preset.Brightness
		}
	}
	if settings.StartupColorMode == "preset" {
		return domain.Color{R: 255, G: 255, B: 255}, settings.LastBrightness
	}
	return settings.LastColor, settings.LastBrightness
}
