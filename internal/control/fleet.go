package control

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"lumidesk/internal/domain"
)

type fleetSession struct {
	device  domain.Device
	manager *Manager
}

type visualEffectRunner struct {
	kind        string
	colors      []domain.Color
	targets     []string
	speed       int
	speedChange chan int
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	started     time.Time
}

// FleetManager coordinates one serialized BLE manager per saved controller.
// The shared LotusTransport gate prevents scans, connections, and GATT writes
// for different strips from racing the process-wide TinyGo adapter.
type FleetManager struct {
	transport Transport
	callbacks Callbacks

	mu       sync.RWMutex
	settings domain.Settings
	presets  []domain.Preset
	sessions map[string]*fleetSession
	scanned  map[string]domain.Device
	scanning bool
	visual   *visualEffectRunner

	stateMu sync.RWMutex
	state   State

	ctx    context.Context
	cancel context.CancelFunc
}

type VisualEffectController interface {
	StartVisualEffect(context.Context, string, []domain.Color, int) error
	SetVisualEffectSpeed(int) error
	StopVisualEffect() error
}

func NewFleetManager(transport Transport, settings domain.Settings, presets []domain.Preset, callbacks Callbacks) *FleetManager {
	if transport == nil {
		transport = &LotusTransport{}
	}
	settings.NormalizeDevices()
	return &FleetManager{
		transport: transport, callbacks: callbacks, settings: settings,
		presets: append([]domain.Preset(nil), presets...), sessions: make(map[string]*fleetSession),
		scanned: make(map[string]domain.Device),
		state:   State{Devices: []domain.Device{}, ConnectedDevices: []domain.Device{}, SelectedDevices: []string{}, Color: settings.LastColor, Brightness: settings.LastBrightness, Powered: settings.LastPowerOn, SyncDevices: settings.SyncDevices},
	}
}

func (f *FleetManager) Start(parent context.Context) {
	f.mu.Lock()
	f.ctx, f.cancel = context.WithCancel(parent)
	settings := cloneSettings(f.settings)
	f.mu.Unlock()
	for _, device := range settings.SavedDevices {
		f.ensureSession(device)
	}
	f.refresh()
}

func (f *FleetManager) Close(ctx context.Context) error {
	f.mu.RLock()
	cancel := f.cancel
	f.mu.RUnlock()
	if cancel == nil {
		return nil
	}
	f.stopVisualEffect(true)
	cancel()
	f.mu.RLock()
	sessions := make([]*fleetSession, 0, len(f.sessions))
	for _, session := range f.sessions {
		sessions = append(sessions, session)
	}
	f.mu.RUnlock()
	var result error
	for _, session := range sessions {
		if err := session.manager.Close(ctx); err != nil && result == nil {
			result = err
		}
	}
	return result
}

func (f *FleetManager) Snapshot() State {
	f.stateMu.RLock()
	defer f.stateMu.RUnlock()
	state := f.state
	state.Devices = append([]domain.Device{}, state.Devices...)
	state.ConnectedDevices = append([]domain.Device{}, state.ConnectedDevices...)
	state.SelectedDevices = append([]string{}, state.SelectedDevices...)
	state.EffectColors = append([]domain.Color{}, state.EffectColors...)
	return state
}

func (f *FleetManager) Settings() domain.Settings {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return cloneSettings(f.settings)
}

func (f *FleetManager) SetSettings(settings domain.Settings, presets []domain.Preset) {
	settings.NormalizeDevices()
	f.mu.Lock()
	selectionChanged := f.settings.SyncDevices != settings.SyncDevices || !sameAddresses(f.settings.SelectedDevices, settings.SelectedDevices)
	f.settings = cloneSettings(settings)
	f.presets = append([]domain.Preset(nil), presets...)
	sessions := make([]*fleetSession, 0, len(f.sessions))
	for _, session := range f.sessions {
		sessions = append(sessions, session)
	}
	f.mu.Unlock()
	if selectionChanged {
		f.stopVisualEffect(true)
	}
	for _, session := range sessions {
		session.manager.SetSettings(f.childSettings(session.device), presets)
	}
	for _, device := range settings.SavedDevices {
		f.ensureSession(device)
	}
	f.refresh()
}

