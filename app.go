package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"lumidesk/internal/control"
	"lumidesk/internal/domain"
	"lumidesk/internal/logging"
	"lumidesk/internal/persistence"
	"lumidesk/internal/platform"
)

type Snapshot struct {
	State             control.State   `json:"state"`
	Settings          domain.Settings `json:"settings"`
	Presets           []domain.Preset `json:"presets"`
	StartupAvailable  bool            `json:"startupAvailable"`
	PreLoginAvailable bool            `json:"preLoginAvailable"`
	RecoveryWarnings  []string        `json:"recoveryWarnings"`
}

type App struct {
	ctx     context.Context
	store   *persistence.Store
	logger  *logging.Logger
	startup platform.StartupRegistrar

	manager control.ControllerManager

	mu            sync.RWMutex
	settings      domain.Settings
	presets       []domain.Preset
	forceExit     atomic.Bool
	systemEnding  atomic.Bool
	shutdownWasOn atomic.Bool
}

func NewApp(store *persistence.Store, logger *logging.Logger, startup platform.StartupRegistrar) (*App, error) {
	settings, err := store.LoadSettings()
	if err != nil {
		return nil, err
	}
	presets, err := store.LoadPresets()
	if err != nil {
		return nil, err
	}
	app := &App{store: store, logger: logger, startup: startup, settings: settings, presets: presets}
	app.manager = control.NewFleetManager(&control.LotusTransport{}, settings, presets, control.Callbacks{
		State: func(state control.State) {
			if app.ctx != nil {
				runtime.EventsEmit(app.ctx, "lumidesk:state", state)
			}
		},
		Settings: app.persistManagerSettings,
		Log:      app.log,
	})
	for _, warning := range store.RecoveryWarnings() {
		app.log("local data recovery: %s", warning)
	}
	return app, nil
}

func (a *App) startupApp(ctx context.Context) {
	a.ctx = ctx
	settings := a.settingsCopy()
	if settings.Theme == "light" {
		runtime.WindowSetLightTheme(ctx)
	}
	if settings.WindowPositionSet {
		runtime.WindowSetPosition(ctx, settings.WindowX, settings.WindowY)
	}
	a.reconcileStartupRegistration()
	a.manager.Start(ctx)
	a.log("LumiDesk started; preferred controller: %s", a.settings.PreferredDevice.Name)
	for _, warning := range a.store.RecoveryWarnings() {
		runtime.EventsEmit(ctx, "lumidesk:notice", warning)
	}
	if !a.settings.MinimizeToTray || !isStartupLaunch() {
		runtime.EventsEmit(ctx, "lumidesk:ready", true)
	}
}

func (a *App) shutdownApp(ctx context.Context) {
	if ctx != nil {
		w, h := runtime.WindowGetSize(ctx)
		x, y := runtime.WindowGetPosition(ctx)
		settings := a.settingsCopy()
		if w >= 760 && h >= 600 {
			settings.WindowWidth, settings.WindowHeight = w, h
			settings.WindowX, settings.WindowY = x, y
			settings.WindowPositionSet = true
			if err := a.store.SaveSettings(settings); err != nil {
				a.log("could not save window placement: %v", err)
			} else {
				a.mu.Lock()
				a.settings = settings
				a.mu.Unlock()
			}
		}
	}
	if a.settingsCopy().TurnOffOnAppExit && !a.systemEnding.Load() {
		commandCtx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
		if err := a.manager.PowerOffForShutdown(commandCtx); err != nil {
			a.log("could not turn off LEDs while exiting LumiDesk: %v", err)
		} else {
			a.log("sent LED OFF because LumiDesk is exiting (user setting enabled)")
		}
		cancel()
	}
	commandCtx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	if err := a.manager.Close(commandCtx); err != nil {
		a.log("Bluetooth cleanup timed out: %v", err)
	}
	cancel()
	a.log("LumiDesk stopped")
	if a.logger != nil {
		_ = a.logger.Close()
	}
	stopTray()
}

