package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"lumidesk/internal/control"
	"lumidesk/internal/domain"
	"lumidesk/internal/persistence"
)

func TestWindowsStartupSettingRegistrationIsTransactional(t *testing.T) {
	store, err := persistence.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registrar := &memoryStartup{}
	app, err := NewApp(store, nil, registrar)
	if err != nil {
		t.Fatal(err)
	}
	settings := app.settingsCopy()
	settings.LaunchAtLogin = true
	if err := app.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if enabled, _ := registrar.Enabled(); !enabled || registrar.setCalls != 1 {
		t.Fatalf("startup registration was not enabled: enabled=%t calls=%d", enabled, registrar.setCalls)
	}
	reloaded, err := store.LoadSettings()
	if err != nil || !reloaded.LaunchAtLogin {
		t.Fatalf("startup preference did not persist: %+v, %v", reloaded, err)
	}

	registrar.err = errors.New("access denied")
	settings.LaunchAtLogin = false
	if err := app.SaveSettings(settings); err == nil {
		t.Fatal("expected registry failure to be reported")
	}
	reloaded, err = store.LoadSettings()
	if err != nil || !reloaded.LaunchAtLogin {
		t.Fatalf("failed registry update changed persisted preference: %+v, %v", reloaded, err)
	}
}

func TestBLEStatePersistenceDoesNotOverwriteUserPreferences(t *testing.T) {
	store, err := persistence.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registrar := &memoryStartup{}
	app, err := NewApp(store, nil, registrar)
	if err != nil {
		t.Fatal(err)
	}
	settings := app.settingsCopy()
	settings.AutoReconnect = false
	settings.CloseToTray = false
	settings.Theme = "light"
	settings.LastPowerOn = true
	if err := app.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	stale := domain.DefaultSettings()
	stale.LastColor = domain.Color{R: 17, G: 29, B: 41}
	stale.LastBrightness = 31
	stale.LastPowerOn = false
	app.persistManagerSettings(stale)
	reloaded, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.AutoReconnect || reloaded.CloseToTray || reloaded.Theme != "light" {
		t.Fatalf("BLE state persistence overwrote UI preferences: %+v", reloaded)
	}
	if reloaded.LastColor != stale.LastColor || reloaded.LastBrightness != 31 || reloaded.LastPowerOn {
		t.Fatalf("BLE state was not persisted: %+v", reloaded)
	}
}

func TestSavingDeviceSelectionDoesNotDeadlockWhenEffectStateIsCleared(t *testing.T) {
	store, err := persistence.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(store, nil, &memoryStartup{})
	if err != nil {
		t.Fatal(err)
	}
	settings := app.settingsCopy()
	device := domain.Device{Name: "ELK-BLEDOB 66", Address: "AA:BB:CC:DD:EE:66"}
	settings.SavedDevices = []domain.Device{device}
	settings.SelectedDevices = []string{device.Address}
	done := make(chan error, 1)
	go func() { done <- app.SaveSettings(settings) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("saving the selected device deadlocked while clearing an effect")
	}
}

func TestSessionEndTurnsOffAndRestoresIfCancelled(t *testing.T) {
	store, err := persistence.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := appTestSettings()
	settings.AutoReconnect = false
	settings.StartupLightsOn = true
	settings.LastPowerOn = true
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(store, nil, &memoryStartup{})
	if err != nil {
		t.Fatal(err)
	}
	transport := &shutdownFakeTransport{}
	app.manager = control.NewManager(transport, settings, nil, control.Callbacks{})
	app.manager.Start(context.Background())
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = app.manager.Close(ctx)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.manager.Connect(ctx, settings.PreferredDevice); err != nil {
		t.Fatal(err)
	}
	if err := transport.lamp.waitForCommand("power:true"); err != nil {
		t.Fatal(err)
	}
	if !app.HandleShutdownQuery() {
		t.Fatal("shutdown query was vetoed")
	}
	if err := transport.lamp.waitForCommand("power:false"); err != nil {
		t.Fatal(err)
	}
	app.HandleShutdownEnd(false)
	if err := transport.lamp.waitForCommand("power:true#2"); err != nil {
		t.Fatalf("cancelled shutdown did not restore LED power: %v; commands=%v", err, transport.lamp.commands())
	}
	if app.systemEnding.Load() || !app.manager.Snapshot().Powered {
		t.Fatalf("session cancellation left inconsistent state: ending=%t state=%+v", app.systemEnding.Load(), app.manager.Snapshot())
	}
}

func TestShutdownPreferenceSkipsOffCommand(t *testing.T) {
	store, _ := persistence.New(t.TempDir())
	settings := appTestSettings()
	settings.AutoReconnect = false
	settings.StartupLightsOn = false
	settings.ShutdownLightsOff = false
	_ = store.SaveSettings(settings)
	app, err := NewApp(store, nil, &memoryStartup{})
	if err != nil {
		t.Fatal(err)
	}
	transport := &shutdownFakeTransport{}
	app.manager = control.NewManager(transport, settings, nil, control.Callbacks{})
	app.manager.Start(context.Background())
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = app.manager.Close(ctx)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.manager.Connect(ctx, settings.PreferredDevice); err != nil {
		t.Fatal(err)
	}
	if !app.HandleShutdownQuery() {
		t.Fatal("shutdown query was vetoed")
	}
	if got := transport.lamp.commands(); len(got) != 0 {
		t.Fatalf("shutdown OFF sent despite disabled setting: %v", got)
	}
}