func (f *FleetManager) Scan(ctx context.Context) ([]domain.Device, error) {
	f.mu.Lock()
	f.scanning = true
	f.mu.Unlock()
	f.refresh()
	devices, err := f.transport.Scan(ctx, 8*time.Second)
	f.mu.Lock()
	f.scanning = false
	if err == nil {
		f.scanned = make(map[string]domain.Device, len(devices))
		for _, device := range devices {
			device.Address = domain.NormalizeAddress(device.Address)
			f.scanned[device.Address] = device
		}
	}
	f.mu.Unlock()
	f.refresh()
	return devices, err
}

func (f *FleetManager) Connect(ctx context.Context, device domain.Device) error {
	device.Address = domain.NormalizeAddress(device.Address)
	if device.Address == "" || device.Name == "" {
		return errors.New("select a Bluetooth LED controller first")
	}
	f.rememberDevice(device)
	session := f.ensureSession(device)
	if session == nil {
		return errors.New("Bluetooth manager is not running")
	}
	err := session.manager.Connect(ctx, device)
	f.refresh()
	return err
}

func (f *FleetManager) ensureSession(device domain.Device) *fleetSession {
	device.Address = domain.NormalizeAddress(device.Address)
	if device.Address == "" || device.Name == "" {
		return nil
	}
	f.mu.Lock()
	if existing := f.sessions[device.Address]; existing != nil {
		f.mu.Unlock()
		return existing
	}
	if f.ctx == nil {
		f.mu.Unlock()
		return nil
	}
	session := &fleetSession{device: device}
	settings := f.childSettingsLocked(device)
	presets := append([]domain.Preset(nil), f.presets...)
	session.manager = NewManager(f.transport, settings, presets, Callbacks{
		State:    func(State) { f.refresh() },
		Settings: func(child domain.Settings) { f.onChildSettings(session, child) },
		Log:      f.callbacks.Log,
	})
	f.sessions[device.Address] = session
	ctx := f.ctx
	f.mu.Unlock()
	session.manager.Start(ctx)
	return session
}

func (f *FleetManager) rememberDevice(device domain.Device) {
	device.Address = domain.NormalizeAddress(device.Address)
	f.mu.Lock()
	settings := cloneSettings(f.settings)
	found := false
	for i := range settings.SavedDevices {
		if domain.NormalizeAddress(settings.SavedDevices[i].Address) == device.Address {
			settings.SavedDevices[i] = device
			found = true
			break
		}
	}
	if !found {
		if len(settings.SavedDevices) == 0 && len(settings.SelectedDevices) == 0 && !settings.SyncDevices {
			settings.SelectedDevices = append(settings.SelectedDevices, device.Address)
		}
		settings.SavedDevices = append(settings.SavedDevices, device)
	}
	settings.PreferredDevice = device
	settings.NormalizeDevices()
	f.settings = settings
	f.mu.Unlock()
	f.notifySettings(settings)
	f.ensureSession(device)
	f.refresh()
}

func (f *FleetManager) Disconnect(ctx context.Context) error {
	f.stopVisualEffect(true)
	f.mu.RLock()
	sessions := make([]*fleetSession, 0, len(f.sessions))
	for _, session := range f.sessions {
		sessions = append(sessions, session)
	}
	f.mu.RUnlock()
	var result error
	for _, session := range sessions {
		if err := session.manager.Disconnect(ctx); err != nil && result == nil {
			result = fmt.Errorf("disconnect %s: %w", session.device.Name, err)
		}
	}
	f.refresh()
	return result
}

func (f *FleetManager) DisconnectDevice(ctx context.Context, address string) error {
	f.stopVisualEffect(true)
	session := f.session(address)
	if session == nil {
		return nil
	}
	err := session.manager.Disconnect(ctx)
	f.refresh()
	return err
}

func (f *FleetManager) RemoveDevice(ctx context.Context, address string) error {
	f.stopVisualEffect(true)
	address = domain.NormalizeAddress(address)
	f.mu.Lock()
	session := f.sessions[address]
	delete(f.sessions, address)
	delete(f.scanned, address)
	f.mu.Unlock()
	if session != nil {
		if err := session.manager.Close(ctx); err != nil {
			return err
		}
	}
	f.refresh()
	return nil
}