func (a *App) beforeClose(ctx context.Context) bool {
	if a.systemEnding.Load() || a.forceExit.Load() {
		return false
	}
	if a.settingsCopy().CloseToTray {
		runtime.WindowHide(ctx)
		return true
	}
	return false
}

func (a *App) HandleShutdownQuery() bool {
	a.systemEnding.Store(true)
	settings := a.settingsCopy()
	state := a.manager.Snapshot()
	a.shutdownWasOn.Store(state.Powered)
	if !settings.ShutdownLightsOff {
		a.log("Windows shutdown detected; shutdown OFF is disabled")
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2200*time.Millisecond)
	defer cancel()
	if err := a.manager.PowerOffForShutdown(ctx); err != nil {
		a.log("Windows shutdown detected; best-effort LED OFF failed: %v", err)
	} else {
		a.log("Windows shutdown detected; native LED OFF command sent")
	}
	return true
}

func (a *App) HandleShutdownEnd(ending bool) {
	if ending {
		a.log("Windows accepted the session-end request")
		return
	}
	a.systemEnding.Store(false)
	if a.shutdownWasOn.Load() {
		ctx, cancel := context.WithTimeout(context.Background(), 1800*time.Millisecond)
		if err := a.manager.SetPower(ctx, true); err != nil {
			a.log("Windows shutdown was cancelled; restoring LED power failed: %v", err)
		} else {
			a.log("Windows shutdown was cancelled; restored previous LED power state")
		}
		cancel()
	}
}

func (a *App) GetSnapshot() Snapshot {
	a.mu.RLock()
	settings := a.settings
	presets := append([]domain.Preset{}, a.presets...)
	a.mu.RUnlock()
	startupAvailable := a.startup != nil
	_, preLoginAvailable := a.startup.(platform.BeforeLoginStartupRegistrar)
	return Snapshot{State: a.manager.Snapshot(), Settings: settings, Presets: presets, StartupAvailable: startupAvailable, PreLoginAvailable: preLoginAvailable, RecoveryWarnings: a.store.RecoveryWarnings()}
}

func (a *App) ScanDevices() ([]domain.Device, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	devices, err := a.manager.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return devices, nil
}

func (a *App) ConnectDevice(address, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	device := domain.Device{Address: domain.NormalizeAddress(address), Name: strings.TrimSpace(name)}
	if err := a.manager.Connect(ctx, device); err != nil {
		return err
	}
	resolved := a.manager.Settings().PreferredDevice
	if resolved.Address != "" {
		device = resolved
	}
	settings := a.settingsCopy()
	found := false
	for i := range settings.SavedDevices {
		if domain.NormalizeAddress(settings.SavedDevices[i].Address) == domain.NormalizeAddress(device.Address) {
			settings.SavedDevices[i] = device
			found = true
			break
		}
	}
	if !found {
		settings.SavedDevices = append(settings.SavedDevices, device)
	}
	if len(settings.SelectedDevices) == 0 && !settings.SyncDevices {
		settings.SelectedDevices = []string{domain.NormalizeAddress(device.Address)}
	}
	settings.PreferredDevice = device
	return a.SaveSettings(settings)
}

func (a *App) DisconnectDevice(address string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return a.manager.DisconnectDevice(ctx, address)
}

func (a *App) RemoveDevice(address string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.manager.RemoveDevice(ctx, address); err != nil {
		return err
	}
	settings := a.settingsCopy()
	address = domain.NormalizeAddress(address)
	devices := make([]domain.Device, 0, len(settings.SavedDevices))
	for _, device := range settings.SavedDevices {
		if domain.NormalizeAddress(device.Address) != address {
			devices = append(devices, device)
		}
	}
	settings.SavedDevices = devices
	selected := make([]string, 0, len(settings.SelectedDevices))
	for _, selectedAddress := range settings.SelectedDevices {
		if domain.NormalizeAddress(selectedAddress) != address {
			selected = append(selected, selectedAddress)
		}
	}
	settings.SelectedDevices = selected
	if domain.NormalizeAddress(settings.PreferredDevice.Address) == address {
		settings.PreferredDevice = domain.Device{}
		if len(devices) > 0 {
			settings.PreferredDevice = devices[0]
		}
	}
	return a.SaveSettings(settings)
}

