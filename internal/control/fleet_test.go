package control

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"lumidesk/internal/domain"
)

func TestFleetControlsSelectedDevicesAndSyncsAll(t *testing.T) {
	first := domain.Device{Name: "Desk strip", Address: "AA:BB:CC:DD:EE:01"}
	second := domain.Device{Name: "Shelf strip", Address: "AA:BB:CC:DD:EE:02"}
	settings := domain.DefaultSettings()
	settings.AutoReconnect = false
	settings.StartupLightsOn = false
	settings.LastPowerOn = false
	settings.SavedDevices = []domain.Device{first, second}
	settings.SelectedDevices = []string{first.Address}
	settings.PreferredDevice = first
	transport := &fakeTransport{}
	fleet := NewFleetManager(transport, settings, nil, Callbacks{})
	fleet.Start(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := fleet.Close(ctx); err != nil {
			t.Errorf("close fleet: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fleet.Connect(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := fleet.Connect(ctx, second); err != nil {
		t.Fatal(err)
	}
	if state := fleet.Snapshot(); !state.Connected || len(state.ConnectedDevices) != 2 || len(state.Devices) != 2 {
		t.Fatalf("fleet did not expose both active strips: %+v", state)
	}
	lamps := transport.allLamps()
	if len(lamps) != 2 {
		t.Fatalf("connected %d fake lamps, want two", len(lamps))
	}
	if err := fleet.SetColor(ctx, domain.Color{R: 255}); err != nil {
		t.Fatal(err)
	}
	if got := lamps[0].commands(); !reflect.DeepEqual(got, []string{"color:255,0,0"}) {
		t.Fatalf("selected first strip got commands %v", got)
	}
	if got := lamps[1].commands(); len(got) != 0 {
		t.Fatalf("unselected second strip was changed: %v", got)
	}

	if err := fleet.SetSelectedDevices([]string{second.Address}); err != nil {
		t.Fatal(err)
	}
	if err := fleet.SetColor(ctx, domain.Color{G: 255}); err != nil {
		t.Fatal(err)
	}
	if got := lamps[0].commands(); !reflect.DeepEqual(got, []string{"color:255,0,0"}) {
		t.Fatalf("first strip changed after selection moved: %v", got)
	}
	if got := lamps[1].commands(); !reflect.DeepEqual(got, []string{"color:0,255,0"}) {
		t.Fatalf("second strip did not receive its selected color: %v", got)
	}

	settings = fleet.Settings()
	settings.SyncDevices = true
	settings.SelectedDevices = []string{first.Address, second.Address}
	fleet.SetSettings(settings, nil)
	if err := fleet.SetColor(ctx, domain.Color{B: 255}); err != nil {
		t.Fatal(err)
	}
	if got := lamps[0].commands(); !reflect.DeepEqual(got, []string{"color:255,0,0", "color:0,0,255"}) {
		t.Fatalf("sync did not update first strip: %v", got)
	}
	if got := lamps[1].commands(); !reflect.DeepEqual(got, []string{"color:0,255,0", "color:0,0,255"}) {
		t.Fatalf("sync did not update second strip: %v", got)
	}
}

func TestFleetDisconnectPreservesSavedDeviceButClearsActiveState(t *testing.T) {
	device := domain.Device{Name: "Desk strip", Address: "AA:BB:CC:DD:EE:01"}
	settings := domain.DefaultSettings()
	settings.AutoReconnect = false
	settings.StartupLightsOn = false
	settings.SavedDevices = []domain.Device{device}
	settings.SelectedDevices = []string{device.Address}
	transport := &fakeTransport{}
	fleet := NewFleetManager(transport, settings, nil, Callbacks{})
	fleet.Start(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = fleet.Close(ctx)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := fleet.Connect(ctx, device); err != nil {
		t.Fatal(err)
	}
	if err := fleet.DisconnectDevice(ctx, device.Address); err != nil {
		t.Fatal(err)
	}
	state := fleet.Snapshot()
	if state.Connected || len(state.ConnectedDevices) != 0 || len(state.Devices) != 1 || state.Devices[0].Connected {
		t.Fatalf("disconnect should keep the saved row but clear active status: %+v", state)
	}
}

func TestFleetFirstAddedControllerBecomesSelectedWithoutANameAssumption(t *testing.T) {
	settings := domain.DefaultSettings()
	settings.AutoReconnect = false
	settings.StartupLightsOn = false
	settings.LastPowerOn = false
	transport := &fakeTransport{}
	fleet := NewFleetManager(transport, settings, nil, Callbacks{})
	fleet.Start(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = fleet.Close(ctx)
	})
	device := domain.Device{Name: "My RGB controller", Address: "AA:BB:CC:DD:EE:99"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := fleet.Connect(ctx, device); err != nil {
		t.Fatal(err)
	}
	got := fleet.Settings()
	if len(got.SavedDevices) != 1 || got.SavedDevices[0].Name != device.Name || len(got.SelectedDevices) != 1 || got.SelectedDevices[0] != device.Address {
		t.Fatalf("first discovered controller was not saved and selected: %+v", got)
	}
	if state := fleet.Snapshot(); !state.Connected || state.DeviceName != device.Name {
		t.Fatalf("connected controller was not active in state: %+v", state)
	}
}

func TestFleetErrorNamesTheControllerThatFailed(t *testing.T) {
	device := domain.Device{Name: "Shelf strip", Address: "AA:BB:CC:DD:EE:22"}
	settings := domain.DefaultSettings()
	settings.AutoReconnect = false
	settings.StartupLightsOn = false
	transport := &fakeTransport{connectErrors: []error{errors.New("async operation failed")}}
	fleet := NewFleetManager(transport, settings, nil, Callbacks{})
	fleet.Start(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = fleet.Close(ctx)
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = fleet.Connect(ctx, device)
	if got := fleet.Snapshot().LastError; !strings.Contains(got, device.Name) {
		t.Fatalf("connection error should identify its controller, got %q", got)
	}
}

func TestFleetSmoothRainbowUsesContinuousRGBWritesAndCanStop(t *testing.T) {
	device := domain.Device{Name: "ELK-BLEDOB 66", Address: "AA:BB:CC:DD:EE:66"}
	settings := domain.DefaultSettings()
	settings.AutoReconnect = false
	settings.StartupLightsOn = false
	settings.LastPowerOn = false
	settings.SavedDevices = []domain.Device{device}
	settings.SelectedDevices = []string{device.Address}
	transport := &fakeTransport{}
	fleet := NewFleetManager(transport, settings, nil, Callbacks{})
	fleet.Start(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := fleet.Close(ctx); err != nil {
			t.Errorf("close fleet: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := fleet.Connect(ctx, device); err != nil {
		t.Fatal(err)
	}
	if err := fleet.StartVisualEffect(ctx, "rainbow", nil, 100); err != nil {
		t.Fatal(err)
	}
	time.Sleep(350 * time.Millisecond)
	if state := fleet.Snapshot(); state.ActiveEffect != "rainbow" {
		t.Fatalf("rainbow wasn't reported active: %+v", state)
	}
	if err := fleet.StopVisualEffect(); err != nil {
		t.Fatal(err)
	}
	commands := transport.lastLamp().commands()
	colorWrites := 0
	for _, command := range commands {
		if strings.HasPrefix(command, "color:") {
			colorWrites++
		}
		if command == "power:false" {
			t.Fatalf("rainbow sent a power-off command between RGB frames: %v", commands)
		}
	}
	if colorWrites < 3 {
		t.Fatalf("rainbow did not produce repeated RGB frames: %v", commands)
	}
	if state := fleet.Snapshot(); state.ActiveEffect != "" {
		t.Fatalf("effect remained active after stop: %+v", state)
	}
}
