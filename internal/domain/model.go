package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Color struct {
	R int `json:"r"`
	G int `json:"g"`
	B int `json:"b"`
}

func (c Color) Validate() error {
	if c.R < 0 || c.R > 255 || c.G < 0 || c.G > 255 || c.B < 0 || c.B > 255 {
		return errors.New("RGB channels must be between 0 and 255")
	}
	return nil
}

func (c Color) Hex() string { return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B) }

func ParseHex(s string) (Color, error) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "#"))
	if len(s) != 6 {
		return Color{}, errors.New("enter a six-digit hex colour, such as #A85BFF")
	}
	red, err := strconv.ParseUint(s[0:2], 16, 8)
	if err != nil {
		return Color{}, errors.New("hex colour contains invalid characters")
	}
	green, err := strconv.ParseUint(s[2:4], 16, 8)
	if err != nil {
		return Color{}, errors.New("hex colour contains invalid characters")
	}
	blue, err := strconv.ParseUint(s[4:6], 16, 8)
	if err != nil {
		return Color{}, errors.New("hex colour contains invalid characters")
	}
	return Color{R: int(red), G: int(green), B: int(blue)}, nil
}

// AtBrightness scales the original channels without mutating the source. The
// controller has a native brightness command; this helper is used for preview
// and is tested to prevent cumulative scaling when the slider moves.
func (c Color) AtBrightness(percent int) Color {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return Color{R: c.R * percent / 100, G: c.G * percent / 100, B: c.B * percent / 100}
}

func (c Color) CSS() string { return c.Hex() }

type Device struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	RSSI      int    `json:"rssi"`
	Connected bool   `json:"connected"`
}

