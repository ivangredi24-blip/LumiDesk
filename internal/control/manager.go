package control

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"lumidesk/internal/domain"
)

type State struct {
	Connected              bool            `json:"connected"`
	Connecting             bool            `json:"connecting"`
	Scanning               bool            `json:"scanning"`
	DeviceName             string          `json:"deviceName"`
	DeviceAddress          string          `json:"deviceAddress"`
	Devices                []domain.Device `json:"devices"`
	ConnectedDevices       []domain.Device `json:"connectedDevices"`
	SelectedDevices        []string        `json:"selectedDevices"`
	SyncDevices            bool            `json:"syncDevices"`
	Color                  domain.Color    `json:"color"`
	Brightness             int             `json:"brightness"`
	Powered                bool            `json:"powered"`
	EffectsSupported       bool            `json:"effectsSupported"`
	VisualEffectsAvailable bool            `json:"visualEffectsAvailable"`
	EffectMode             int             `json:"effectMode"`
	EffectSpeed            int             `json:"effectSpeed"`
	ActiveEffect           string          `json:"activeEffect"`
	EffectColors           []domain.Color  `json:"effectColors"`
	VisualEffectSpeed      int             `json:"visualEffectSpeed"`
	LastError              string          `json:"lastError"`
	LastCommandAt          time.Time       `json:"lastCommandAt"`
}

type Callbacks struct {
	State    func(State)
	Settings func(domain.Settings)
	Log      func(string, ...any)
}

type ControllerManager interface {
	Start(context.Context)
	Close(context.Context) error
	Snapshot() State
	Settings() domain.Settings
	SetSettings(domain.Settings, []domain.Preset)
	Scan(context.Context) ([]domain.Device, error)
	Connect(context.Context, domain.Device) error
	Disconnect(context.Context) error
	DisconnectDevice(context.Context, string) error
	RemoveDevice(context.Context, string) error
	Reconnect(context.Context) error
	ReconnectDevice(context.Context, string) error
	SetColor(context.Context, domain.Color) error
	SetBrightness(context.Context, int) error
	SetPower(context.Context, bool) error
	ApplyPreset(context.Context, domain.Color, int) error
	SetEffectMode(context.Context, int) error
	SetEffectSpeed(context.Context, int) error
	PowerOffForShutdown(context.Context) error
}

type operationKind uint8

const (
	opScan operationKind = iota
	opConnect
	opDisconnect
	opReconnect
	opColor
	opBrightness
	opPower
	opShutdownOff
	opPreset
	opEffectMode
	opEffectSpeed
	opEffectFrame
)

type operation struct {
	kind    operationKind
	device  domain.Device
	color   domain.Color
	level   int
	mode    int
	effect  domain.Color
	on      bool
	ctx     context.Context
	reply   chan response
	replies []chan response
}

type response struct {
	devices []domain.Device
	err     error
}

type connectionEvent struct {
	device    domain.Device
	connected bool
}

type Manager struct {
	transport Transport
	callbacks Callbacks

	stateMu  sync.RWMutex
	state    State
	cfgMu    sync.RWMutex
	settings domain.Settings
	presets  []domain.Preset

	queue        chan *operation
	priority     chan *operation
	colorMu      sync.Mutex
	pendingColor *operation

	ctx                      context.Context
	cancel                   context.CancelFunc
	done                     chan struct{}
	events                   chan connectionEvent
	removeConnectionListener func()
	removeListenerOnce       sync.Once
}

func NewManager(transport Transport, settings domain.Settings, presets []domain.Preset, callbacks Callbacks) *Manager {
	if transport == nil {
		transport = &LotusTransport{}
	}
	manager := &Manager{
		transport: transport, callbacks: callbacks, settings: settings, presets: append([]domain.Preset(nil), presets...),
		queue: make(chan *operation, 64), priority: make(chan *operation, 2), done: make(chan struct{}), events: make(chan connectionEvent, 32),
		state: State{Color: settings.LastColor, Brightness: settings.LastBrightness, Powered: settings.LastPowerOn, EffectMode: settings.LastEffectMode, EffectSpeed: settings.LastEffectSpeed, EffectColors: []domain.Color{}, VisualEffectSpeed: settings.VisualEffectSpeed, Devices: append([]domain.Device{}, settings.SavedDevices...), ConnectedDevices: []domain.Device{}, SelectedDevices: append([]string{}, settings.SelectedDevices...), SyncDevices: settings.SyncDevices},
	}
	return manager
}