func (f *FleetManager) Reconnect(ctx context.Context) error {
	addresses, err := f.targetAddresses(false)
	if err != nil {
		return err
	}
	return f.reconnectAddresses(ctx, addresses)
}

func (f *FleetManager) ReconnectDevice(ctx context.Context, address string) error {
	session := f.session(address)
	if session == nil {
		return errors.New("controller is not in the saved device list")
	}
	return session.manager.Reconnect(ctx)
}

func (f *FleetManager) SetColor(ctx context.Context, color domain.Color) error {
	if err := color.Validate(); err != nil {
		return err
	}
	f.stopVisualEffect(true)
	addresses, err := f.targetAddresses(false)
	if err != nil {
		return err
	}
	return f.forTargets(ctx, addresses, func(manager *Manager) error { return manager.SetColor(ctx, color) })
}

func (f *FleetManager) SetSelectedDevices(addresses []string) error {
	f.mu.Lock()
	settings := cloneSettings(f.settings)
	settings.SelectedDevices = append([]string{}, addresses...)
	settings.NormalizeDevices()
	if err := settings.Validate(); err != nil {
		f.mu.Unlock()
		return err
	}
	f.settings = settings
	presets := append([]domain.Preset(nil), f.presets...)
	sessions := make([]*fleetSession, 0, len(f.sessions))
	for _, session := range f.sessions {
		sessions = append(sessions, session)
	}
	f.mu.Unlock()
	for _, session := range sessions {
		session.manager.SetSettings(f.childSettings(session.device), presets)
	}
	f.notifySettings(settings)
	f.refresh()
	return nil
}

func (f *FleetManager) SetBrightness(ctx context.Context, brightness int) error {
	if brightness < 0 || brightness > 100 {
		return errors.New("brightness must be between 0 and 100")
	}
	addresses, err := f.targetAddresses(false)
	if err != nil {
		return err
	}
	return f.forTargets(ctx, addresses, func(manager *Manager) error { return manager.SetBrightness(ctx, brightness) })
}

func (f *FleetManager) SetPower(ctx context.Context, on bool) error {
	if !on {
		f.stopVisualEffect(true)
	}
	addresses, err := f.targetAddresses(false)
	if err != nil {
		return err
	}
	return f.forTargets(ctx, addresses, func(manager *Manager) error { return manager.SetPower(ctx, on) })
}

func (f *FleetManager) ApplyPreset(ctx context.Context, color domain.Color, brightness int) error {
	if err := color.Validate(); err != nil {
		return err
	}
	if brightness < 0 || brightness > 100 {
		return errors.New("brightness must be between 0 and 100")
	}
	f.stopVisualEffect(true)
	addresses, err := f.targetAddresses(false)
	if err != nil {
		return err
	}
	return f.forTargets(ctx, addresses, func(manager *Manager) error { return manager.ApplyPreset(ctx, color, brightness) })
}

func (f *FleetManager) SetEffectMode(ctx context.Context, mode int) error {
	f.stopVisualEffect(true)
	if !validEffectMode(mode) {
		return errors.New("select a supported ELK-BLEDOB effect")
	}
	addresses, err := f.targetAddresses(false)
	if err != nil {
		return err
	}
	if mode != EffectModeStatic {
		for _, address := range addresses {
			session := f.session(address)
			if session != nil && !IsEffectSupported(session.device.Name) {
				return fmt.Errorf("%s does not have a verified effect mapping", session.device.Name)
			}
		}
	}
	return f.forTargets(ctx, addresses, func(manager *Manager) error { return manager.SetEffectMode(ctx, mode) })
}

func (f *FleetManager) SetEffectSpeed(ctx context.Context, speed int) error {
	if speed < EffectSpeedMin || speed > EffectSpeedMax {
		return errors.New("effect speed must be between 0 and 183")
	}
	addresses, err := f.targetAddresses(false)
	if err != nil {
		return err
	}
	for _, address := range addresses {
		session := f.session(address)
		if session != nil && !IsEffectSupported(session.device.Name) {
			return fmt.Errorf("%s does not have a verified effect mapping", session.device.Name)
		}
	}
	return f.forTargets(ctx, addresses, func(manager *Manager) error { return manager.SetEffectSpeed(ctx, speed) })
}