func (a *App) ReconnectDevice() error {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	return a.manager.Reconnect(ctx)
}

func (a *App) ReconnectController(address string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	return a.manager.ReconnectDevice(ctx, address)
}

func (a *App) SetColor(r, g, b int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	return a.manager.SetColor(ctx, domain.Color{R: r, G: g, B: b})
}

func (a *App) SetBrightness(level int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	return a.manager.SetBrightness(ctx, level)
}

func (a *App) SetPower(on bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	return a.manager.SetPower(ctx, on)
}

func (a *App) ApplyColor(color domain.Color, brightness int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	return a.manager.ApplyPreset(ctx, color, brightness)
}

func (a *App) StartVisualEffect(kind string, colors []domain.Color, speed int) error {
	manager, ok := a.manager.(control.VisualEffectController)
	if !ok {
		return errors.New("visual effects are unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return manager.StartVisualEffect(ctx, kind, colors, speed)
}

func (a *App) SetVisualEffectSpeed(speed int) error {
	manager, ok := a.manager.(control.VisualEffectController)
	if !ok {
		return errors.New("visual effects are unavailable")
	}
	return manager.SetVisualEffectSpeed(speed)
}

func (a *App) StopVisualEffect() error {
	manager, ok := a.manager.(control.VisualEffectController)
	if !ok {
		return errors.New("visual effects are unavailable")
	}
	return manager.StopVisualEffect()
}

func (a *App) SaveSettings(settings domain.Settings) error {
	settings.NormalizeDevices()
	if err := settings.Validate(); err != nil {
		return err
	}
	previous := a.settingsCopy()
	launchChanged := settings.LaunchAtLogin != previous.LaunchAtLogin
	preLoginChanged := settings.PreLoginLights != previous.PreLoginLights
	preLoginRegistrar, preLoginSupported := a.startup.(platform.BeforeLoginStartupRegistrar)
	if settings.PreLoginLights && !preLoginSupported {
		return errors.New("before-sign-in light startup is unavailable on this platform")
	}
	launchApplied := false
	preLoginApplied := false
	if a.startup != nil && launchChanged {
		if err := a.startup.SetEnabled(settings.LaunchAtLogin); err != nil {
			return fmt.Errorf("could not update Windows startup: %w", err)
		}
		launchApplied = true
	}
	if preLoginChanged && preLoginSupported {
		if err := preLoginRegistrar.SetBeforeLoginEnabled(settings.PreLoginLights); err != nil {
			if launchApplied {
				_ = a.startup.SetEnabled(previous.LaunchAtLogin)
			}
			return fmt.Errorf("could not update before-sign-in light startup: %w", err)
		}
		preLoginApplied = true
	}
	a.mu.Lock()
	if err := a.store.SaveSettings(settings); err != nil {
		a.mu.Unlock()
		if preLoginApplied {
			_ = preLoginRegistrar.SetBeforeLoginEnabled(previous.PreLoginLights)
		}
		if a.startup != nil && launchApplied {
			_ = a.startup.SetEnabled(previous.LaunchAtLogin)
		}
		return err
	}
	a.settings = settings
	presets := append([]domain.Preset(nil), a.presets...)
	a.mu.Unlock()
	a.manager.SetSettings(settings, presets)
	if a.ctx != nil {
		if settings.Theme == "light" {
			runtime.WindowSetLightTheme(a.ctx)
		} else {
			runtime.WindowSetDarkTheme(a.ctx)
		}
	}
	a.publishSnapshot()
	a.log("settings updated")
	return nil
}

func (a *App) CreatePreset(name string, color domain.Color, brightness int) (domain.Preset, error) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) == 0 || len([]rune(name)) > 48 {
		return domain.Preset{}, errors.New("preset name must be between 1 and 48 characters")
	}
	if err := color.Validate(); err != nil {
		return domain.Preset{}, err
	}
	if brightness < 0 || brightness > 100 {
		return domain.Preset{}, errors.New("brightness must be between 0 and 100")
	}
	now := time.Now().UTC()
	preset := domain.Preset{ID: newID(), Name: name, Color: color, Hex: color.Hex(), Brightness: brightness, CreatedAt: now, UpdatedAt: now}
	a.mu.Lock()
	a.presets = append(a.presets, preset)
	if err := a.store.SavePresets(a.presets); err != nil {
		a.presets = a.presets[:len(a.presets)-1]
		a.mu.Unlock()
		return domain.Preset{}, err
	}
	presets := append([]domain.Preset(nil), a.presets...)
	settings := a.settings
	a.mu.Unlock()
	a.manager.SetSettings(settings, presets)
	a.publishSnapshot()
	return preset, nil
}

