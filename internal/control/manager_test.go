package control

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"lumidesk/internal/domain"
)

func TestManagerSerializesNativeCommandsAndState(t *testing.T) {
	settings := controlTestSettings()
	settings.AutoReconnect = false
	settings.StartupLightsOn = false
	settings.LastPowerOn = false
	transport := &fakeTransport{}
	manager := startTestManager(t, transport, settings)
	device := domain.Device{Name: "ELK-BLEDOM 66", Address: "be-68-ea-07-4d-02"}
	if err := manager.Connect(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetColor(context.Background(), domain.Color{R: 255, G: 12, B: 40}); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetBrightness(context.Background(), 43); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetPower(context.Background(), true); err != nil {
		t.Fatal(err)
	}

	want := []string{"color:255,12,40", "brightness:110", "color:255,12,40", "brightness:110", "power:true"}
	if got := transport.lastLamp().commands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %v, want ordered commands %v", got, want)
	}
	state := manager.Snapshot()
	if !state.Connected || !state.Powered || state.Color != (domain.Color{R: 255, G: 12, B: 40}) || state.Brightness != 43 {
		t.Fatalf("unexpected manager state after commands: %+v", state)
	}
	if manager.Settings().PreferredDevice.Address != "BE:68:EA:07:4D:02" {
		t.Fatalf("preferred address wasn't normalized/saved: %+v", manager.Settings().PreferredDevice)
	}
}

func TestApplyPresetSendsOneOrderedColorBrightnessPowerSequence(t *testing.T) {
	settings := controlTestSettings()
	settings.AutoReconnect = false
	settings.StartupLightsOn = false
	settings.LastPowerOn = false
	transport := &fakeTransport{}
	manager := startTestManager(t, transport, settings)
	if err := manager.Connect(context.Background(), settings.PreferredDevice); err != nil {
		t.Fatal(err)
	}
	color := domain.Color{R: 255, G: 0, B: 0}
	if err := manager.ApplyPreset(context.Background(), color, 75); err != nil {
		t.Fatal(err)
	}
	want := []string{"color:255,0,0", "brightness:191", "power:true"}
	if got := transport.lastLamp().commands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("preset apply commands = %v, want %v", got, want)
	}
	state := manager.Snapshot()
	if !state.Powered || state.Color != color || state.Brightness != 75 {
		t.Fatalf("preset apply didn't update active state: %+v", state)
	}
}

func TestELKBLEDOBEffectAndSpeedUseSerializedCommands(t *testing.T) {
	settings := domain.DefaultSettings()
	settings.AutoReconnect = false
	settings.StartupLightsOn = false
	settings.LastPowerOn = false
	device := domain.Device{Name: "ELK-BLEDOB 66", Address: "BE:68:EA:07:4D:02"}
	settings.PreferredDevice = device
	settings.SavedDevices = []domain.Device{device}
	settings.SelectedDevices = []string{device.Address}
	transport := &fakeTransport{}
	manager := startTestManager(t, transport, settings)
	if err := manager.Connect(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetEffectMode(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetEffectSpeed(context.Background(), 183); err != nil {
		t.Fatal(err)
	}
	want := []string{"mode:7", "power:true", "speed:183"}
	if got := transport.lastLamp().commands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("effect command sequence = %v, want %v", got, want)
	}
	state := manager.Snapshot()
	if !state.EffectsSupported || state.EffectMode != 7 || state.EffectSpeed != 183 || !state.Powered {
		t.Fatalf("effect state wasn't saved: %+v", state)
	}
}

func TestPendingColorUpdatesAreCoalescedInOrder(t *testing.T) {
	settings := controlTestSettings()
	settings.AutoReconnect = false
	settings.StartupLightsOn = false
	transport := &fakeTransport{}
	manager := startTestManager(t, transport, settings)
	if err := manager.Connect(context.Background(), settings.PreferredDevice); err != nil {
		t.Fatal(err)
	}
	lamp := transport.lastLamp()
	lamp.blockFirstColor()
	firstDone := make(chan error, 1)
	go func() { firstDone <- manager.SetColor(context.Background(), domain.Color{R: 255}) }()
	select {
	case <-lamp.firstColorStarted:
	case <-time.After(time.Second):
		t.Fatal("first color command did not start")
	}
	secondDone := make(chan error, 1)
	thirdDone := make(chan error, 1)
	go func() { secondDone <- manager.SetColor(context.Background(), domain.Color{G: 255}) }()
	time.Sleep(30 * time.Millisecond)
	go func() { thirdDone <- manager.SetColor(context.Background(), domain.Color{B: 255}) }()
	time.Sleep(30 * time.Millisecond)
	close(lamp.releaseFirstColor)
	for name, done := range map[string]<-chan error{"first": firstDone, "second": secondDone, "third": thirdDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s color request failed: %v", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s color request was not completed", name)
		}
	}
	want := []string{"color:255,0,0", "color:0,0,255"}
	if got := lamp.commands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("coalesced color writes = %v, want %v", got, want)
	}
}

