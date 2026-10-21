package persistence

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lumidesk/internal/domain"
)

const maxImportBytes = 5 << 20

type Store struct {
	dir      string
	mu       sync.Mutex
	warnings []string
}

func New(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("configuration directory is empty")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create configuration directory: %w", err)
	}
	return &Store{dir: dir}, nil
}

func InUserConfig() (*Store, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("locate user configuration directory: %w", err)
	}
	return New(filepath.Join(base, "LumiDesk"))
}

func (s *Store) Directory() string    { return s.dir }
func (s *Store) SettingsPath() string { return filepath.Join(s.dir, "settings.json") }
func (s *Store) PresetsPath() string  { return filepath.Join(s.dir, "presets.json") }

func (s *Store) RecoveryWarnings() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.warnings...)
}

func (s *Store) LoadSettings() (domain.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	settings := domain.DefaultSettings()
	b, err := os.ReadFile(s.SettingsPath())
	if errors.Is(err, os.ErrNotExist) {
		if saveErr := writeJSONAtomic(s.SettingsPath(), settings); saveErr != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("Settings could not be initialized and will use safe in-memory defaults: %v", saveErr))
		}
		return settings, nil
	}
	if err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("Settings could not be read and safe in-memory defaults will be used: %v", err))
		return settings, nil
	}
	if err := json.Unmarshal(b, &settings); err != nil {
		s.recoverSettings(fmt.Sprintf("Settings JSON was invalid (%v)", err))
		return domain.DefaultSettings(), nil
	}
	// Hardware blink/jump modes were unreliable on the target strips and are
	// replaced by LumiDesk's smooth, direct-RGB effects.
	migrateOldEffect := settings.LastEffectMode != 0
	if migrateOldEffect {
		settings.LastEffectMode = 0
	}
	settings.NormalizeDevices()
	if err := settings.Validate(); err != nil {
		s.recoverSettings(fmt.Sprintf("Settings contained invalid values (%v)", err))
		return domain.DefaultSettings(), nil
	}
	if migrateOldEffect {
		if err := writeJSONAtomic(s.SettingsPath(), settings); err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("The old hardware effect selection was cleared in memory but could not be migrated on disk: %v", err))
		}
	}
	return settings, nil
}

func (s *Store) SaveSettings(settings domain.Settings) error {
	settings.NormalizeDevices()
	if err := settings.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSONAtomic(s.SettingsPath(), settings)
}

func (s *Store) LoadPresets() ([]domain.Preset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, err := os.ReadFile(s.PresetsPath())
	if errors.Is(err, os.ErrNotExist) {
		presets := []domain.Preset{}
		if saveErr := writeJSONAtomic(s.PresetsPath(), presets); saveErr != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("Preset data could not be initialized and an empty in-memory list will be used: %v", saveErr))
		}
		return presets, nil
	}
	if err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("Preset data could not be read and an empty in-memory list will be used: %v", err))
		return []domain.Preset{}, nil
	}
	var presets []domain.Preset
	if err := json.Unmarshal(b, &presets); err != nil || presets == nil {
		reason := "Preset data was empty or invalid"
		if err != nil {
			reason = fmt.Sprintf("Preset JSON was invalid (%v)", err)
		}
		return s.recoverPresets(reason), nil
	}
	needsMigration := false
	for i := range presets {
		if presets[i].Hex == "" {
			presets[i].Hex = presets[i].Color.Hex()
			needsMigration = true
		}
	}
	if err := validatePresetList(presets); err != nil {
		return s.recoverPresets(fmt.Sprintf("Preset data failed validation (%v)", err)), nil
	}
	if needsMigration {
		if err := writeJSONAtomic(s.PresetsPath(), presets); err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("Preset data was loaded but its hex values could not be upgraded on disk: %v", err))
		}
	}
	return presets, nil
}

func (s *Store) SavePresets(presets []domain.Preset) error {
	presets = append([]domain.Preset(nil), presets...)
	for i := range presets {
		if presets[i].Hex == "" {
			presets[i].Hex = presets[i].Color.Hex()
		}
	}
	if err := validatePresetList(presets); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSONAtomic(s.PresetsPath(), presets)
}

func (s *Store) ExportPresets() (string, error) {
	presets, err := s.LoadPresets()
	if err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(presets, "", "  ")
	return string(b), err
}