func (a *App) EditPreset(id, name string, color domain.Color, brightness int) error {
	name = strings.TrimSpace(name)
	if len([]rune(name)) == 0 || len([]rune(name)) > 48 {
		return errors.New("preset name must be between 1 and 48 characters")
	}
	if err := color.Validate(); err != nil {
		return err
	}
	if brightness < 0 || brightness > 100 {
		return errors.New("brightness must be between 0 and 100")
	}
	a.mu.Lock()
	index := a.presetIndex(id)
	if index < 0 {
		a.mu.Unlock()
		return errors.New("preset no longer exists")
	}
	previous := a.presets[index]
	a.presets[index].Name, a.presets[index].Color, a.presets[index].Brightness = name, color, brightness
	a.presets[index].Hex = color.Hex()
	a.presets[index].UpdatedAt = time.Now().UTC()
	if err := a.store.SavePresets(a.presets); err != nil {
		a.presets[index] = previous
		a.mu.Unlock()
		return err
	}
	presets := append([]domain.Preset(nil), a.presets...)
	settings := a.settings
	a.mu.Unlock()
	a.manager.SetSettings(settings, presets)
	a.publishSnapshot()
	return nil
}

func (a *App) DeletePreset(id string) error {
	a.mu.Lock()
	index := a.presetIndex(id)
	if index < 0 {
		a.mu.Unlock()
		return errors.New("preset no longer exists")
	}
	removed := a.presets[index]
	a.presets = append(a.presets[:index], a.presets[index+1:]...)
	previousSettings := a.settings
	if a.settings.FavoritePresetID == id {
		a.settings.FavoritePresetID = ""
	}
	if a.settings.StartupPresetID == id {
		a.settings.StartupPresetID = ""
		a.settings.StartupColorMode = "last"
	}
	if err := a.store.SavePresets(a.presets); err != nil {
		a.presets = append(a.presets[:index], append([]domain.Preset{removed}, a.presets[index:]...)...)
		a.settings = previousSettings
		a.mu.Unlock()
		return err
	}
	settings := a.settings
	if err := a.store.SaveSettings(settings); err != nil {
		a.presets = append(a.presets[:index], append([]domain.Preset{removed}, a.presets[index:]...)...)
		a.settings = previousSettings
		_ = a.store.SavePresets(a.presets)
		a.mu.Unlock()
		return err
	}
	presets := append([]domain.Preset(nil), a.presets...)
	a.mu.Unlock()
	a.manager.SetSettings(settings, presets)
	a.publishSnapshot()
	return nil
}