func TestShutdownPowerOffUsesCurrentLinkOnly(t *testing.T) {
	settings := controlTestSettings()
	settings.AutoReconnect = false
	settings.StartupLightsOn = false
	transport := &fakeTransport{}
	manager := startTestManager(t, transport, settings)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.PowerOffForShutdown(ctx); err == nil {
		t.Fatal("shutdown unexpectedly tried to connect")
	}
	if transport.connectCount() != 0 {
		t.Fatal("shutdown attempted a new connection")
	}
	if err := manager.Connect(ctx, settings.PreferredDevice); err != nil {
		t.Fatal(err)
	}
	if err := manager.PowerOffForShutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if got := transport.lastLamp().commands(); !reflect.DeepEqual(got, []string{"power:false"}) {
		t.Fatalf("shutdown command = %v, want native power-off only", got)
	}
	if manager.Snapshot().Powered {
		t.Fatal("state remained powered after successful shutdown OFF")
	}
}

func TestReconnectTransitionsAndRetry(t *testing.T) {
	settings := controlTestSettings()
	settings.AutoReconnect = false
	settings.StartupLightsOn = false
	transport := &fakeTransport{}
	manager := startTestManager(t, transport, settings)
	if err := manager.Connect(context.Background(), settings.PreferredDevice); err != nil {
		t.Fatal(err)
	}
	if err := manager.Connect(context.Background(), settings.PreferredDevice); err != nil {
		t.Fatal(err)
	}
	if transport.connectCount() != 1 {
		t.Fatal("connecting to the already-active controller made a competing BLE connection")
	}
	if err := manager.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if manager.Snapshot().Connected {
		t.Fatal("manager remained connected after disconnect")
	}
	if err := manager.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !manager.Snapshot().Connected || transport.connectCount() != 2 {
		t.Fatalf("reconnect did not establish a new connection: state=%+v count=%d", manager.Snapshot(), transport.connectCount())
	}
}

func TestAdapterConnectionEventsUpdateVisibleState(t *testing.T) {
	settings := controlTestSettings()
	settings.AutoReconnect = true
	settings.StartupLightsOn = false
	transport := &fakeTransport{}
	manager := startTestManager(t, transport, settings)
	device := settings.PreferredDevice
	if err := manager.Connect(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	manager.handleConnectionEvent(domain.Device{Address: device.Address}, false)
	state := waitForState(t, manager, func(s State) bool { return !s.Connected })
	if state.Connected || state.LastError != "Bluetooth link dropped; reconnecting automatically" {
		t.Fatalf("disconnect event wasn't reflected in state: %+v", state)
	}
	manager.handleConnectionEvent(domain.Device{Address: device.Address}, true)
	state = waitForState(t, manager, func(s State) bool { return s.Connected })
	if !state.Connected || state.LastError != "" {
		t.Fatalf("reconnected event wasn't reflected in state: %+v", state)
	}
}

func TestAutomaticReconnectUsesBackoffAfterStartupFailure(t *testing.T) {
	settings := controlTestSettings()
	settings.StartupLightsOn = false
	transport := &fakeTransport{connectErrors: []error{errors.New("adapter starting"), nil}}
	manager := NewManager(transport, settings, nil, Callbacks{})
	manager.Start(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = manager.Close(ctx)
	})
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if manager.Snapshot().Connected {
			if transport.connectCount() < 2 {
				t.Fatal("connected without retrying the failed first attempt")
			}
			return
		}
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("manager failed to reconnect after startup failure: state=%+v attempts=%d", manager.Snapshot(), transport.connectCount())
		}
	}
}

func TestScanPublishesOnlyTransportResults(t *testing.T) {
	settings := domain.DefaultSettings()
	settings.AutoReconnect = false
	transport := &fakeTransport{scanDevices: []domain.Device{{Name: "ELK-BLEDOM 66", Address: "AA:BB:CC:DD:EE:FF", RSSI: -48}}}
	manager := startTestManager(t, transport, settings)
	devices, err := manager.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].RSSI != -48 || manager.Snapshot().Scanning {
		t.Fatalf("unexpected scan result/state: devices=%+v state=%+v", devices, manager.Snapshot())
	}
}

func TestSnapshotUsesEmptyDeviceArrayBeforeFirstScan(t *testing.T) {
	manager := NewManager(&fakeTransport{}, domain.DefaultSettings(), nil, Callbacks{})
	if manager.Snapshot().Devices == nil {
		t.Fatal("device list should serialize as an empty array, not null, before scanning")
	}
}