func (f *FleetManager) StartVisualEffect(ctx context.Context, kind string, colors []domain.Color, speed int) error {
	colors = append([]domain.Color(nil), colors...)
	if err := validVisualEffect(kind, colors, speed); err != nil {
		return err
	}
	f.mu.RLock()
	parent := f.ctx
	f.mu.RUnlock()
	if parent == nil {
		return errors.New("LumiDesk lighting manager is not running")
	}
	addresses, err := f.targetAddresses(false)
	if err != nil {
		return err
	}
	f.stopVisualEffect(false)
	for _, address := range addresses {
		session := f.session(address)
		if session == nil {
			return fmt.Errorf("saved controller %s is unavailable", address)
		}
		state := session.manager.Snapshot()
		if !supportedName(session.device.Name) {
			return fmt.Errorf("%s is not a supported RGB LED controller", session.device.Name)
		}
		if !state.Connected || !state.Powered {
			if err := session.manager.SetPower(ctx, true); err != nil {
				return fmt.Errorf("turn on %s before starting the effect: %w", session.device.Name, err)
			}
		}
	}
	effectCtx, cancel := context.WithCancel(parent)
	runner := &visualEffectRunner{
		kind: kind, colors: colors, targets: append([]string(nil), addresses...), speed: speed,
		speedChange: make(chan int, 1), ctx: effectCtx, cancel: cancel, done: make(chan struct{}), started: time.Now(),
	}
	f.mu.Lock()
	f.visual = runner
	settings := cloneSettings(f.settings)
	settings.LastVisualEffect = kind
	if kind == "custom-crossfade" {
		settings.VisualEffectColors = append([]domain.Color{}, colors...)
	}
	settings.VisualEffectSpeed = speed
	settings.LastEffectMode = 0
	f.settings = settings
	sessions := make([]*fleetSession, 0, len(f.sessions))
	for _, session := range f.sessions {
		sessions = append(sessions, session)
	}
	presets := append([]domain.Preset(nil), f.presets...)
	f.mu.Unlock()
	for _, session := range sessions {
		session.manager.SetSettings(f.childSettings(session.device), presets)
	}
	f.notifySettings(settings)
	f.refresh()
	go f.runVisualEffect(runner)
	return nil
}

func (f *FleetManager) SetVisualEffectSpeed(speed int) error {
	if speed < 1 || speed > 100 {
		return errors.New("visual effect speed must be between 1 and 100")
	}
	f.mu.Lock()
	runner := f.visual
	if runner == nil {
		f.mu.Unlock()
		return errors.New("start Rainbow or Custom crossfade before changing its speed")
	}
	settings := cloneSettings(f.settings)
	settings.VisualEffectSpeed = speed
	f.settings = settings
	sessions := make([]*fleetSession, 0, len(f.sessions))
	for _, session := range f.sessions {
		sessions = append(sessions, session)
	}
	presets := append([]domain.Preset(nil), f.presets...)
	f.mu.Unlock()
	select {
	case runner.speedChange <- speed:
	default:
		select {
		case <-runner.speedChange:
		default:
		}
		runner.speedChange <- speed
	}
	for _, session := range sessions {
		session.manager.SetSettings(f.childSettings(session.device), presets)
	}
	f.notifySettings(settings)
	f.refresh()
	return nil
}

func (f *FleetManager) StopVisualEffect() error {
	f.stopVisualEffect(true)
	return nil
}

func (f *FleetManager) stopVisualEffect(clearSaved bool) {
	f.mu.Lock()
	runner := f.visual
	if runner == nil && !clearSaved {
		f.mu.Unlock()
		return
	}
	f.visual = nil
	settings := cloneSettings(f.settings)
	if clearSaved {
		settings.LastVisualEffect = ""
		f.settings = settings
	}
	sessions := make([]*fleetSession, 0, len(f.sessions))
	for _, session := range f.sessions {
		sessions = append(sessions, session)
	}
	presets := append([]domain.Preset(nil), f.presets...)
	f.mu.Unlock()
	if runner != nil {
		runner.cancel()
		select {
		case <-runner.done:
		case <-time.After(1500 * time.Millisecond):
		}
	}
	if clearSaved {
		for _, session := range sessions {
			session.manager.SetSettings(f.childSettings(session.device), presets)
		}
		f.notifySettings(settings)
	}
	f.refresh()
}