func (a *App) SetFavoritePreset(id string) error {
	a.mu.Lock()
	if id != "" && a.presetIndex(id) < 0 {
		a.mu.Unlock()
		return errors.New("select a saved preset first")
	}
	previousPresets := append([]domain.Preset(nil), a.presets...)
	previousSettings := a.settings
	for i := range a.presets {
		a.presets[i].Favorite = a.presets[i].ID == id
		if a.presets[i].Favorite {
			a.presets[i].UpdatedAt = time.Now().UTC()
		}
	}
	a.settings.FavoritePresetID = id
	if err := a.store.SavePresets(a.presets); err != nil {
		a.presets, a.settings = previousPresets, previousSettings
		a.mu.Unlock()
		return err
	}
	settings := a.settings
	if err := a.store.SaveSettings(settings); err != nil {
		a.presets, a.settings = previousPresets, previousSettings
		_ = a.store.SavePresets(a.presets)
		a.mu.Unlock()
		return err
	}
	presets := append([]domain.Preset(nil), a.presets...)
	a.mu.Unlock()
	a.manager.SetSettings(settings, presets)
	a.publishSnapshot()
	return nil
}

func (a *App) MovePreset(id string, direction int) error {
	if direction != -1 && direction != 1 {
		return errors.New("preset order direction must be -1 or 1")
	}
	a.mu.Lock()
	index := a.presetIndex(id)
	other := index + direction
	if index < 0 || other < 0 || other >= len(a.presets) {
		a.mu.Unlock()
		return nil
	}
	a.presets[index], a.presets[other] = a.presets[other], a.presets[index]
	if err := a.store.SavePresets(a.presets); err != nil {
		a.presets[index], a.presets[other] = a.presets[other], a.presets[index]
		a.mu.Unlock()
		return err
	}
	a.mu.Unlock()
	a.publishSnapshot()
	return nil
}

func (a *App) ApplyPreset(id string) error {
	a.mu.RLock()
	index := a.presetIndex(id)
	if index < 0 {
		a.mu.RUnlock()
		return errors.New("preset no longer exists")
	}
	preset := a.presets[index]
	a.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return a.manager.ApplyPreset(ctx, preset.Color, preset.Brightness)
}

func (a *App) ExportPresets() (string, error) { return a.store.ExportPresets() }

func (a *App) ImportPresets(raw string, replace bool) error {
	presets, err := a.store.ImportPresets(raw, replace, newID)
	if err != nil {
		return err
	}
	a.mu.Lock()
	previousSettings := a.settings
	a.presets = presets
	if a.settings.FavoritePresetID != "" && !hasPreset(presets, a.settings.FavoritePresetID) {
		a.settings.FavoritePresetID = ""
	}
	if a.settings.StartupPresetID != "" && !hasPreset(presets, a.settings.StartupPresetID) {
		a.settings.StartupPresetID = ""
		a.settings.StartupColorMode = "last"
	}
	settings := a.settings
	if err := a.store.SaveSettings(settings); err != nil {
		a.settings = previousSettings
		a.mu.Unlock()
		return err
	}
	a.mu.Unlock()
	a.manager.SetSettings(settings, presets)
	a.publishSnapshot()
	return nil
}

func (a *App) OpenMainWindow() {
	if a.ctx != nil {
		runtime.WindowShow(a.ctx)
		runtime.WindowUnminimise(a.ctx)
	}
}

func (a *App) HideToTray() {
	if a.ctx != nil {
		runtime.WindowHide(a.ctx)
	}
}

func (a *App) ExitApplication() {
	a.forceExit.Store(true)
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}

func (a *App) NavigateToSettings() {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "lumidesk:navigate", "settings")
		a.OpenMainWindow()
	}
}

func (a *App) StartupRegistrationEnabled() (bool, error) {
	if a.startup == nil {
		return false, errors.New("Windows startup registration is unavailable on this platform")
	}
	return a.startup.Enabled()
}

