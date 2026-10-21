package control

import (
	"errors"
	"math"
	"strings"
	"time"

	"lumidesk/internal/domain"
)

const (
	EffectModeStatic = 0
	EffectSpeedMin   = 0
	EffectSpeedMax   = 183
)

type EffectOption struct {
	Mode int    `json:"mode"`
	Name string `json:"name"`
}

// EffectOptions retains the non-blink hardware modes for internal compatibility.
// The user-facing effects use software RGB transitions instead.
var effectOptions = []EffectOption{
	{7, "Jump: RGB"},
	{8, "Jump: rainbow"},
	{9, "Crossfade: RGB"},
	{10, "Crossfade: rainbow"},
	{11, "Crossfade: red"},
	{12, "Crossfade: green"},
	{13, "Crossfade: blue"},
	{14, "Crossfade: yellow"},
	{15, "Crossfade: cyan"},
	{16, "Crossfade: magenta"},
	{17, "Crossfade: white"},
	{18, "Crossfade: red + green"},
	{19, "Crossfade: red + blue"},
	{20, "Crossfade: green + blue"},
}

func EffectOptions() []EffectOption { return append([]EffectOption(nil), effectOptions...) }

func IsEffectSupported(deviceName string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(deviceName)), "ELK-BLEDOB")
}

func validEffectMode(mode int) bool {
	if mode == EffectModeStatic {
		return true
	}
	for _, option := range effectOptions {
		if option.Mode == mode {
			return true
		}
	}
	return false
}

func validVisualEffect(kind string, colors []domain.Color, speed int) error {
	if kind != "rainbow" && kind != "custom-crossfade" {
		return errors.New("choose Rainbow or Custom crossfade")
	}
	if speed < 1 || speed > 100 {
		return errors.New("visual effect speed must be between 1 and 100")
	}
	if kind == "custom-crossfade" && (len(colors) < 2 || len(colors) > 8) {
		return errors.New("custom crossfade needs 2 to 8 colors")
	}
	for _, color := range colors {
		if err := color.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// visualFrameInterval balances visibly smooth changes against BLEDOM GATT
// implementations, which can drop links under rapid writes. pacedLamp also
// enforces a 75ms minimum gap between physical BLE writes.
func visualFrameInterval(speed int) time.Duration {
	speed = max(1, min(speed, 100))
	return time.Duration(180-(speed-1)*1) * time.Millisecond
}

func visualFrame(kind string, colors []domain.Color, speed int, elapsed time.Duration) domain.Color {
	if kind == "rainbow" {
		cycle := time.Duration((30.0 - float64(speed-1)*0.22) * float64(time.Second))
		if cycle <= 0 {
			cycle = 8 * time.Second
		}
		hue := float64(elapsed%cycle) / float64(cycle) * 360
		return hsvColor(hue)
	}
	if len(colors) == 0 {
		return domain.Color{}
	}
	if len(colors) == 1 {
		return colors[0]
	}
	segment := time.Duration((10.0 - float64(speed-1)*0.08) * float64(time.Second))
	if segment <= 0 {
		segment = 2 * time.Second
	}
	cycle := time.Duration(len(colors)) * segment
	position := elapsed % cycle
	index := int(position / segment)
	amount := float64(position%segment) / float64(segment)
	from, to := colors[index%len(colors)], colors[(index+1)%len(colors)]
	return domain.Color{
		R: int(math.Round(float64(from.R) + (float64(to.R)-float64(from.R))*amount)),
		G: int(math.Round(float64(from.G) + (float64(to.G)-float64(from.G))*amount)),
		B: int(math.Round(float64(from.B) + (float64(to.B)-float64(from.B))*amount)),
	}
}

func hsvColor(hue float64) domain.Color {
	h := math.Mod(hue, 360) / 60
	x := 1 - math.Abs(math.Mod(h, 2)-1)
	var r, g, b float64
	switch {
	case h < 1:
		r, g = 1, x
	case h < 2:
		r, g = x, 1
	case h < 3:
		g, b = 1, x
	case h < 4:
		g, b = x, 1
	case h < 5:
		r, b = x, 1
	default:
		r, b = 1, x
	}
	return domain.Color{R: int(math.Round(r * 255)), G: int(math.Round(g * 255)), B: int(math.Round(b * 255))}
}
