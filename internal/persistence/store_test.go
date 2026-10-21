package persistence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"lumidesk/internal/domain"
)

func TestSettingsAndPresetPersistence(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.LastColor = domain.Color{R: 255, G: 32, B: 10}
	settings.LastBrightness = 63
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.LastColor != settings.LastColor || reloaded.LastBrightness != 63 {
		t.Fatalf("settings did not persist: %+v", reloaded)
	}

	preset := testPreset("warm", "Warm Evening", domain.Color{R: 255, G: 124, B: 45}, 72)
	if err := store.SavePresets([]domain.Preset{preset}); err != nil {
		t.Fatal(err)
	}
	presets, err := store.LoadPresets()
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != 1 || presets[0] != preset {
		t.Fatalf("unexpected persisted presets: %+v", presets)
	}
	if got, err := store.ExportPresets(); err != nil || !strings.Contains(got, "Warm Evening") {
		t.Fatalf("export failed or omitted preset: %q, %v", got, err)
	}
}

func TestCorruptedSettingsAreBackedUpAndRecovered(t *testing.T) {
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.SettingsPath(), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(settings, domain.DefaultSettings()) {
		t.Fatalf("corrupt settings were not replaced with defaults: %+v", settings)
	}
	backups, err := filepath.Glob(store.SettingsPath() + ".corrupt-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one preserved corrupt backup, found %v (%v)", backups, err)
	}
	if _, err := os.Stat(store.SettingsPath()); err != nil {
		t.Fatalf("recovered settings file missing: %v", err)
	}
	if warnings := store.RecoveryWarnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "safe defaults") {
		t.Fatalf("settings recovery was not exposed to diagnostics/UI: %v", warnings)
	}
}

func TestPresetImportMergesAndRegeneratesCollidingIDs(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePresets([]domain.Preset{testPreset("same", "Original", domain.Color{R: 1}, 10)}); err != nil {
		t.Fatal(err)
	}
	incoming := []domain.Preset{testPreset("same", "Imported", domain.Color{G: 2}, 20)}
	raw, _ := json.Marshal(incoming)
	result, err := store.ImportPresets(string(raw), false, func() string { return "generated-id" })
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[1].ID != "generated-id" || result[1].Name != "Imported" {
		t.Fatalf("unexpected merged presets: %+v", result)
	}
}

func TestPresetImportRejectsInvalidAndOversizedData(t *testing.T) {
	store, _ := New(t.TempDir())
	for _, raw := range []string{"{}", "not json", strings.Repeat("x", maxImportBytes+1)} {
		if _, err := store.ImportPresets(raw, false, func() string { return "id" }); err == nil {
			t.Errorf("accepted invalid import payload of length %d", len(raw))
		}
	}
	valid := testPreset("valid", "Valid", domain.Color{B: 255}, 100)
	valid.Name = "  "
	raw, _ := json.Marshal([]domain.Preset{valid})
	if _, err := store.ImportPresets(string(raw), true, func() string { return "id" }); err == nil {
		t.Fatal("accepted a preset with a blank name")
	}
}

func TestLegacyPresetWithoutHexIsMigrated(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	legacy := testPreset("legacy", "Legacy Blue", domain.Color{R: 9, G: 87, B: 231}, 75)
	legacy.Hex = ""
	raw, _ := json.Marshal([]domain.Preset{legacy})
	if err := os.WriteFile(store.PresetsPath(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	presets, err := store.LoadPresets()
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != 1 || presets[0].Hex != "#0957E7" {
		t.Fatalf("legacy preset hex wasn't migrated: %+v", presets)
	}
	var persisted []domain.Preset
	b, err := os.ReadFile(store.PresetsPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &persisted); err != nil || len(persisted) != 1 || persisted[0].Hex != "#0957E7" {
		t.Fatalf("hex migration wasn't saved to disk: %+v, %v", persisted, err)
	}
}

func TestLegacyPreferredDeviceMigratesWithoutHardcodingANewDevice(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	legacy := domain.DefaultSettings()
	legacy.PreferredDevice = domain.Device{Name: "User Strip", Address: "aa:bb:cc:dd:ee:03"}
	legacy.SavedDevices = nil
	legacy.SelectedDevices = nil
	b, _ := json.Marshal(legacy)
	if err := os.WriteFile(store.SettingsPath(), b, 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.SavedDevices) != 1 || settings.SavedDevices[0].Name != "User Strip" || len(settings.SelectedDevices) != 1 || settings.SelectedDevices[0] != "AA:BB:CC:DD:EE:03" {
		t.Fatalf("legacy device was not migrated: %+v", settings)
	}
	defaults := domain.DefaultSettings()
	if defaults.PreferredDevice.Name != "" || len(defaults.SavedDevices) != 0 {
		t.Fatalf("fresh defaults should not assume a particular controller: %+v", defaults)
	}
}

func TestOldHardwareEffectSelectionIsClearedWithoutLosingSettings(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	legacy := domain.DefaultSettings()
	legacy.LastEffectMode = 22 // Old blink mode.
	device := domain.Device{Name: "ELK-BLEDOB 66", Address: "AA:BB:CC:DD:EE:66"}
	legacy.PreferredDevice = device
	legacy.SavedDevices = []domain.Device{device}
	legacy.SelectedDevices = []string{device.Address}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.SettingsPath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.LastEffectMode != 0 || len(reloaded.SavedDevices) != 1 || reloaded.SavedDevices[0] != device {
		t.Fatalf("old hardware effect migration lost settings or kept blink active: %+v", reloaded)
	}
}

func TestAtomicSaveLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	store, _ := New(dir)
	settings := domain.DefaultSettings()
	settings.WindowX = 42
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(dir); err != nil {
		t.Fatal(err)
	} else {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".tmp") {
				t.Errorf("temporary file left behind: %s", entry.Name())
			}
		}
	}
}

func testPreset(id, name string, color domain.Color, brightness int) domain.Preset {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return domain.Preset{ID: id, Name: name, Color: color, Hex: color.Hex(), Brightness: brightness, CreatedAt: now, UpdatedAt: now}
}