func (a *App) ApplyFavoritePreset() error {
	settings := a.settingsCopy()
	if settings.FavoritePresetID == "" {
		return errors.New("no favourite preset has been selected")
	}
	return a.ApplyPreset(settings.FavoritePresetID)
}

func (a *App) TrayPower(on bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := a.manager.SetPower(ctx, on); err != nil {
		a.log("tray power command failed: %v", err)
	}
}

func (a *App) TrayReconnect() {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	if err := a.manager.Reconnect(ctx); err != nil {
		a.log("tray reconnect failed: %v", err)
	}
}

func (a *App) settingsCopy() domain.Settings {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.settings
}

func (a *App) presetIndex(id string) int {
	for i := range a.presets {
		if a.presets[i].ID == id {
			return i
		}
	}
	return -1
}

func (a *App) persistManagerSettings(settings domain.Settings) {
	a.mu.Lock()
	merged := a.settings
	merged.LastColor = settings.LastColor
	merged.LastBrightness = settings.LastBrightness
	merged.LastPowerOn = settings.LastPowerOn
	merged.LastEffectMode = settings.LastEffectMode
	merged.LastEffectSpeed = settings.LastEffectSpeed
	merged.LastVisualEffect = settings.LastVisualEffect
	merged.VisualEffectColors = append([]domain.Color{}, settings.VisualEffectColors...)
	merged.VisualEffectSpeed = settings.VisualEffectSpeed
	merged.SavedDevices = append([]domain.Device{}, settings.SavedDevices...)
	merged.SelectedDevices = append([]string{}, settings.SelectedDevices...)
	merged.SyncDevices = settings.SyncDevices
	if settings.PreferredDevice.Address != "" {
		merged.PreferredDevice = settings.PreferredDevice
	}
	if err := a.store.SaveSettings(merged); err != nil {
		a.log("could not persist LED settings: %v", err)
	}
	a.settings = merged
	a.mu.Unlock()
	a.publishSnapshot()
}

func (a *App) publishSnapshot() {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "lumidesk:snapshot", a.GetSnapshot())
	}
}

func (a *App) reconcileStartupRegistration() {
	if a.startup == nil {
		return
	}
	enabled, err := a.startup.Enabled()
	if err != nil {
		a.log("could not inspect Windows startup registration: %v", err)
	} else if enabled != a.settingsCopy().LaunchAtLogin {
		if err := a.startup.SetEnabled(a.settingsCopy().LaunchAtLogin); err != nil {
			a.log("could not synchronize Windows startup registration: %v", err)
		}
	}
	if beforeLogin, ok := a.startup.(platform.BeforeLoginStartupRegistrar); ok {
		settings := a.settingsCopy()
		enabled, err := beforeLogin.BeforeLoginEnabled()
		if err != nil {
			a.log("could not inspect the pre-login startup task: %v", err)
			// Older prototype tasks may deny query access. Reinstall once when the
			// user has opted in; the new SYSTEM task records its registration marker.
			if settings.PreLoginLights {
				if err := beforeLogin.SetBeforeLoginEnabled(true); err != nil {
					a.log("could not repair the pre-login startup task: %v", err)
				}
			}
		} else if enabled != settings.PreLoginLights {
			if err := beforeLogin.SetBeforeLoginEnabled(settings.PreLoginLights); err != nil {
				a.log("could not synchronize the pre-login startup task: %v", err)
			}
		}
	}
}

func (a *App) log(format string, args ...any) {
	if a.logger != nil {
		a.logger.Printf(format, args...)
	}
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("preset-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" + hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:])
}

func hasPreset(presets []domain.Preset, id string) bool {
	for _, preset := range presets {
		if preset.ID == id {
			return true
		}
	}
	return false
}

func isStartupLaunch() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--startup" {
			return true
		}
	}
	return false
}

func userLogDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "LumiDesk", "logs")
}