func TestStartupColorSelectionSupportsLastFavoriteAndNamedPreset(t *testing.T) {
	settings := domain.DefaultSettings()
	favorite := domain.Preset{ID: "favorite-id", Name: "Game", Color: domain.Color{R: 125, B: 255}, Brightness: 68}
	selected := domain.Preset{ID: "startup-id", Name: "Evening", Color: domain.Color{R: 255, G: 150}, Brightness: 42}
	manager := NewManager(nil, settings, []domain.Preset{favorite, selected}, Callbacks{})
	current := domain.Color{R: 2, G: 3, B: 4}

	settings.StartupColorMode = "last"
	settings.RestoreLastColor = true
	if gotColor, gotBrightness := manager.startupSelection(settings, current, 21); gotColor != settings.LastColor || gotBrightness != settings.LastBrightness {
		t.Fatalf("last-used selection = %+v/%d", gotColor, gotBrightness)
	}

	settings.StartupColorMode = "favorite"
	settings.FavoritePresetID = favorite.ID
	if gotColor, gotBrightness := manager.startupSelection(settings, current, 21); gotColor != favorite.Color || gotBrightness != favorite.Brightness {
		t.Fatalf("favourite selection = %+v/%d", gotColor, gotBrightness)
	}

	settings.StartupColorMode = "preset"
	settings.StartupPresetID = selected.ID
	if gotColor, gotBrightness := manager.startupSelection(settings, current, 21); gotColor != selected.Color || gotBrightness != selected.Brightness {
		t.Fatalf("specific preset selection = %+v/%d", gotColor, gotBrightness)
	}

	settings.RestoreLastColor = false
	settings.StartupPresetID = "missing"
	if gotColor, _ := manager.startupSelection(settings, current, 21); gotColor != (domain.Color{R: 255, G: 255, B: 255}) {
		t.Fatalf("missing specific preset should use safe neutral white: %+v", gotColor)
	}
}

func startTestManager(t *testing.T, transport *fakeTransport, settings domain.Settings) *Manager {
	t.Helper()
	manager := NewManager(transport, settings, nil, Callbacks{})
	manager.Start(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close manager: %v", err)
		}
	})
	return manager
}

func controlTestSettings() domain.Settings {
	settings := domain.DefaultSettings()
	device := domain.Device{Name: "Test LED", Address: "AA:BB:CC:DD:EE:01"}
	settings.PreferredDevice = device
	settings.SavedDevices = []domain.Device{device}
	settings.SelectedDevices = []string{device.Address}
	return settings
}

func waitForState(t *testing.T, manager *Manager, ready func(State) bool) State {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		state := manager.Snapshot()
		if ready(state) {
			return state
		}
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("state transition not observed: %+v", manager.Snapshot())
		}
	}
}

type fakeTransport struct {
	mu            sync.Mutex
	connectErrors []error
	connects      int
	lamps         []*fakeLamp
	scanDevices   []domain.Device
}

func (f *fakeTransport) Scan(context.Context, time.Duration) ([]domain.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.Device(nil), f.scanDevices...), nil
}

func (f *fakeTransport) Connect(_ context.Context, device domain.Device) (Lamp, domain.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connects++
	if len(f.connectErrors) > 0 {
		err := f.connectErrors[0]
		f.connectErrors = f.connectErrors[1:]
		if err != nil {
			return nil, device, err
		}
	}
	lamp := newFakeLamp()
	f.lamps = append(f.lamps, lamp)
	return lamp, device, nil
}

func (f *fakeTransport) lastLamp() *fakeLamp {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.lamps) == 0 {
		return nil
	}
	return f.lamps[len(f.lamps)-1]
}

func (f *fakeTransport) allLamps() []*fakeLamp {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*fakeLamp(nil), f.lamps...)
}

func (f *fakeTransport) connectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connects
}

type fakeLamp struct {
	mu                sync.Mutex
	log               []string
	firstColorStarted chan struct{}
	releaseFirstColor chan struct{}
	blockColor        bool
	closed            bool
}

func newFakeLamp() *fakeLamp { return &fakeLamp{} }

func (l *fakeLamp) LightOn(on bool) error {
	l.mu.Lock()
	l.log = append(l.log, fmt.Sprintf("power:%t", on))
	l.mu.Unlock()
	return nil
}

func (l *fakeLamp) ChangeColorRGB(r, g, b int) error {
	l.mu.Lock()
	shouldBlock := l.blockColor
	if shouldBlock {
		l.blockColor = false
	}
	started, release := l.firstColorStarted, l.releaseFirstColor
	l.mu.Unlock()
	if shouldBlock {
		close(started)
		<-release
	}
	l.mu.Lock()
	l.log = append(l.log, fmt.Sprintf("color:%d,%d,%d", r, g, b))
	l.mu.Unlock()
	return nil
}

func (l *fakeLamp) ChangeBrightness(level, mode int) error {
	l.mu.Lock()
	l.log = append(l.log, fmt.Sprintf("brightness:%d", level))
	l.mu.Unlock()
	return nil
}

func (l *fakeLamp) ChangeMode(mode int) error {
	l.mu.Lock()
	l.log = append(l.log, fmt.Sprintf("mode:%d", mode))
	l.mu.Unlock()
	return nil
}
func (l *fakeLamp) ChangeModeSpeed(speed int) error {
	l.mu.Lock()
	l.log = append(l.log, fmt.Sprintf("speed:%d", speed))
	l.mu.Unlock()
	return nil
}
func (l *fakeLamp) Close() error {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	return nil
}

func (l *fakeLamp) commands() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.log...)
}

func (l *fakeLamp) blockFirstColor() {
	l.mu.Lock()
	l.blockColor = true
	l.firstColorStarted = make(chan struct{})
	l.releaseFirstColor = make(chan struct{})
	l.mu.Unlock()
}