func (f *FleetManager) runVisualEffect(runner *visualEffectRunner) {
	defer close(runner.done)
	timer := time.NewTimer(0)
	defer timer.Stop()
	speed := runner.speed
	for {
		select {
		case <-runner.ctx.Done():
			return
		case speed = <-runner.speedChange:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(visualFrameInterval(speed))
		case <-timer.C:
			color := visualFrame(runner.kind, runner.colors, speed, time.Since(runner.started))
			for _, address := range runner.targets {
				if runner.ctx.Err() != nil {
					return
				}
				session := f.session(address)
				if session == nil || !session.manager.Snapshot().Connected {
					continue
				}
				if err := session.manager.SetVisualColorFrame(runner.ctx, color); err != nil && runner.ctx.Err() == nil {
					f.log("%s effect color update failed: %s", session.device.Name, diagnosticBLEError(err))
				}
			}
			timer.Reset(visualFrameInterval(speed))
		}
	}
}

func (f *FleetManager) PowerOffForShutdown(ctx context.Context) error {
	f.stopVisualEffect(true)
	f.mu.RLock()
	sessions := make([]*fleetSession, 0, len(f.sessions))
	for _, session := range f.sessions {
		if session.manager.Snapshot().Connected {
			sessions = append(sessions, session)
		}
	}
	f.mu.RUnlock()
	if len(sessions) == 0 {
		return errors.New("no LED controllers are connected; shutdown OFF was not sent")
	}
	var result error
	for _, session := range sessions {
		if err := session.manager.PowerOffForShutdown(ctx); err != nil && result == nil {
			result = fmt.Errorf("turn off %s: %w", session.device.Name, err)
		}
	}
	f.refresh()
	return result
}

func (f *FleetManager) childSettings(device domain.Device) domain.Settings {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.childSettingsLocked(device)
}

// Caller must hold f.mu or f.mu.RLock.
func (f *FleetManager) childSettingsLocked(device domain.Device) domain.Settings {
	settings := cloneSettings(f.settings)
	settings.PreferredDevice = device
	return settings
}

func (f *FleetManager) onChildSettings(session *fleetSession, child domain.Settings) {
	f.mu.Lock()
	settings := cloneSettings(f.settings)
	settings.LastColor = child.LastColor
	settings.LastBrightness = child.LastBrightness
	settings.LastPowerOn = child.LastPowerOn
	settings.LastEffectMode = child.LastEffectMode
	settings.LastEffectSpeed = child.LastEffectSpeed
	resolved := child.PreferredDevice
	resolved.Address = domain.NormalizeAddress(resolved.Address)
	if resolved.Address != "" && resolved.Name != "" {
		oldAddress := domain.NormalizeAddress(session.device.Address)
		if resolved.Address != oldAddress {
			delete(f.sessions, oldAddress)
			delete(f.scanned, oldAddress)
			f.scanned[resolved.Address] = resolved
			for i := range settings.SavedDevices {
				if domain.NormalizeAddress(settings.SavedDevices[i].Address) == oldAddress {
					settings.SavedDevices[i] = resolved
				}
			}
			for i, selected := range settings.SelectedDevices {
				if domain.NormalizeAddress(selected) == oldAddress {
					settings.SelectedDevices[i] = resolved.Address
				}
			}
			session.device = resolved
			f.sessions[resolved.Address] = session
		}
		settings.PreferredDevice = resolved
	}
	settings.NormalizeDevices()
	f.settings = settings
	presets := append([]domain.Preset(nil), f.presets...)
	sessions := make([]*fleetSession, 0, len(f.sessions))
	for _, current := range f.sessions {
		sessions = append(sessions, current)
	}
	f.mu.Unlock()
	for _, current := range sessions {
		current.manager.SetSettings(f.childSettings(current.device), presets)
	}
	f.notifySettings(settings)
	f.refresh()
}