func (m *Manager) Start(parent context.Context) {
	m.ctx, m.cancel = context.WithCancel(parent)
	if listener, ok := m.transport.(interface {
		SetConnectionEventHandler(func(domain.Device, bool)) func()
	}); ok {
		m.removeConnectionListener = listener.SetConnectionEventHandler(m.handleConnectionEvent)
	}
	m.publishState()
	go m.run()
}

func (m *Manager) Close(ctx context.Context) error {
	if m.cancel == nil {
		return nil
	}
	m.cancel()
	select {
	case <-m.done:
		m.removeListenerOnce.Do(func() {
			if m.removeConnectionListener != nil {
				m.removeConnectionListener()
			}
		})
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Snapshot() State {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	copy := m.state
	copy.Devices = append([]domain.Device{}, m.state.Devices...)
	copy.ConnectedDevices = append([]domain.Device{}, m.state.ConnectedDevices...)
	copy.SelectedDevices = append([]string{}, m.state.SelectedDevices...)
	copy.EffectColors = append([]domain.Color{}, m.state.EffectColors...)
	return copy
}

func (m *Manager) Settings() domain.Settings {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	settings := m.settings
	settings.SavedDevices = append([]domain.Device{}, settings.SavedDevices...)
	settings.SelectedDevices = append([]string{}, settings.SelectedDevices...)
	settings.VisualEffectColors = append([]domain.Color{}, settings.VisualEffectColors...)
	return settings
}

func (m *Manager) SetSettings(settings domain.Settings, presets []domain.Preset) {
	settings.NormalizeDevices()
	m.cfgMu.Lock()
	m.settings = settings
	m.settings.VisualEffectColors = append([]domain.Color{}, settings.VisualEffectColors...)
	m.presets = append([]domain.Preset(nil), presets...)
	m.cfgMu.Unlock()
	m.stateMu.Lock()
	m.state.SelectedDevices = append([]string{}, settings.SelectedDevices...)
	m.state.SyncDevices = settings.SyncDevices
	m.state.EffectMode = settings.LastEffectMode
	m.state.EffectSpeed = settings.LastEffectSpeed
	m.state.VisualEffectSpeed = settings.VisualEffectSpeed
	m.stateMu.Unlock()
}

func (m *Manager) Scan(ctx context.Context) ([]domain.Device, error) {
	r := m.submit(ctx, &operation{kind: opScan})
	return r.devices, r.err
}

func (m *Manager) Connect(ctx context.Context, device domain.Device) error {
	device.Address = domain.NormalizeAddress(device.Address)
	return m.submit(ctx, &operation{kind: opConnect, device: device}).err
}

func (m *Manager) Disconnect(ctx context.Context) error {
	return m.submit(ctx, &operation{kind: opDisconnect}).err
}

func (m *Manager) DisconnectDevice(ctx context.Context, address string) error {
	if m.Snapshot().DeviceAddress == "" || domain.NormalizeAddress(m.Snapshot().DeviceAddress) == domain.NormalizeAddress(address) {
		return m.Disconnect(ctx)
	}
	return nil
}

func (m *Manager) RemoveDevice(ctx context.Context, address string) error {
	return m.DisconnectDevice(ctx, address)
}

func (m *Manager) Reconnect(ctx context.Context) error {
	return m.submit(ctx, &operation{kind: opReconnect}).err
}

func (m *Manager) ReconnectDevice(ctx context.Context, address string) error {
	settings := m.Settings()
	for _, device := range settings.SavedDevices {
		if domain.NormalizeAddress(device.Address) == domain.NormalizeAddress(address) {
			return m.Connect(ctx, device)
		}
	}
	if domain.NormalizeAddress(settings.PreferredDevice.Address) == domain.NormalizeAddress(address) {
		return m.Connect(ctx, settings.PreferredDevice)
	}
	return errors.New("controller is not in the saved device list")
}

func (m *Manager) SetColor(ctx context.Context, color domain.Color) error {
	if err := color.Validate(); err != nil {
		return err
	}
	return m.submitColor(ctx, color)
}

func (m *Manager) SetVisualColorFrame(ctx context.Context, color domain.Color) error {
	if err := color.Validate(); err != nil {
		return err
	}
	return m.submit(ctx, &operation{kind: opEffectFrame, color: color}).err
}

func (m *Manager) SetBrightness(ctx context.Context, brightness int) error {
	if brightness < 0 || brightness > 100 {
		return errors.New("brightness must be between 0 and 100")
	}
	return m.submit(ctx, &operation{kind: opBrightness, level: brightness}).err
}

func (m *Manager) SetPower(ctx context.Context, on bool) error {
	return m.submit(ctx, &operation{kind: opPower, on: on}).err
}

func (m *Manager) ApplyPreset(ctx context.Context, color domain.Color, brightness int) error {
	if err := color.Validate(); err != nil {
		return err
	}
	if brightness < 0 || brightness > 100 {
		return errors.New("brightness must be between 0 and 100")
	}
	return m.submit(ctx, &operation{kind: opPreset, color: color, level: brightness}).err
}

func (m *Manager) SetEffectMode(ctx context.Context, mode int) error {
	if !validEffectMode(mode) {
		return errors.New("select a supported ELK-BLEDOB effect")
	}
	return m.submit(ctx, &operation{kind: opEffectMode, mode: mode}).err
}

func (m *Manager) SetEffectSpeed(ctx context.Context, speed int) error {
	if speed < EffectSpeedMin || speed > EffectSpeedMax {
		return errors.New("effect speed must be between 0 and 183")
	}
	return m.submit(ctx, &operation{kind: opEffectSpeed, level: speed}).err
}

// PowerOffForShutdown intentionally does not reconnect. Shutdown gets a short,
// best-effort window to send the native OFF command through the existing link.
func (m *Manager) PowerOffForShutdown(ctx context.Context) error {
	if !m.Snapshot().Connected {
		return errors.New("controller is not connected; shutdown OFF was not sent")
	}
	return m.submitPriority(ctx, &operation{kind: opShutdownOff}).err
}

func (m *Manager) submit(ctx context.Context, op *operation) response {
	if ctx == nil {
		ctx = context.Background()
	}
	op.ctx = ctx
	op.reply = make(chan response, 1)
	select {
	case m.queue <- op:
	case <-ctx.Done():
		return response{err: ctx.Err()}
	case <-m.done:
		return response{err: errors.New("Bluetooth manager is closed")}
	}
	select {
	case r := <-op.reply:
		return r
	case <-ctx.Done():
		return response{err: ctx.Err()}
	case <-m.done:
		return response{err: errors.New("Bluetooth manager is closed")}
	}
}

func (m *Manager) submitPriority(ctx context.Context, op *operation) response {
	if ctx == nil {
		ctx = context.Background()
	}
	op.ctx = ctx
	op.reply = make(chan response, 1)
	select {
	case m.priority <- op:
	case <-ctx.Done():
		return response{err: ctx.Err()}
	case <-m.done:
		return response{err: errors.New("Bluetooth manager is closed")}
	}
	select {
	case r := <-op.reply:
		return r
	case <-ctx.Done():
		return response{err: ctx.Err()}
	case <-m.done:
		return response{err: errors.New("Bluetooth manager is closed")}
	}
}

func (m *Manager) submitColor(ctx context.Context, color domain.Color) error {
	if ctx == nil {
		ctx = context.Background()
	}
	reply := make(chan response, 1)
	m.colorMu.Lock()
	op := m.pendingColor
	if op == nil {
		op = &operation{kind: opColor, color: color, ctx: ctx, reply: reply, replies: []chan response{reply}}
		m.pendingColor = op
		m.colorMu.Unlock()
		select {
		case m.queue <- op:
		case <-ctx.Done():
			m.colorMu.Lock()
			if m.pendingColor == op {
				m.pendingColor = nil
			}
			m.colorMu.Unlock()
			return ctx.Err()
		case <-m.done:
			return errors.New("Bluetooth manager is closed")
		}
	} else {
		op.color = color
		op.replies = append(op.replies, reply)
		m.colorMu.Unlock()
	}
	select {
	case r := <-reply:
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	case <-m.done:
		return errors.New("Bluetooth manager is closed")
	}
}

func (m *Manager) run() {
	defer close(m.done)
	settings := m.Settings()
	startupPending := true
	manuallyDisconnected := false
	retryDelay := 2 * time.Second
	nextRetry := time.Now()
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	var lamp Lamp
	defer func() {
		if lamp != nil {
			_ = lamp.Close()
		}
	}()

	for {
		select {
		case <-m.ctx.Done():
			m.setConnection(false, false, "", "")
			return
		default:
		}
		// Give the shutdown-priority channel first refusal before normal work.
		select {
		case op := <-m.priority:
			m.process(op, &lamp, &settings, &startupPending, &manuallyDisconnected, &retryDelay, &nextRetry)
			continue
		default:
		}
		select {
		case <-m.ctx.Done():
			m.setConnection(false, false, "", "")
			return
		case op := <-m.priority:
			m.process(op, &lamp, &settings, &startupPending, &manuallyDisconnected, &retryDelay, &nextRetry)
		case event := <-m.events:
			m.applyConnectionEvent(event, &retryDelay, &nextRetry)
		case op := <-m.queue:
			if op.kind == opColor {
				m.colorMu.Lock()
				if m.pendingColor == op {
					m.pendingColor = nil
				}
				m.colorMu.Unlock()
			}
			m.process(op, &lamp, &settings, &startupPending, &manuallyDisconnected, &retryDelay, &nextRetry)
		case <-tick.C:
			settings = m.Settings()
			state := m.Snapshot()
			if !state.Connected && settings.AutoReconnect && !manuallyDisconnected && settings.PreferredDevice.Address != "" && !time.Now().Before(nextRetry) {
				m.log("automatic reconnect attempt to %s", settings.PreferredDevice.Name)
				if err := m.connect(m.ctx, settings.PreferredDevice, &lamp, &startupPending, false, true); err != nil {
					m.fail(err)
					nextRetry = time.Now().Add(retryDelay)
					retryDelay = minDuration(retryDelay*2, 60*time.Second)
				} else {
					retryDelay = 2 * time.Second
				}
			}
		}
	}
}

func (m *Manager) process(op *operation, lamp *Lamp, settings *domain.Settings, startupPending, manuallyDisconnected *bool, retryDelay *time.Duration, nextRetry *time.Time) {
	if op.ctx != nil && op.ctx.Err() != nil {
		m.reply(op, response{err: op.ctx.Err()})
		return
	}
	var r response
	switch op.kind {
	case opScan:
		m.setScanning(true, "")
		devices, err := m.transport.Scan(op.ctx, 8*time.Second)
		if err != nil {
			m.setScanning(false, err.Error())
			r.err = err
		} else {
			current := m.Snapshot()
			for i := range devices {
				devices[i].Connected = current.Connected && domain.NormalizeAddress(devices[i].Address) == domain.NormalizeAddress(current.DeviceAddress)
			}
			m.setDevices(devices)
			m.setScanning(false, "")
			r.devices = devices
		}
	case opConnect:
		*manuallyDisconnected = false
		op.device.Address = domain.NormalizeAddress(op.device.Address)
		if err := m.connect(op.ctx, op.device, lamp, startupPending, true, true); err != nil {
			r.err = err
			*nextRetry = time.Now().Add(*retryDelay)
			*retryDelay = minDuration(*retryDelay*2, 60*time.Second)
		} else {
			*retryDelay = 2 * time.Second
			*nextRetry = time.Now().Add(*retryDelay)
			*settings = m.Settings()
		}
	case opDisconnect:
		*manuallyDisconnected = true
		m.setConnection(false, false, "", "")
		if *lamp != nil {
			if err := (*lamp).Close(); err != nil {
				r.err = err
			}
			*lamp = nil
		}
	case opReconnect:
		*manuallyDisconnected = false
		*settings = m.Settings()
		m.setConnection(false, true, settings.PreferredDevice.Name, domain.NormalizeAddress(settings.PreferredDevice.Address))
		if *lamp != nil {
			_ = (*lamp).Close()
			*lamp = nil
		}
		if err := m.connect(op.ctx, settings.PreferredDevice, lamp, startupPending, true, true); err != nil {
			r.err = err
			*nextRetry = time.Now().Add(*retryDelay)
			*retryDelay = minDuration(*retryDelay*2, 60*time.Second)
		} else {
			*retryDelay = 2 * time.Second
			*nextRetry = time.Now().Add(*retryDelay)
		}
	case opColor:
		if err := m.ensureConnected(op.ctx, lamp, startupPending, manuallyDisconnected, true); err != nil {
			r.err = err
			break
		}
		if err := (*lamp).ChangeColorRGB(op.color.R, op.color.G, op.color.B); err != nil {
			r.err = m.commandFailed(err, lamp)
			break
		}
		m.updateColor(op.color, settings)
	case opBrightness:
		if err := m.ensureConnected(op.ctx, lamp, startupPending, manuallyDisconnected, true); err != nil {
			r.err = err
			break
		}
		if err := (*lamp).ChangeBrightness(percentToController(op.level), 0); err != nil {
			r.err = m.commandFailed(err, lamp)
			break
		}
		m.updateBrightness(op.level, settings)
	case opPower:
		if err := m.ensureConnected(op.ctx, lamp, startupPending, manuallyDisconnected, op.on); err != nil {
			r.err = err
			break
		}
		if op.on {
			state := m.Snapshot()
			if state.EffectMode != 0 && state.EffectsSupported {
				if err := (*lamp).ChangeMode(state.EffectMode); err != nil {
					r.err = m.commandFailed(err, lamp)
					break
				}
			} else {
				if err := (*lamp).ChangeColorRGB(state.Color.R, state.Color.G, state.Color.B); err != nil {
					r.err = m.commandFailed(err, lamp)
					break
				}
				if err := (*lamp).ChangeBrightness(percentToController(state.Brightness), 0); err != nil {
					r.err = m.commandFailed(err, lamp)
					break
				}
			}
		}
		if err := (*lamp).LightOn(op.on); err != nil {
			r.err = m.commandFailed(err, lamp)
			break
		}
		m.updatePower(op.on, settings)
	case opShutdownOff:
		if *lamp == nil || !m.Snapshot().Connected {
			r.err = errors.New("controller is not connected; shutdown OFF was not sent")
			break
		}
		if err := (*lamp).LightOn(false); err != nil {
			r.err = m.commandFailed(err, lamp)
			break
		}
		m.updatePower(false, settings)
	case opPreset:
		if err := m.ensureConnected(op.ctx, lamp, startupPending, manuallyDisconnected, false); err != nil {
			r.err = err
			break
		}
		if err := (*lamp).ChangeColorRGB(op.color.R, op.color.G, op.color.B); err != nil {
			r.err = m.commandFailed(err, lamp)
			break
		}
		if err := (*lamp).ChangeBrightness(percentToController(op.level), 0); err != nil {
			r.err = m.commandFailed(err, lamp)
			break
		}
		if err := (*lamp).LightOn(true); err != nil {
			r.err = m.commandFailed(err, lamp)
			break
		}
		m.updatePreset(op.color, op.level, settings)
	case opEffectMode:
		restore := op.mode == EffectModeStatic
		if err := m.ensureConnected(op.ctx, lamp, startupPending, manuallyDisconnected, restore); err != nil {
			r.err = err
			break
		}
		if op.mode != EffectModeStatic && !m.Snapshot().EffectsSupported {
			r.err = errors.New("dynamic effects are documented only for ELK-BLEDOB controllers")
			break
		}
		if op.mode == EffectModeStatic {
			state := m.Snapshot()
			if err := (*lamp).ChangeColorRGB(state.Color.R, state.Color.G, state.Color.B); err != nil {
				r.err = m.commandFailed(err, lamp)
				break
			}
			if err := (*lamp).ChangeBrightness(percentToController(state.Brightness), 0); err != nil {
				r.err = m.commandFailed(err, lamp)
				break
			}
		} else {
			wasPowered := m.Snapshot().Powered
			if err := (*lamp).ChangeMode(op.mode); err != nil {
				r.err = m.commandFailed(err, lamp)
				break
			}
			if !wasPowered {
				if err := (*lamp).LightOn(true); err != nil {
					r.err = m.commandFailed(err, lamp)
					break
				}
			}
		}
		m.updateEffectMode(op.mode, settings)
	case opEffectSpeed:
		if err := m.ensureConnected(op.ctx, lamp, startupPending, manuallyDisconnected, true); err != nil {
			r.err = err
			break
		}
		if !m.Snapshot().EffectsSupported {
			r.err = errors.New("dynamic effects are documented only for ELK-BLEDOB controllers")
			break
		}
		if m.Snapshot().EffectMode == EffectModeStatic {
			r.err = errors.New("select a dynamic effect before adjusting its speed")
			break
		}
		if err := (*lamp).ChangeModeSpeed(op.level); err != nil {
			r.err = m.commandFailed(err, lamp)
			break
		}
		m.updateEffectSpeed(op.level, settings)
	case opEffectFrame:
		if err := m.ensureConnected(op.ctx, lamp, startupPending, manuallyDisconnected, true); err != nil {
			r.err = err
			break
		}
		if err := (*lamp).ChangeColorRGB(op.color.R, op.color.G, op.color.B); err != nil {
			r.err = m.commandFailed(err, lamp)
			break
		}
		m.stateMu.Lock()
		m.state.Color = op.color
		m.state.LastCommandAt = time.Now().UTC()
		m.stateMu.Unlock()
		m.publishState()
	}
	m.reply(op, r)
}

func (m *Manager) connect(ctx context.Context, device domain.Device, lamp *Lamp, startupPending *bool, savePreferred, restoreState bool) error {
	if strings.TrimSpace(device.Address) == "" || strings.TrimSpace(device.Name) == "" {
		return errors.New("no preferred LED controller is selected; scan for a device first")
	}
	device.Address = domain.NormalizeAddress(device.Address)
	if *lamp != nil {
		state := m.Snapshot()
		if state.Connected && domain.NormalizeAddress(state.DeviceAddress) == device.Address && state.DeviceName == device.Name && state.LastError == "" {
			return nil
		}
		m.setConnection(false, true, device.Name, device.Address)
		_ = (*lamp).Close()
		*lamp = nil
	} else {
		m.setConnection(false, true, device.Name, device.Address)
	}
	newLamp, resolvedDevice, err := m.transport.Connect(ctx, device)
	if err != nil {
		m.setConnection(false, false, "", "")
		m.setError(err.Error())
		m.log("Bluetooth connection failed: %s", diagnosticBLEError(err))
		return err
	}
	gate := newBLECommandGate(75 * time.Millisecond)
	if provider, ok := m.transport.(interface{ CommandGate() *BLECommandGate }); ok {
		gate = provider.CommandGate()
	}
	newLamp = &pacedLamp{inner: newLamp, gate: gate}
	device = resolvedDevice
	device.Address = domain.NormalizeAddress(device.Address)
	*lamp = newLamp
	m.setConnection(true, false, device.Name, device.Address)
	m.setError("")
	cfg := m.Settings()
	if savePreferred || cfg.PreferredDevice.Address != device.Address || cfg.PreferredDevice.Name != device.Name {
		m.mutateSettings(func(settings *domain.Settings) {
			settings.PreferredDevice.Name = device.Name
			settings.PreferredDevice.Address = device.Address
		})
	}
	if restoreState {
		if err := m.restore(*lamp, startupPending); err != nil {
			m.setConnection(false, false, "", "")
			_ = (*lamp).Close()
			*lamp = nil
			m.setError("Connected, but restoring the saved light state failed: " + err.Error())
			m.log("restore after connect failed: %v", err)
			return err
		}
	} else {
		*startupPending = false
	}
	m.log("connected to %s", device.Name)
	return nil
}

func (m *Manager) restore(lamp Lamp, startupPending *bool) error {
	cfg := m.Settings()
	state := m.Snapshot()
	if *startupPending {
		*startupPending = false
		if !cfg.StartupLightsOn {
			return nil
		}
		color, brightness := m.startupSelection(cfg, state.Color, state.Brightness)
		effectMode := EffectModeStatic
		if cfg.StartupColorMode == "last" && cfg.RestoreLastColor && state.EffectsSupported {
			effectMode = cfg.LastEffectMode
		}
		if err := lamp.ChangeBrightness(percentToController(brightness), 0); err != nil {
			return err
		}
		if err := lamp.ChangeColorRGB(color.R, color.G, color.B); err != nil {
			return err
		}
		if err := lamp.LightOn(true); err != nil {
			return err
		}
		if effectMode != EffectModeStatic {
			if err := lamp.ChangeMode(effectMode); err != nil {
				return err
			}
		}
		m.stateMu.Lock()
		m.state.Color, m.state.Brightness, m.state.Powered, m.state.EffectMode = color, brightness, true, effectMode
		m.stateMu.Unlock()
		m.mutateSettings(func(settings *domain.Settings) {
			settings.LastColor, settings.LastBrightness, settings.LastPowerOn = color, brightness, true
			settings.LastEffectMode = effectMode
		})
		m.publishState()
		return nil
	}
	if cfg.LastPowerOn {
		if err := lamp.ChangeBrightness(percentToController(cfg.LastBrightness), 0); err != nil {
			return err
		}
		if err := lamp.ChangeColorRGB(cfg.LastColor.R, cfg.LastColor.G, cfg.LastColor.B); err != nil {
			return err
		}
	}
	if err := lamp.LightOn(cfg.LastPowerOn); err != nil {
		return err
	}
	effectMode := cfg.LastEffectMode
	if !cfg.LastPowerOn || !state.EffectsSupported {
		effectMode = EffectModeStatic
	}
	if effectMode != EffectModeStatic {
		if err := lamp.ChangeMode(effectMode); err != nil {
			return err
		}
	}
	m.stateMu.Lock()
	m.state.EffectMode, m.state.EffectSpeed = effectMode, cfg.LastEffectSpeed
	m.stateMu.Unlock()
	m.publishState()
	return nil
}

func (m *Manager) startupSelection(settings domain.Settings, current domain.Color, brightness int) (domain.Color, int) {
	if settings.StartupColorMode == "last" {
		if settings.RestoreLastColor {
			return settings.LastColor, settings.LastBrightness
		}
		return domain.Color{R: 255, G: 255, B: 255}, settings.LastBrightness
	}
	for _, p := range m.currentPresets() {
		if (settings.StartupColorMode == "favorite" && p.ID == settings.FavoritePresetID) ||
			(settings.StartupColorMode == "preset" && p.ID == settings.StartupPresetID) {
			return p.Color, p.Brightness
		}
	}
	if settings.StartupColorMode == "preset" {
		return domain.Color{R: 255, G: 255, B: 255}, brightness
	}
	return current, brightness
}

func (m *Manager) ensureConnected(ctx context.Context, lamp *Lamp, startupPending, manuallyDisconnected *bool, restoreState bool) error {
	if *lamp != nil && m.Snapshot().Connected {
		return nil
	}
	*manuallyDisconnected = false
	cfg := m.Settings()
	if err := m.connect(ctx, cfg.PreferredDevice, lamp, startupPending, false, restoreState); err != nil {
		return err
	}
	return nil
}

func (m *Manager) commandFailed(err error, lamp *Lamp) error {
	m.setConnection(false, false, "", "")
	if *lamp != nil {
		_ = (*lamp).Close()
		*lamp = nil
	}
	m.setError("The LED controller stopped responding: " + err.Error())
	m.log("BLE command failed: %s", diagnosticBLEError(err))
	return err
}

func (m *Manager) updateColor(c domain.Color, settings *domain.Settings) {
	m.stateMu.Lock()
	m.state.Color = c
	m.state.EffectMode = EffectModeStatic
	m.state.LastCommandAt = time.Now().UTC()
	m.state.LastError = ""
	m.stateMu.Unlock()
	m.mutateSettings(func(cfg *domain.Settings) {
		cfg.LastColor = c
		cfg.LastEffectMode = EffectModeStatic
	})
	*settings = m.Settings()
	m.publishState()
}

func (m *Manager) updateBrightness(level int, settings *domain.Settings) {
	m.stateMu.Lock()
	m.state.Brightness = level
	m.state.LastCommandAt = time.Now().UTC()
	m.state.LastError = ""
	m.stateMu.Unlock()
	m.mutateSettings(func(cfg *domain.Settings) { cfg.LastBrightness = level })
	*settings = m.Settings()
	m.publishState()
}

func (m *Manager) updatePower(on bool, settings *domain.Settings) {
	m.stateMu.Lock()
	m.state.Powered = on
	m.state.LastCommandAt = time.Now().UTC()
	m.state.LastError = ""
	m.stateMu.Unlock()
	m.mutateSettings(func(cfg *domain.Settings) { cfg.LastPowerOn = on })
	*settings = m.Settings()
	m.publishState()
}

func (m *Manager) updatePreset(color domain.Color, brightness int, settings *domain.Settings) {
	m.stateMu.Lock()
	m.state.Color, m.state.Brightness, m.state.Powered = color, brightness, true
	m.state.EffectMode = EffectModeStatic
	m.state.LastCommandAt = time.Now().UTC()
	m.state.LastError = ""
	m.stateMu.Unlock()
	m.mutateSettings(func(cfg *domain.Settings) {
		cfg.LastColor, cfg.LastBrightness, cfg.LastPowerOn = color, brightness, true
		cfg.LastEffectMode = EffectModeStatic
	})
	*settings = m.Settings()
	m.publishState()
}

func (m *Manager) updateEffectMode(mode int, settings *domain.Settings) {
	m.stateMu.Lock()
	m.state.EffectMode = mode
	if mode != EffectModeStatic {
		m.state.Powered = true
	}
	m.state.LastCommandAt = time.Now().UTC()
	m.state.LastError = ""
	m.stateMu.Unlock()
	m.mutateSettings(func(cfg *domain.Settings) {
		cfg.LastEffectMode = mode
		if mode != EffectModeStatic {
			cfg.LastPowerOn = true
		}
	})
	*settings = m.Settings()
	m.publishState()
}

func (m *Manager) updateEffectSpeed(speed int, settings *domain.Settings) {
	m.stateMu.Lock()
	m.state.EffectSpeed = speed
	m.state.LastCommandAt = time.Now().UTC()
	m.state.LastError = ""
	m.stateMu.Unlock()
	m.mutateSettings(func(cfg *domain.Settings) { cfg.LastEffectSpeed = speed })
	*settings = m.Settings()
	m.publishState()
}

func (m *Manager) mutateSettings(update func(*domain.Settings)) {
	m.cfgMu.Lock()
	update(&m.settings)
	settings := m.settings
	m.cfgMu.Unlock()
	if m.callbacks.Settings != nil {
		m.callbacks.Settings(settings)
	}
}

func (m *Manager) currentPresets() []domain.Preset {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	return append([]domain.Preset(nil), m.presets...)
}

func (m *Manager) setConnection(connected, connecting bool, name, address string) {
	settings := m.Settings()
	m.stateMu.Lock()
	m.state.Connected, m.state.Connecting = connected, connecting
	m.state.DeviceName, m.state.DeviceAddress = name, address
	m.state.EffectsSupported = connected && IsEffectSupported(name)
	m.state.VisualEffectsAvailable = connected && supportedName(name)
	if connected {
		m.state.EffectMode = settings.LastEffectMode
		m.state.EffectSpeed = settings.LastEffectSpeed
	} else {
		m.state.EffectsSupported = false
	}
	if connected {
		active := domain.Device{Name: name, Address: address, Connected: true}
		m.state.ConnectedDevices = []domain.Device{active}
		found := false
		for i := range m.state.Devices {
			if domain.NormalizeAddress(m.state.Devices[i].Address) == domain.NormalizeAddress(address) {
				m.state.Devices[i].Name = name
				m.state.Devices[i].Connected = true
				found = true
			}
		}
		if !found {
			m.state.Devices = append(m.state.Devices, active)
		}
	} else {
		m.state.ConnectedDevices = []domain.Device{}
		for i := range m.state.Devices {
			m.state.Devices[i].Connected = false
		}
	}
	m.stateMu.Unlock()
	m.publishState()
}

func (m *Manager) setScanning(scanning bool, err string) {
	m.stateMu.Lock()
	m.state.Scanning = scanning
	if err != "" {
		m.state.LastError = err
	} else if !scanning {
		m.state.LastError = ""
	}
	m.stateMu.Unlock()
	m.publishState()
}

func (m *Manager) setDevices(devices []domain.Device) {
	m.stateMu.Lock()
	m.state.Devices = append([]domain.Device(nil), devices...)
	m.stateMu.Unlock()
	m.publishState()
}

func (m *Manager) setError(err string) {
	m.stateMu.Lock()
	m.state.LastError = err
	m.stateMu.Unlock()
	m.publishState()
}

func (m *Manager) handleConnectionEvent(device domain.Device, connected bool) {
	select {
	case m.events <- connectionEvent{device: device, connected: connected}:
	case <-m.done:
	default:
		m.log("dropping a Bluetooth connection-status event because the event queue is full")
	}
}

func (m *Manager) applyConnectionEvent(event connectionEvent, retryDelay *time.Duration, nextRetry *time.Time) {
	device, connected := event.device, event.connected
	address := domain.NormalizeAddress(device.Address)
	settings := m.Settings()
	changed := false
	m.stateMu.Lock()
	if address == domain.NormalizeAddress(m.state.DeviceAddress) {
		if connected && !m.state.Connected && !m.state.Connecting {
			// LotusLantern may reconnect internally after a dropped GATT write.
			m.state.Connected = true
			m.state.LastError = ""
			changed = true
			*retryDelay = 2 * time.Second
		} else if !connected && m.state.Connected {
			m.state.Connected = false
			m.state.Connecting = false
			if settings.AutoReconnect {
				m.state.LastError = "Bluetooth link dropped; reconnecting automatically"
			} else {
				m.state.LastError = "Bluetooth link dropped; reconnect to continue"
			}
			changed = true
			*nextRetry = time.Now().Add(*retryDelay)
			*retryDelay = minDuration(*retryDelay*2, 60*time.Second)
		}
	}
	m.stateMu.Unlock()
	if changed {
		if connected {
			m.log("Bluetooth link restored for %s", m.Snapshot().DeviceName)
		} else {
			m.log("Bluetooth link dropped for %s; automatic reconnect is enabled when configured", m.Snapshot().DeviceName)
		}
		m.publishState()
	}
}

func (m *Manager) fail(err error) {
	m.setConnection(false, false, "", "")
	m.setError(err.Error())
	m.log("automatic reconnect failed: %s", diagnosticBLEError(err))
}

func (m *Manager) publishState() {
	if m.callbacks.State != nil {
		m.callbacks.State(m.Snapshot())
	}
}

func (m *Manager) reply(op *operation, result response) {
	if op.reply != nil {
		select {
		case op.reply <- result:
		default:
		}
	}
	for _, ch := range op.replies {
		select {
		case ch <- result:
		default:
		}
	}
}

func (m *Manager) log(format string, args ...any) {
	if m.callbacks.Log != nil {
		m.callbacks.Log(format, args...)
	}
}

func percentToController(percent int) int { return (percent*255 + 50) / 100 }

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
