package domain

import "testing"

func TestHexRoundTrip(t *testing.T) {
	want := Color{R: 11, G: 171, B: 255}
	if got := want.Hex(); got != "#0BABFF" {
		t.Fatalf("Hex() = %q", got)
	}
	got, err := ParseHex(" #0babff ")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ParseHex() = %+v, want %+v", got, want)
	}
}

func TestParseHexRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{"", "#FFF", "GG0000", "#1234567", "12 456"} {
		if _, err := ParseHex(input); err == nil {
			t.Errorf("ParseHex(%q) unexpectedly succeeded", input)
		}
	}
}

func TestBrightnessDoesNotMutateOriginalColor(t *testing.T) {
	original := Color{R: 240, G: 127, B: 32}
	if got := original.AtBrightness(50); got != (Color{R: 120, G: 63, B: 16}) {
		t.Fatalf("AtBrightness(50) = %+v", got)
	}
	if got := original.AtBrightness(25); got != (Color{R: 60, G: 31, B: 8}) {
		t.Fatalf("second brightness calculation was cumulative: %+v", got)
	}
	if original != (Color{R: 240, G: 127, B: 32}) {
		t.Fatalf("original color changed: %+v", original)
	}
}

func TestSettingsAndPresetValidation(t *testing.T) {
	settings := DefaultSettings()
	if err := settings.Validate(); err != nil {
		t.Fatalf("default settings invalid: %v", err)
	}
	settings.LastEffectMode = 21
	if err := settings.Validate(); err == nil {
		t.Fatal("accepted a removed hardware blink mode")
	}
	settings.LastEffectMode = 0
	settings.LastBrightness = 101
	if err := settings.Validate(); err == nil {
		t.Fatal("accepted brightness outside 0..100")
	}
	preset := Preset{ID: "one", Name: "Gaming Purple", Color: Color{R: 168, G: 91, B: 255}, Brightness: 80}
	if err := preset.Validate(); err != nil {
		t.Fatalf("valid preset rejected: %v", err)
	}
	preset.Name = "bad\nname"
	if err := preset.Validate(); err == nil {
		t.Fatal("accepted a preset name containing a control character")
	}
	preset.Name = "Gaming Purple"
	preset.Hex = "#FFFFFF"
	if err := preset.Validate(); err == nil {
		t.Fatal("accepted a hex value that disagrees with the RGB channels")
	}
}