func (f *FleetManager) notifySettings(settings domain.Settings) {
	if f.callbacks.Settings != nil {
		f.callbacks.Settings(cloneSettings(settings))
	}
}

func (f *FleetManager) session(address string) *fleetSession {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.sessions[domain.NormalizeAddress(address)]
}

func (f *FleetManager) targetAddresses(includeAll bool) ([]string, error) {
	settings := f.Settings()
	var addresses []string
	if includeAll || settings.SyncDevices {
		for _, device := range settings.SavedDevices {
			addresses = append(addresses, domain.NormalizeAddress(device.Address))
		}
	} else {
		addresses = append(addresses, settings.SelectedDevices...)
	}
	if len(addresses) == 0 {
		return nil, errors.New("select one or more controllers before changing lights")
	}
	return addresses, nil
}

func (f *FleetManager) forTargets(ctx context.Context, addresses []string, command func(*Manager) error) error {
	for _, address := range addresses {
		session := f.session(address)
		if session == nil {
			continue
		}
		if err := command(session.manager); err != nil {
			return fmt.Errorf("%s: %w", session.device.Name, err)
		}
	}
	f.refresh()
	return nil
}

func (f *FleetManager) reconnectAddresses(ctx context.Context, addresses []string) error {
	return f.forTargets(ctx, addresses, func(manager *Manager) error { return manager.Reconnect(ctx) })
}

