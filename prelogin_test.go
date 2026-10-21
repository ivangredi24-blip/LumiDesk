package main

import (
	"reflect"
	"testing"

	"lumidesk/internal/domain"
)

func TestPreLoginStartupConfigParsesScheduledTaskArguments(t *testing.T) {
	configDir, requested := preLoginStartupConfig([]string{
		"--prelogin-lights", "--config-dir", `C:\Users\Test User\AppData\Roaming\LumiDesk`,
	})
	if !requested || configDir != `C:\Users\Test User\AppData\Roaming\LumiDesk` {
		t.Fatalf("parsed pre-login task args = (%q, %t)", configDir, requested)
	}
	if _, requested := preLoginStartupConfig([]string{"--startup"}); requested {
		t.Fatal("normal tray startup was mistaken for the headless boot task")
	}
}

func TestPreLoginTargetsRespectSelectionAndSyncAll(t *testing.T) {
	first := domain.Device{Name: "Desk", Address: "AA:BB:CC:DD:EE:01"}
	second := domain.Device{Name: "Shelf", Address: "AA:BB:CC:DD:EE:02"}
	settings := domain.DefaultSettings()
	settings.SavedDevices = []domain.Device{first, second}
	settings.SelectedDevices = []string{first.Address}
	if got := preLoginTargets(settings); !reflect.DeepEqual(got, []domain.Device{first}) {
		t.Fatalf("selected pre-login targets = %+v", got)
	}
	settings.SyncDevices = true
	if got := preLoginTargets(settings); !reflect.DeepEqual(got, []domain.Device{first, second}) {
		t.Fatalf("sync-all pre-login targets = %+v", got)
	}
	agent := cloneBootSettings(settings, []domain.Device{first})
	if !agent.SyncDevices || len(agent.SavedDevices) != 1 || agent.PreferredDevice != first {
		t.Fatalf("boot settings weren't scoped to the selected device: %+v", agent)
	}
}

func TestPreLoginStartupColorMatchesConfiguredStartupChoice(t *testing.T) {
	settings := domain.DefaultSettings()
	settings.StartupColorMode = "last"
	settings.RestoreLastColor = false
	settings.LastBrightness = 37
	if color, brightness := preLoginStartupColor(settings, nil); color != (domain.Color{R: 255, G: 255, B: 255}) || brightness != 37 {
		t.Fatalf("default startup color = %+v at %d%%", color, brightness)
	}
	preset := domain.Preset{ID: "favorite", Color: domain.Color{R: 12, G: 180, B: 250}, Brightness: 63}
	settings.StartupColorMode = "favorite"
	settings.FavoritePresetID = preset.ID
	if color, brightness := preLoginStartupColor(settings, []domain.Preset{preset}); color != preset.Color || brightness != preset.Brightness {
		t.Fatalf("favorite startup color = %+v at %d%%", color, brightness)
	}
}