func TestPresetCreateEditFavoriteImportOrderAndDelete(t *testing.T) {
	store, err := persistence.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(store, nil, &memoryStartup{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := app.CreatePreset("Gaming Purple", domain.Color{R: 170, B: 255}, 78)
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.CreatePreset("Warm Evening", domain.Color{R: 255, G: 126, B: 46}, 64)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.EditPreset(first.ID, "Night Mode", domain.Color{R: 106, G: 67, B: 220}, 52); err != nil {
		t.Fatal(err)
	}
	if err := app.SetFavoritePreset(first.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.MovePreset(first.ID, 1); err != nil {
		t.Fatal(err)
	}
	snapshot := app.GetSnapshot()
	if len(snapshot.Presets) != 2 || snapshot.Presets[0].ID != second.ID || snapshot.Presets[1].Name != "Night Mode" || !snapshot.Presets[1].Favorite {
		t.Fatalf("preset edit/favourite/order did not persist: %+v", snapshot.Presets)
	}
	if snapshot.Settings.FavoritePresetID != first.ID {
		t.Fatalf("favourite reference was not persisted: %+v", snapshot.Settings)
	}

	imported, _ := json.Marshal([]domain.Preset{{ID: "external", Name: "Movie Blue", Color: domain.Color{B: 210}, Brightness: 90}})
	if err := app.ImportPresets(string(imported), false); err != nil {
		t.Fatal(err)
	}
	if len(app.GetSnapshot().Presets) != 3 {
		t.Fatalf("preset import did not merge: %+v", app.GetSnapshot().Presets)
	}
	if err := app.DeletePreset(first.ID); err != nil {
		t.Fatal(err)
	}
	if app.settingsCopy().FavoritePresetID != "" {
		t.Fatal("deleting the favourite preset left a dangling favourite setting")
	}
	if len(app.GetSnapshot().Presets) != 2 {
		t.Fatalf("preset deletion failed: %+v", app.GetSnapshot().Presets)
	}
}

type memoryStartup struct {
	mu       sync.Mutex
	enabled  bool
	setCalls int
	err      error
}

func appTestSettings() domain.Settings {
	settings := domain.DefaultSettings()
	device := domain.Device{Name: "Test LED", Address: "AA:BB:CC:DD:EE:10"}
	settings.PreferredDevice = device
	settings.SavedDevices = []domain.Device{device}
	settings.SelectedDevices = []string{device.Address}
	return settings
}

func (s *memoryStartup) Enabled() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled, nil
}

func (s *memoryStartup) SetEnabled(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setCalls++
	if s.err != nil {
		return s.err
	}
	s.enabled = enabled
	return nil
}

type shutdownFakeTransport struct{ lamp *shutdownFakeLamp }

func (f *shutdownFakeTransport) Scan(context.Context, time.Duration) ([]domain.Device, error) {
	return []domain.Device{}, nil
}

func (f *shutdownFakeTransport) Connect(_ context.Context, device domain.Device) (control.Lamp, domain.Device, error) {
	if f.lamp == nil {
		f.lamp = &shutdownFakeLamp{commandsLog: []string{}}
	}
	return f.lamp, device, nil
}

type shutdownFakeLamp struct {
	mu          sync.Mutex
	commandsLog []string
	changed     chan struct{}
}

func (l *shutdownFakeLamp) record(command string) {
	l.mu.Lock()
	l.commandsLog = append(l.commandsLog, command)
	if l.changed != nil {
		close(l.changed)
	}
	l.changed = make(chan struct{})
	l.mu.Unlock()
}

func (l *shutdownFakeLamp) LightOn(on bool) error { l.record(fmt.Sprintf("power:%t", on)); return nil }
func (l *shutdownFakeLamp) ChangeColorRGB(r, g, b int) error {
	l.record(fmt.Sprintf("color:%d,%d,%d", r, g, b))
	return nil
}
func (l *shutdownFakeLamp) ChangeBrightness(level, _ int) error {
	l.record(fmt.Sprintf("brightness:%d", level))
	return nil
}
func (l *shutdownFakeLamp) ChangeMode(int) error      { return nil }
func (l *shutdownFakeLamp) ChangeModeSpeed(int) error { return nil }
func (l *shutdownFakeLamp) Close() error              { return nil }

func (l *shutdownFakeLamp) commands() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.commandsLog...)
}

func (l *shutdownFakeLamp) waitForCommand(want string) error {
	target := want
	minimum := 1
	if len(want) > 2 && want[len(want)-2:] == "#2" {
		target = want[:len(want)-2]
		minimum = 2
	}
	deadline := time.After(time.Second)
	for {
		l.mu.Lock()
		count := 0
		for _, command := range l.commandsLog {
			if command == target {
				count++
			}
		}
		changed := l.changed
		l.mu.Unlock()
		if count >= minimum {
			return nil
		}
		if changed == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		select {
		case <-changed:
		case <-deadline:
			return fmt.Errorf("command %q did not arrive", want)
		}
	}
}