func (f *FleetManager) refresh() {
	f.mu.RLock()
	settings := cloneSettings(f.settings)
	scanned := make(map[string]domain.Device, len(f.scanned))
	for address, device := range f.scanned {
		scanned[address] = device
	}
	scanning := f.scanning
	sessions := make([]*fleetSession, 0, len(f.sessions))
	for _, session := range f.sessions {
		sessions = append(sessions, session)
	}
	f.mu.RUnlock()

	devicesByAddress := make(map[string]domain.Device, len(settings.SavedDevices)+len(scanned))
	order := make([]string, 0, len(settings.SavedDevices)+len(scanned))
	for _, device := range settings.SavedDevices {
		device.Address = domain.NormalizeAddress(device.Address)
		device.Connected = false
		devicesByAddress[device.Address] = device
		order = append(order, device.Address)
	}
	for address, device := range scanned {
		if _, exists := devicesByAddress[address]; !exists {
			device.Connected = false
			devicesByAddress[address] = device
			order = append(order, address)
		}
	}
	childStates := make(map[string]State, len(sessions))
	deviceErrors := make(map[string]string)
	connecting := false
	for _, session := range sessions {
		child := session.manager.Snapshot()
		address := domain.NormalizeAddress(child.DeviceAddress)
		if address == "" {
			address = domain.NormalizeAddress(session.device.Address)
		}
		childStates[address] = child
		connecting = connecting || child.Connecting
		if child.LastError != "" {
			deviceName := session.device.Name
			if child.DeviceName != "" {
				deviceName = child.DeviceName
			}
			deviceErrors[address] = fmt.Sprintf("%s: %s", deviceName, child.LastError)
		}
		if device, ok := devicesByAddress[address]; ok {
			if child.DeviceName != "" {
				device.Name = child.DeviceName
			}
			device.Connected = child.Connected
			devicesByAddress[address] = device
		}
	}
	devices := make([]domain.Device, 0, len(devicesByAddress))
	for _, address := range order {
		if device, ok := devicesByAddress[address]; ok {
			devices = append(devices, device)
		}
	}
	for address, device := range devicesByAddress {
		if !containsAddress(order, address) {
			devices = append(devices, device)
		}
	}
	connected := make([]domain.Device, 0)
	for _, device := range devices {
		if device.Connected {
			connected = append(connected, device)
		}
	}
	selected := make([]string, 0, len(settings.SelectedDevices))
	for _, address := range settings.SelectedDevices {
		selected = append(selected, domain.NormalizeAddress(address))
	}
	var lastError string
	for _, address := range selected {
		if deviceErrors[address] != "" {
			lastError = deviceErrors[address]
			break
		}
	}
	if lastError == "" {
		for _, device := range devices {
			if deviceErrors[domain.NormalizeAddress(device.Address)] != "" {
				lastError = deviceErrors[domain.NormalizeAddress(device.Address)]
				break
			}
		}
	}
	selectedForEffects := selected
	if settings.SyncDevices {
		selectedForEffects = make([]string, 0, len(settings.SavedDevices))
		for _, device := range settings.SavedDevices {
			selectedForEffects = append(selectedForEffects, domain.NormalizeAddress(device.Address))
		}
	}
	effectsSupported := len(selectedForEffects) > 0
	for _, address := range selectedForEffects {
		device, found := devicesByAddress[address]
		if !found || !IsEffectSupported(device.Name) {
			effectsSupported = false
			break
		}
	}
	selectedForVisuals := selected
	if settings.SyncDevices {
		selectedForVisuals = make([]string, 0, len(settings.SavedDevices))
		for _, device := range settings.SavedDevices {
			selectedForVisuals = append(selectedForVisuals, domain.NormalizeAddress(device.Address))
		}
	}
	visualEffectsAvailable := len(selectedForVisuals) > 0
	for _, address := range selectedForVisuals {
		device, found := devicesByAddress[address]
		if !found || !supportedName(device.Name) {
			visualEffectsAvailable = false
			break
		}
	}
	primary := domain.Device{}
	for _, address := range selected {
		for _, device := range connected {
			if device.Address == address {
				primary = device
				break
			}
		}
		if primary.Address != "" {
			break
		}
	}
	if primary.Address == "" && len(connected) > 0 {
		primary = connected[0]
	}
	state := State{
		Connected: len(connected) > 0, Connecting: connecting, Scanning: scanning,
		DeviceName: primary.Name, DeviceAddress: primary.Address,
		Devices: devices, ConnectedDevices: connected,
		SelectedDevices: selected, SyncDevices: settings.SyncDevices,
		Color: settings.LastColor, Brightness: settings.LastBrightness, Powered: settings.LastPowerOn,
		EffectsSupported: effectsSupported, VisualEffectsAvailable: visualEffectsAvailable,
		EffectMode: settings.LastEffectMode, EffectSpeed: settings.LastEffectSpeed,
		VisualEffectSpeed: settings.VisualEffectSpeed, EffectColors: append([]domain.Color{}, settings.VisualEffectColors...), LastError: lastError,
	}
	f.mu.RLock()
	if f.visual != nil {
		state.ActiveEffect = f.visual.kind
		state.EffectColors = append([]domain.Color{}, f.visual.colors...)
	}
	f.mu.RUnlock()
	if primary.Address != "" {
		if child, ok := childStates[primary.Address]; ok {
			state.Color, state.Brightness, state.Powered = child.Color, child.Brightness, child.Powered
			state.EffectMode, state.EffectSpeed = child.EffectMode, child.EffectSpeed
			state.LastCommandAt = child.LastCommandAt
		}
	}
	f.stateMu.Lock()
	f.state = state
	f.stateMu.Unlock()
	if f.callbacks.State != nil {
		f.callbacks.State(state)
	}
}

func containsAddress(addresses []string, wanted string) bool {
	for _, address := range addresses {
		if domain.NormalizeAddress(address) == domain.NormalizeAddress(wanted) {
			return true
		}
	}
	return false
}

func sameAddresses(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	addresses := make(map[string]int, len(first))
	for _, address := range first {
		addresses[domain.NormalizeAddress(address)]++
	}
	for _, address := range second {
		normalized := domain.NormalizeAddress(address)
		if addresses[normalized] == 0 {
			return false
		}
		addresses[normalized]--
	}
	return true
}

func (f *FleetManager) log(format string, args ...any) {
	if f.callbacks.Log != nil {
		f.callbacks.Log(format, args...)
	}
}

func cloneSettings(settings domain.Settings) domain.Settings {
	settings.SavedDevices = append([]domain.Device{}, settings.SavedDevices...)
	settings.SelectedDevices = append([]string{}, settings.SelectedDevices...)
	settings.VisualEffectColors = append([]domain.Color{}, settings.VisualEffectColors...)
	return settings
}