func (s *Store) ImportPresets(raw string, replace bool, newID func() string) ([]domain.Preset, error) {
	if len(raw) > maxImportBytes {
		return nil, errors.New("preset file is larger than 5 MB")
	}
	var incoming []domain.Preset
	if err := json.Unmarshal([]byte(raw), &incoming); err != nil {
		return nil, fmt.Errorf("could not read preset file: %w", err)
	}
	if incoming == nil {
		return nil, errors.New("preset file must contain a JSON array")
	}
	if len(incoming) > 10000 {
		return nil, errors.New("preset file contains too many entries")
	}
	for i := range incoming {
		incoming[i].Name = strings.TrimSpace(incoming[i].Name)
		if incoming[i].ID == "" {
			incoming[i].ID = newID()
		}
		if incoming[i].CreatedAt.IsZero() {
			incoming[i].CreatedAt = time.Now().UTC()
		}
		if incoming[i].UpdatedAt.IsZero() {
			incoming[i].UpdatedAt = incoming[i].CreatedAt
		}
		if err := incoming[i].Validate(); err != nil {
			return nil, fmt.Errorf("preset %d is invalid: %w", i+1, err)
		}
	}
	if err := validatePresetList(incoming); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	merged := incoming
	if !replace {
		var current []domain.Preset
		b, err := os.ReadFile(s.PresetsPath())
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read existing presets: %w", err)
		}
		if err == nil {
			if err := json.Unmarshal(b, &current); err != nil || validatePresetList(current) != nil {
				return nil, errors.New("existing presets are invalid; repair or export them before importing")
			}
		}
		ids := make(map[string]bool, len(current)+len(incoming))
		for _, p := range current {
			ids[p.ID] = true
		}
		merged = append([]domain.Preset(nil), current...)
		for _, p := range incoming {
			if ids[p.ID] {
				p.ID = newID()
			}
			ids[p.ID] = true
			merged = append(merged, p)
		}
	}
	for i := range merged {
		if merged[i].Hex == "" {
			merged[i].Hex = merged[i].Color.Hex()
		}
	}
	if err := validatePresetList(merged); err != nil {
		return nil, err
	}
	if err := writeJSONAtomic(s.PresetsPath(), merged); err != nil {
		return nil, err
	}
	return merged, nil
}

func validatePresetList(presets []domain.Preset) error {
	ids := make(map[string]struct{}, len(presets))
	favorites := 0
	for i, p := range presets {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("preset %d: %w", i+1, err)
		}
		if _, ok := ids[p.ID]; ok {
			return fmt.Errorf("duplicate preset ID %q", p.ID)
		}
		ids[p.ID] = struct{}{}
		if p.Favorite {
			favorites++
		}
	}
	if favorites > 1 {
		return errors.New("only one preset can be favourite")
	}
	return nil
}

func (s *Store) recoverSettings(reason string) {
	backup, err := s.backupCorrupt(s.SettingsPath())
	if err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("%s; the original file could not be moved (%v), so it was left untouched and defaults are in use", reason, err))
		return
	}
	if err := writeJSONAtomic(s.SettingsPath(), domain.DefaultSettings()); err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("%s; the original was kept at %s but defaults could not be written (%v)", reason, filepath.Base(backup), err))
		return
	}
	s.warnings = append(s.warnings, fmt.Sprintf("%s; safe defaults were restored and the original was kept at %s", reason, filepath.Base(backup)))
}

func (s *Store) recoverPresets(reason string) []domain.Preset {
	backup, err := s.backupCorrupt(s.PresetsPath())
	if err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("%s; the original file could not be moved (%v), so it was left untouched and an empty in-memory list is in use", reason, err))
		return []domain.Preset{}
	}
	presets := []domain.Preset{}
	if err := writeJSONAtomic(s.PresetsPath(), presets); err != nil {
		s.warnings = append(s.warnings, fmt.Sprintf("%s; the original was kept at %s but an empty list could not be written (%v)", reason, filepath.Base(backup), err))
		return presets
	}
	s.warnings = append(s.warnings, fmt.Sprintf("%s; an empty list was restored and the original was kept at %s", reason, filepath.Base(backup)))
	return presets
}

func (s *Store) backupCorrupt(path string) (string, error) {
	backup := fmt.Sprintf("%s.corrupt-%s", path, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.Rename(path, backup); err != nil {
		return "", err
	}
	return backup, nil
}

func writeJSONAtomic(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(b, '\n'))
}
