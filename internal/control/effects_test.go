package control

import (
	"strings"
	"testing"
	"time"

	"lumidesk/internal/domain"
)

func TestNonBlinkELKBLEDOBModesMapToLibraryModeArguments(t *testing.T) {
	effects := EffectOptions()
	if len(effects) != 14 {
		t.Fatalf("effect count = %d, want 14 non-blink modes", len(effects))
	}
	seen := make(map[int]bool)
	for _, effect := range effects {
		if effect.Mode < 7 || effect.Mode > 20 || seen[effect.Mode] || effect.Name == "" || strings.Contains(strings.ToLower(effect.Name), "blink") {
			t.Errorf("invalid effect mapping: %+v", effect)
		}
		seen[effect.Mode] = true
	}
	if !IsEffectSupported("ELK-BLEDOB 66") || IsEffectSupported("ELK-BLEDDM") {
		t.Fatal("effect compatibility should be limited to the documented BLEDOB family")
	}
}

func TestVisualFramesTransitionWithoutBlackFrames(t *testing.T) {
	red := domain.Color{R: 255}
	blue := domain.Color{B: 255}
	frames := []domain.Color{
		visualFrame("custom-crossfade", []domain.Color{red, blue}, 50, 0),
		visualFrame("custom-crossfade", []domain.Color{red, blue}, 50, 3040*time.Millisecond),
		visualFrame("custom-crossfade", []domain.Color{red, blue}, 50, 6080*time.Millisecond),
	}
	if frames[0] != red || frames[2] != blue {
		t.Fatalf("crossfade endpoints were not preserved: %+v", frames)
	}
	if frames[1] == (domain.Color{}) {
		t.Fatal("crossfade inserted an off/black frame between colors")
	}
	for _, frame := range frames {
		if err := frame.Validate(); err != nil {
			t.Fatalf("invalid transition color: %v", err)
		}
	}
	if rainbow := visualFrame("rainbow", nil, 50, time.Second); rainbow == (domain.Color{}) {
		t.Fatal("rainbow generated a black/off frame")
	}
}