type Preset struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Color      Color     `json:"color"`
	Hex        string    `json:"hex"`
	Brightness int       `json:"brightness"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Favorite   bool      `json:"favorite"`
}

func (p Preset) Validate() error {
	if strings.TrimSpace(p.Name) == "" || len([]rune(p.Name)) > 48 {
		return errors.New("preset name must be between 1 and 48 characters")
	}
	if p.ID == "" {
		return errors.New("preset ID is required")
	}
	if strings.IndexFunc(p.Name, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return errors.New("preset name contains unsupported control characters")
	}
	if err := p.Color.Validate(); err != nil {
		return err
	}
	if p.Hex != "" {
		parsed, err := ParseHex(p.Hex)
		if err != nil || parsed != p.Color {
			return errors.New("preset hex value must match its RGB channels")
		}
	}
	if p.Brightness < 0 || p.Brightness > 100 {
		return errors.New("brightness must be between 0 and 100")
	}
	return nil
}

type Settings struct {
	PreferredDevice    Device   `json:"preferredDevice"`
	SavedDevices       []Device `json:"savedDevices"`
	SelectedDevices    []string `json:"selectedDevices"`
	SyncDevices        bool     `json:"syncDevices"`
	LastColor          Color    `json:"lastColor"`
	LastBrightness     int      `json:"lastBrightness"`
	LastPowerOn        bool     `json:"lastPowerOn"`
	LastEffectMode     int      `json:"lastEffectMode"`
	LastEffectSpeed    int      `json:"lastEffectSpeed"`
	LastVisualEffect   string   `json:"lastVisualEffect"`
	VisualEffectColors []Color  `json:"visualEffectColors"`
	VisualEffectSpeed  int      `json:"visualEffectSpeed"`
	LaunchAtLogin      bool     `json:"launchAtLogin"`
	StartupLightsOn    bool     `json:"startupLightsOn"`
	PreLoginLights     bool     `json:"preLoginLights"`
	ShutdownLightsOff  bool     `json:"shutdownLightsOff"`
	RestoreLastColor   bool     `json:"restoreLastColor"`
	StartupColorMode   string   `json:"startupColorMode"` // last, favorite, preset
	StartupPresetID    string   `json:"startupPresetId"`
	FavoritePresetID   string   `json:"favoritePresetId"`
	AutoReconnect      bool     `json:"autoReconnect"`
	MinimizeToTray     bool     `json:"minimizeToTray"`
	CloseToTray        bool     `json:"closeToTray"`
	TurnOffOnAppExit   bool     `json:"turnOffOnAppExit"`
	WindowPositionSet  bool     `json:"windowPositionSet"`
	WindowX            int      `json:"windowX"`
	WindowY            int      `json:"windowY"`
	WindowWidth        int      `json:"windowWidth"`
	WindowHeight       int      `json:"windowHeight"`
	Theme              string   `json:"theme"`
}

func DefaultSettings() Settings {
	return Settings{
		SavedDevices:       []Device{},
		SelectedDevices:    nil,
		LastColor:          Color{R: 168, G: 91, B: 255},
		LastBrightness:     80,
		LastPowerOn:        true,
		LastEffectSpeed:    91,
		VisualEffectColors: []Color{},
		VisualEffectSpeed:  50,
		StartupLightsOn:    true,
		ShutdownLightsOff:  true,
		RestoreLastColor:   true,
		StartupColorMode:   "last",
		AutoReconnect:      true,
		MinimizeToTray:     true,
		CloseToTray:        true,
		WindowWidth:        1120,
		WindowHeight:       820,
		Theme:              "dark",
	}
}

// NormalizeDevices migrates the single-device preference into the device list
// and cleans addresses/selections without inventing a hardware name.
func (s *Settings) NormalizeDevices() {
	if s.VisualEffectColors == nil {
		s.VisualEffectColors = []Color{}
	}
	seen := make(map[string]bool)
	devices := make([]Device, 0, len(s.SavedDevices)+1)
	for _, device := range s.SavedDevices {
		device.Address = NormalizeAddress(device.Address)
		if device.Address == "" || strings.TrimSpace(device.Name) == "" || seen[device.Address] {
			continue
		}
		device.Connected = false
		seen[device.Address] = true
		devices = append(devices, device)
	}
	preferred := s.PreferredDevice
	preferred.Address = NormalizeAddress(preferred.Address)
	if preferred.Address != "" && strings.TrimSpace(preferred.Name) != "" && !seen[preferred.Address] {
		preferred.Connected = false
		devices = append(devices, preferred)
		seen[preferred.Address] = true
	}
	s.SavedDevices = devices
	if s.PreferredDevice.Address == "" && len(devices) > 0 {
		s.PreferredDevice = devices[0]
	} else {
		s.PreferredDevice.Address = NormalizeAddress(s.PreferredDevice.Address)
		if s.PreferredDevice.Name == "" {
			for _, device := range devices {
				if device.Address == s.PreferredDevice.Address {
					s.PreferredDevice = device
					break
				}
			}
		}
	}
	if s.SelectedDevices == nil {
		s.SelectedDevices = []string{}
		if s.PreferredDevice.Address != "" {
			s.SelectedDevices = append(s.SelectedDevices, s.PreferredDevice.Address)
		}
	}
	selected := make([]string, 0, len(s.SelectedDevices))
	selectedSeen := make(map[string]bool)
	for _, address := range s.SelectedDevices {
		address = NormalizeAddress(address)
		if address != "" && seen[address] && !selectedSeen[address] {
			selected = append(selected, address)
			selectedSeen[address] = true
		}
	}
	s.SelectedDevices = selected
}

func (s Settings) Validate() error {
	if err := s.LastColor.Validate(); err != nil {
		return err
	}
	if s.LastBrightness < 0 || s.LastBrightness > 100 {
		return errors.New("brightness must be between 0 and 100")
	}
	if s.LastEffectMode != 0 && (s.LastEffectMode < 7 || s.LastEffectMode > 20) {
		return errors.New("effect mode must be a supported non-blink ELK-BLEDOB mode")
	}
	if s.LastEffectSpeed < 0 || s.LastEffectSpeed > 183 {
		return errors.New("effect speed must be between 0 and 183")
	}
	if s.LastVisualEffect != "" && s.LastVisualEffect != "rainbow" && s.LastVisualEffect != "custom-crossfade" {
		return errors.New("visual effect must be rainbow or custom-crossfade")
	}
	if s.VisualEffectSpeed < 1 || s.VisualEffectSpeed > 100 {
		return errors.New("visual effect speed must be between 1 and 100")
	}
	if len(s.VisualEffectColors) > 8 {
		return errors.New("custom crossfade supports at most 8 colors")
	}
	for _, color := range s.VisualEffectColors {
		if err := color.Validate(); err != nil {
			return err
		}
	}
	if s.LastVisualEffect == "custom-crossfade" && len(s.VisualEffectColors) < 2 {
		return errors.New("custom crossfade requires at least 2 colors")
	}
	if s.WindowWidth != 0 && (s.WindowWidth < 760 || s.WindowWidth > 5000) {
		return errors.New("window width is outside the supported range")
	}
	if s.WindowHeight != 0 && (s.WindowHeight < 600 || s.WindowHeight > 5000) {
		return errors.New("window height is outside the supported range")
	}
	if s.StartupColorMode == "" {
		s.StartupColorMode = "last"
	}
	if s.StartupColorMode != "last" && s.StartupColorMode != "favorite" && s.StartupColorMode != "preset" {
		return errors.New("startup colour mode must be last, favorite, or preset")
	}
	if s.Theme != "" && s.Theme != "dark" && s.Theme != "light" {
		return errors.New("theme must be dark or light")
	}
	addresses := make(map[string]bool, len(s.SavedDevices))
	for _, device := range s.SavedDevices {
		if strings.TrimSpace(device.Name) == "" || NormalizeAddress(device.Address) == "" {
			return errors.New("saved devices must have a name and Bluetooth address")
		}
		address := NormalizeAddress(device.Address)
		if addresses[address] {
			return errors.New("saved device addresses must be unique")
		}
		addresses[address] = true
	}
	for _, address := range s.SelectedDevices {
		if !addresses[NormalizeAddress(address)] {
			return errors.New("selected devices must be in the saved device list")
		}
	}
	return nil
}

func NormalizeAddress(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", ":")
	return s
}
