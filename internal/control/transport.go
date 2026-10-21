package control

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lotuslantern "github.com/Rxflex/LotusLantern"
	"tinygo.org/x/bluetooth"

	"lumidesk/internal/domain"
)

type Lamp interface {
	LightOn(bool) error
	ChangeColorRGB(int, int, int) error
	ChangeBrightness(int, int) error
	ChangeMode(int) error
	ChangeModeSpeed(int) error
	Close() error
}

type Transport interface {
	Scan(context.Context, time.Duration) ([]domain.Device, error)
	Connect(context.Context, domain.Device) (Lamp, domain.Device, error)
}

type LotusTransport struct {
	mu             sync.RWMutex
	lamps          map[string]*trackedLamp
	connectionFns  map[uint64]func(domain.Device, bool)
	nextConnection uint64
	handlerOnce    sync.Once
	gateOnce       sync.Once
	gate           *BLECommandGate
}

type BLECommandGate struct {
	mu        sync.Mutex
	lastWrite time.Time
	interval  time.Duration
}

func newBLECommandGate(interval time.Duration) *BLECommandGate {
	return &BLECommandGate{interval: interval}
}

func (g *BLECommandGate) Write(command func() error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if wait := time.Until(g.lastWrite.Add(g.interval)); !g.lastWrite.IsZero() && wait > 0 {
		time.Sleep(wait)
	}
	err := command()
	g.lastWrite = time.Now()
	return err
}

func (g *BLECommandGate) Exclusive(command func() error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return command()
}

func (t *LotusTransport) CommandGate() *BLECommandGate {
	t.gateOnce.Do(func() { t.gate = newBLECommandGate(75 * time.Millisecond) })
	return t.gate
}

func (t *LotusTransport) SetConnectionEventHandler(handler func(domain.Device, bool)) func() {
	t.mu.Lock()
	t.nextConnection++
	id := t.nextConnection
	if t.connectionFns == nil {
		t.connectionFns = make(map[uint64]func(domain.Device, bool))
	}
	t.connectionFns[id] = handler
	if t.lamps == nil {
		t.lamps = make(map[string]*trackedLamp)
	}
	t.mu.Unlock()
	t.handlerOnce.Do(func() {
		bluetooth.DefaultAdapter.SetConnectHandler(func(device bluetooth.Device, connected bool) {
			address := domain.NormalizeAddress(device.Address.String())
			t.mu.RLock()
			lamp := t.lamps[address]
			notifiers := make([]func(domain.Device, bool), 0, len(t.connectionFns))
			for _, notify := range t.connectionFns {
				notifiers = append(notifiers, notify)
			}
			t.mu.RUnlock()
			if lamp == nil {
				return
			}
			changed := false
			if connected {
				changed = lamp.markConnected()
			} else {
				changed = lamp.markDisconnected()
			}
			if changed {
				for _, notify := range notifiers {
					notify(domain.Device{Address: address}, connected)
				}
			}
		})
	})
	return func() {
		t.mu.Lock()
		delete(t.connectionFns, id)
		t.mu.Unlock()
	}
}

// pacedLamp spaces write-without-response commands. BLEDOM clones commonly
// drop GATT or miss frames when writes are sent back-to-back.
type pacedLamp struct {
	inner Lamp
	gate  *BLECommandGate
}

func (p *pacedLamp) write(command func() error) error {
	if p.gate != nil {
		return p.gate.Write(command)
	}
	return command()
}

func (p *pacedLamp) LightOn(on bool) error {
	return p.write(func() error { return p.inner.LightOn(on) })
}
func (p *pacedLamp) ChangeColorRGB(r, g, b int) error {
	return p.write(func() error { return p.inner.ChangeColorRGB(r, g, b) })
}
func (p *pacedLamp) ChangeBrightness(level, mode int) error {
	return p.write(func() error { return p.inner.ChangeBrightness(level, mode) })
}
func (p *pacedLamp) ChangeMode(mode int) error {
	return p.write(func() error { return p.inner.ChangeMode(mode) })
}
func (p *pacedLamp) ChangeModeSpeed(speed int) error {
	return p.write(func() error { return p.inner.ChangeModeSpeed(speed) })
}
func (p *pacedLamp) Close() error {
	if p.gate != nil {
		return p.gate.Exclusive(p.inner.Close)
	}
	return p.inner.Close()
}

type trackedLamp struct {
	inner     Lamp
	connected atomic.Bool
	onClose   func()
	closeOnce sync.Once
}

func (l *trackedLamp) markConnected() bool    { return !l.connected.Swap(true) }
func (l *trackedLamp) markDisconnected() bool { return l.connected.Swap(false) }

func (l *trackedLamp) command(fn func() error) error {
	if !l.connected.Load() {
		return errors.New("BLE connection is no longer active")
	}
	return fn()
}

func (l *trackedLamp) LightOn(on bool) error {
	return l.command(func() error { return l.inner.LightOn(on) })
}
func (l *trackedLamp) ChangeColorRGB(r, g, b int) error {
	return l.command(func() error { return l.inner.ChangeColorRGB(r, g, b) })
}
func (l *trackedLamp) ChangeBrightness(level, mode int) error {
	return l.command(func() error { return l.inner.ChangeBrightness(level, mode) })
}
func (l *trackedLamp) ChangeMode(mode int) error {
	return l.command(func() error { return l.inner.ChangeMode(mode) })
}
func (l *trackedLamp) ChangeModeSpeed(speed int) error {
	return l.command(func() error { return l.inner.ChangeModeSpeed(speed) })
}
func (l *trackedLamp) Close() error {
	wasConnected := l.connected.Swap(false)
	l.closeOnce.Do(func() {
		if l.onClose != nil {
			l.onClose()
		}
	})
	if !wasConnected {
		return nil
	}
	return l.inner.Close()
}

func (t *LotusTransport) Scan(ctx context.Context, timeout time.Duration) ([]domain.Device, error) {
	gate := t.CommandGate()
	gate.mu.Lock()
	defer gate.mu.Unlock()
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return nil, friendlyBLEError(err)
	}
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 8 * time.Second
	}
	devices := make(map[string]domain.Device)
	var devicesMu sync.Mutex
	timer := time.AfterFunc(timeout, func() { _ = adapter.StopScan() })
	stopCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-stopCtx.Done():
			_ = adapter.StopScan()
		case <-time.After(timeout + time.Second):
		}
	}()
	err := adapter.Scan(func(adp *bluetooth.Adapter, result bluetooth.ScanResult) {
		name := strings.TrimSpace(result.LocalName())
		if !supportedName(name) {
			return
		}
		address := domain.NormalizeAddress(result.Address.String())
		if address == "" {
			return
		}
		devicesMu.Lock()
		previous, seen := devices[address]
		if !seen || int(result.RSSI) > previous.RSSI {
			devices[address] = domain.Device{Name: name, Address: address, RSSI: int(result.RSSI)}
		}
		devicesMu.Unlock()
	})
	timer.Stop()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, friendlyBLEError(err)
	}
	devicesMu.Lock()
	out := make([]domain.Device, 0, len(devices))
	for _, device := range devices {
		out = append(out, device)
	}
	devicesMu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].RSSI > out[j].RSSI })
	if len(out) == 0 {
		return []domain.Device{}, nil
	}
	return out, nil
}

func (t *LotusTransport) Connect(ctx context.Context, device domain.Device) (Lamp, domain.Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, device, err
	}
	if device.Address == "" || device.Name == "" {
		return nil, device, errors.New("select a Bluetooth LED controller first")
	}
	address := domain.NormalizeAddress(device.Address)
	lamp, err := t.connect(address, device.Name)
	if err == nil {
		device.Address = address
		return t.track(lamp, device), device, nil
	}
	if !strings.Contains(strings.ToLower(err.Error()), "device not seen in scan") || ctx.Err() != nil {
		return nil, device, friendlyBLEError(err)
	}
	// Some controllers advertise with a rotating private address. If the saved
	// address is no longer present, resolve the same advertised device name once.
	nearby, scanErr := t.Scan(ctx, 3*time.Second)
	if scanErr != nil {
		return nil, device, friendlyBLEError(err)
	}
	for _, candidate := range nearby {
		if !strings.EqualFold(strings.TrimSpace(candidate.Name), strings.TrimSpace(device.Name)) || domain.NormalizeAddress(candidate.Address) == address {
			continue
		}
		if ctx.Err() != nil {
			return nil, device, ctx.Err()
		}
		resolved, connectErr := t.connect(candidate.Address, candidate.Name)
		if connectErr == nil {
			return t.track(resolved, candidate), candidate, nil
		}
		return nil, candidate, friendlyBLEError(connectErr)
	}
	return nil, device, friendlyBLEError(err)
}

func (t *LotusTransport) connect(address, name string) (*lotuslantern.Lamp, error) {
	gate := t.CommandGate()
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return lotuslantern.Connect(address, name)
}

func (t *LotusTransport) track(lamp Lamp, device domain.Device) Lamp {
	device.Address = domain.NormalizeAddress(device.Address)
	tracked := &trackedLamp{inner: lamp}
	tracked.connected.Store(true)
	t.mu.Lock()
	if t.lamps == nil {
		t.lamps = make(map[string]*trackedLamp)
	}
	t.lamps[device.Address] = tracked
	t.mu.Unlock()
	tracked.onClose = func() {
		t.mu.Lock()
		if t.lamps[device.Address] == tracked {
			delete(t.lamps, device.Address)
		}
		t.mu.Unlock()
	}
	return tracked
}

func supportedName(name string) bool {
	upper := strings.ToUpper(name)
	for _, prefix := range []string{"ELK-", "ELK~", "LED LIGHT STRIP", "XSL-"} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

type bleTransportError struct {
	message string
	cause   error
}

func (e *bleTransportError) Error() string { return e.message }
func (e *bleTransportError) Unwrap() error { return e.cause }
func (e *bleTransportError) Diagnostic() string {
	return fmt.Sprintf("%s (Windows BLE detail: %v)", e.message, e.cause)
}

func diagnosticBLEError(err error) string {
	var detail interface{ Diagnostic() string }
	if errors.As(err, &detail) {
		return detail.Diagnostic()
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

func friendlyBLEError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "bluetooth") && strings.Contains(message, "enable"):
		return &bleTransportError{message: "Bluetooth is off or the BLE adapter is unavailable. Turn Bluetooth on and try again", cause: err}
	case strings.Contains(message, "device not seen"), strings.Contains(message, "no lotus lantern lamp found"):
		return &bleTransportError{message: "No compatible LED controller was found. Check that it is powered on and nearby, then scan again", cause: err}
	case strings.Contains(message, "incorrect function"), strings.Contains(message, "async operation failed"), strings.Contains(message, "0x80000013"):
		return &bleTransportError{message: "Windows couldn't open the Bluetooth link to this controller. It may be out of range or already connected to another app. Close other lighting apps, then click Connect to retry", cause: err}
	case strings.Contains(message, "unreachable"), strings.Contains(message, "connect"):
		return &bleTransportError{message: "Windows couldn't connect to the LED controller. Move it closer and close other lighting apps, then retry", cause: err}
	default:
		return err
	}
}
