import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Activity, ArrowDown, ArrowRight, ArrowUp, Bluetooth, Check,
  ChevronDown, CircleHelp, Clock3, Download, Heart, Info, Lightbulb,
  LoaderCircle, Moon, MoreHorizontal, Palette, Plus, Power, RefreshCw,
  Search, Settings2, Sparkles, Sun, Upload, X, Zap,
} from 'lucide-react'
import { hexOf, hsvToRgb, parseHex, rgbToHsv } from './color'
import type { AppAPI, Color, ControllerState, Device, Preset, Settings, Snapshot } from './types'

type Page = 'studio' | 'automation'
type Toast = { message: string; kind: 'success' | 'error' | 'info' }

function api(): AppAPI {
  const binding = window.go?.main?.App
  if (!binding) throw new Error('LumiDesk backend is not ready. Close and reopen the app.')
  return binding
}

const emptyState: ControllerState = {
  connected: false, connecting: false, scanning: false, deviceName: '', deviceAddress: '',
  devices: [], connectedDevices: [], selectedDevices: [], syncDevices: false,
  color: { r: 168, g: 91, b: 255 }, brightness: 80, powered: false,
  effectsSupported: false, visualEffectsAvailable: false, effectMode: 0, effectSpeed: 91,
  activeEffect: '', effectColors: [], visualEffectSpeed: 50, lastError: '', lastCommandAt: '',
}

const quickColors: Array<{ name: string; color: Color }> = [
  { name: 'Red', color: { r: 255, g: 0, b: 0 } },
  { name: 'Green', color: { r: 0, g: 255, b: 0 } },
  { name: 'Blue', color: { r: 0, g: 0, b: 255 } },
  { name: 'White', color: { r: 255, g: 255, b: 255 } },
  { name: 'Yellow', color: { r: 255, g: 214, b: 0 } },
  { name: 'Cyan', color: { r: 0, g: 224, b: 255 } },
  { name: 'Magenta', color: { r: 255, g: 0, b: 196 } },
]

export default function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [page, setPage] = useState<Page>('studio')
  const [toast, setToast] = useState<Toast | null>(null)
  const [busy, setBusy] = useState(false)
  const [color, setColor] = useState<Color>(emptyState.color)
  const [brightness, setBrightness] = useState(80)
  const [hexDraft, setHexDraft] = useState('#A85BFF')
  const [deviceMenu, setDeviceMenu] = useState(false)
  const [presetDialog, setPresetDialog] = useState<Preset | 'new' | null>(null)
  const [presetSearch, setPresetSearch] = useState('')
  const [visualSpeed, setVisualSpeed] = useState(50)
  const [crossfadeColors, setCrossfadeColors] = useState<Color[]>([
    { r: 255, g: 0, b: 0 }, { r: 0, g: 0, b: 255 },
  ])
  const colorTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const brightnessTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const visualSpeedTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const toastTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const hueMemory = useRef(282)
  const importRef = useRef<HTMLInputElement>(null)

  const showToast = useCallback((message: string, kind: Toast['kind'] = 'info') => {
    setToast({ message, kind })
    if (toastTimer.current) clearTimeout(toastTimer.current)
    toastTimer.current = setTimeout(() => setToast(null), 3600)
  }, [])

  const acceptSnapshot = useCallback((next: Snapshot) => {
    setSnapshot(next)
    setColor(next.state.color)
    setBrightness(next.state.brightness)
    setVisualSpeed(next.state.visualEffectSpeed ?? next.settings.visualEffectSpeed ?? 50)
    if (next.settings.visualEffectColors?.length >= 2) setCrossfadeColors(next.settings.visualEffectColors)
    setHexDraft(hexOf(next.state.color))
  }, [])

  useEffect(() => {
    let offState: (() => void) | undefined
    let offSnapshot: (() => void) | undefined
    let offNavigate: (() => void) | undefined
    let offNotice: (() => void) | undefined
    try {
      offState = window.runtime?.EventsOn('lumidesk:state', (value) => {
        const nextState = value as ControllerState
        setSnapshot((current) => current ? { ...current, state: nextState } : current)
      })
      offSnapshot = window.runtime?.EventsOn('lumidesk:snapshot', (value) => acceptSnapshot(value as Snapshot))
      offNavigate = window.runtime?.EventsOn('lumidesk:navigate', () => setPage('automation'))
      offNotice = window.runtime?.EventsOn('lumidesk:notice', (value) => showToast(String(value), 'info'))
      api().GetSnapshot().then((next) => {
        acceptSnapshot(next)
        if (next.recoveryWarnings?.length) showToast(next.recoveryWarnings.join(' · '), 'info')
      }).catch((error) => showToast(String(error), 'error'))
    } catch (error) {
      showToast(String(error), 'error')
    }
    return () => {
      offState?.(); offSnapshot?.(); offNavigate?.(); offNotice?.()
      if (colorTimer.current) clearTimeout(colorTimer.current)
      if (brightnessTimer.current) clearTimeout(brightnessTimer.current)
      if (visualSpeedTimer.current) clearTimeout(visualSpeedTimer.current)
      if (toastTimer.current) clearTimeout(toastTimer.current)
    }
  }, [acceptSnapshot, showToast])

  const state = snapshot?.state ?? emptyState
  const settings = snapshot?.settings
  const presets = snapshot?.presets ?? []
  const devices = Array.isArray(state.devices) ? state.devices : []
  const connectedDevices = Array.isArray(state.connectedDevices) ? state.connectedDevices : devices.filter((device) => device.connected)
  const currentHSV = useMemo(() => rgbToHsv(color), [color])
  useEffect(() => {
    if (!state.activeEffect && visualSpeedTimer.current) {
      clearTimeout(visualSpeedTimer.current)
      visualSpeedTimer.current = null
    }
  }, [state.activeEffect])
  if (currentHSV.s > 0.02) hueMemory.current = currentHSV.h

  const perform = useCallback(async (action: () => Promise<unknown>, success?: string) => {
    setBusy(true)
    try {
      await action()
      if (success) showToast(success, 'success')
      const next = await api().GetSnapshot()
      acceptSnapshot(next)
      return true
    } catch (error) {
      showToast(friendlyMessage(error), 'error')
      try { acceptSnapshot(await api().GetSnapshot()) } catch { /* keep the last known state */ }
      return false
    } finally {
      setBusy(false)
    }
  }, [acceptSnapshot, showToast])

  const scheduleColor = useCallback((next: Color) => {
    setColor(next)
    setHexDraft(hexOf(next))
    if (colorTimer.current) clearTimeout(colorTimer.current)
    colorTimer.current = setTimeout(() => {
      api().SetColor(next.r, next.g, next.b).catch((error) => showToast(friendlyMessage(error), 'error'))
    }, 110)
  }, [showToast])

  const scheduleBrightness = useCallback((next: number) => {
    setBrightness(next)
    if (brightnessTimer.current) clearTimeout(brightnessTimer.current)
    brightnessTimer.current = setTimeout(() => {
      api().SetBrightness(next).catch((error) => showToast(friendlyMessage(error), 'error'))
    }, 100)
  }, [showToast])

  const scheduleVisualSpeed = useCallback((next: number) => {
    setVisualSpeed(next)
    if (visualSpeedTimer.current) clearTimeout(visualSpeedTimer.current)
    if (state.activeEffect) {
      visualSpeedTimer.current = setTimeout(() => {
        visualSpeedTimer.current = null
        api().SetVisualEffectSpeed(next).catch((error) => showToast(friendlyMessage(error), 'error'))
      }, 120)
    }
  }, [state.activeEffect, showToast])

  const startVisualEffect = (kind: 'rainbow' | 'custom-crossfade') => {
    if (visualSpeedTimer.current) clearTimeout(visualSpeedTimer.current)
    return perform(
      () => api().StartVisualEffect(kind, kind === 'custom-crossfade' ? crossfadeColors : [], visualSpeed),
      kind === 'rainbow' ? 'Smooth rainbow started' : 'Custom crossfade started',
    )
  }

  const stopVisualEffect = () => {
    if (visualSpeedTimer.current) clearTimeout(visualSpeedTimer.current)
    visualSpeedTimer.current = null
    return perform(() => api().StopVisualEffect(), 'Effect stopped')
  }

  const updateCrossfadeColor = (index: number, value: string) => {
    const nextColor = parseHex(value)
    if (!nextColor) return
    setCrossfadeColors((current) => current.map((entry, i) => i === index ? nextColor : entry))
  }

  const applyPreset = (preset: Preset) => perform(() => api().ApplyPreset(preset.id), `${preset.name} applied`)

  const updateSettings = (patch: Partial<Settings>) => {
    if (!settings) return
    const next = { ...settings, ...patch }
    setSnapshot((current) => current ? { ...current, settings: next } : current)
    void perform(() => api().SaveSettings(next), 'Settings saved')
  }

  const scan = async () => {
    setDeviceMenu(true)
    await perform(() => api().ScanDevices(), 'Scan complete')
  }

  const connect = (device: Device) => {
    setDeviceMenu(false)
    perform(() => api().ConnectDevice(device.address, device.name), `Connected to ${device.name}`)
  }

  const selectDevice = (address: string, checked: boolean) => {
    if (!settings) return
    const selected = settings.syncDevices
      ? (settings.savedDevices ?? []).map((device) => device.address)
      : [...(settings.selectedDevices ?? [])]
    const normalized = address.toUpperCase()
    const nextSelection = checked
      ? Array.from(new Set([...selected, normalized]))
      : selected.filter((value) => value.toUpperCase() !== normalized)
    updateSettings({ syncDevices: false, selectedDevices: nextSelection })
  }

  const toggleSync = (enabled: boolean) => {
    if (!settings) return
    updateSettings({
      syncDevices: enabled,
      selectedDevices: enabled ? (settings.savedDevices ?? []).map((device) => device.address) : settings.selectedDevices,
    })
  }

  const disconnectDevice = (device: Device) => perform(
    () => api().DisconnectDevice(device.address),
    `${device.name} disconnected`,
  )

  const removeDevice = (device: Device) => {
    if (!window.confirm(`Remove ${device.name} from LumiDesk?`)) return
    void perform(() => api().RemoveDevice(device.address), `${device.name} removed`)
  }

  const exportPresets = async () => {
    try {
      const data = await api().ExportPresets()
      const blob = new Blob([data], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = 'lumidesk-presets.json'
      anchor.click()
      URL.revokeObjectURL(url)
      showToast('Preset file exported', 'success')
    } catch (error) { showToast(friendlyMessage(error), 'error') }
  }

  const importPresets = async (file?: File) => {
    if (!file) return
    try {
      const raw = await file.text()
      const merge = window.confirm('Merge these presets with your current collection? Choose Cancel to replace the current collection.')
      await api().ImportPresets(raw, !merge)
      acceptSnapshot(await api().GetSnapshot())
      showToast('Presets imported', 'success')
    } catch (error) { showToast(friendlyMessage(error), 'error') }
    finally { if (importRef.current) importRef.current.value = '' }
  }

  const filteredPresets = presets.filter((preset) => preset.name.toLowerCase().includes(presetSearch.toLowerCase()))

  return (
    <div className={`app-shell ${settings?.theme === 'light' ? 'theme-light' : ''}`}>
      <aside className="sidebar">
        <div className="brand-lockup">
          <div className="brand-mark"><span /></div>
          <div><div className="brand-name">lumi<span>desk</span></div><div className="brand-caption">LIGHTING STUDIO</div></div>
        </div>

        <div className="sidebar-section-label">WORKSPACE</div>
        <nav className="main-nav">
          <NavButton icon={<Palette size={17} />} label="Studio" active={page === 'studio'} onClick={() => setPage('studio')} />
          <NavButton icon={<Clock3 size={17} />} label="Automation" active={page === 'automation'} onClick={() => setPage('automation')} />
        </nav>

        <div className="sidebar-spacer" />
        <div className="sidebar-device-card">
          <div className="device-card-top"><span className={`connection-dot ${state.connected ? 'online' : state.connecting ? 'pending' : ''}`} />
            <span>{state.connected ? 'CONNECTED' : state.connecting ? 'CONNECTING' : 'DEVICE'}</span>
            <button className="icon-button tiny" onClick={() => setDeviceMenu((open) => !open)} title="Choose device"><MoreHorizontal size={16} /></button>
          </div>
          <div className="sidebar-device-name">{state.connected ? state.deviceName : state.connecting ? (state.deviceName || 'Connecting…') : 'No active devices'}</div>
          <div className="sidebar-device-foot">{state.connected ? `${connectedDevices.length} Bluetooth LE session${connectedDevices.length === 1 ? '' : 's'} · Ready` : state.connecting ? 'Bluetooth LE · Connecting' : 'Bluetooth LE · Not connected'}</div>
          {!state.connected && <button className="sidebar-connect" onClick={scan}><Search size={14} /> Find controller</button>}
        </div>
        <div className="sidebar-footer"><span className="footer-led" /> Local control · No cloud</div>
      </aside>

      <main className="main-area">
        <header className="topbar">
          <div className="breadcrumb"><span>Workspace</span><ChevronDown size={13} /><strong>{pageTitle(page)}</strong></div>
          <div className="top-actions">
            <button className={`connection-pill ${state.connected ? 'is-connected' : state.connecting ? 'is-pending' : ''}`} onClick={() => setDeviceMenu(true)} title="Scan or choose a Bluetooth controller">
              <Bluetooth size={14} /><span>{state.connected ? 'Connected' : state.connecting ? 'Connecting' : 'Disconnected'}</span>
            </button>
            <button className="icon-button top-settings" title="Automation & settings" onClick={() => setPage('automation')}><Settings2 size={18} /></button>
            <button className="icon-button" title="Minimize to tray" onClick={() => api().HideToTray()}><ArrowDown size={17} /></button>
          </div>
        </header>

        <div className="page-scroll">
          {page === 'studio' && <>
            <section className="welcome-row">
              <div><div className="eyebrow"><span className="eyebrow-line" /> YOUR LIGHT, YOUR MOMENT</div><h1>Lighting <span>studio</span></h1>
                <p>Shape the atmosphere with a colour that feels like yours.</p></div>
              <div className="welcome-device">
                <div className={`device-ring ${state.connected && state.powered ? 'lit' : ''}`} style={{ '--led-color': hexOf(color) } as React.CSSProperties}><Lightbulb size={21} /></div>
                  <div><span className="muted-label">{state.connected ? (connectedDevices.length > 1 ? 'ACTIVE DEVICES' : 'ACTIVE DEVICE') : 'DEVICE STATUS'}</span><strong>{state.connected ? (connectedDevices.length > 1 ? `${connectedDevices.length} LED strips` : connectedDevices[0]?.name || state.deviceName) : state.connecting ? (state.deviceName || 'Connecting…') : 'No active devices'}</strong></div>
                {state.connected
                  ? <button className="icon-button" onClick={() => perform(() => api().ReconnectDevice(), 'Reconnected')} title="Reconnect selected devices"><RefreshCw size={15} /></button>
                  : <button className="icon-button" onClick={() => setDeviceMenu(true)} title="Scan for LED controllers"><Search size={16} /></button>}
              </div>
            </section>

            {state.lastError && <div className="inline-error"><Info size={16} /><span>{state.lastError}</span><button onClick={() => setSnapshot((s) => s ? { ...s, state: { ...s.state, lastError: '' } } : s)}><X size={15} /></button></div>}

            <section className="studio-grid">
              <div className="panel color-panel">
                <div className="panel-heading"><div><span className="section-kicker">COLOR CONTROL</span><h2>Make it yours</h2></div><span className="live-label"><i /> LIVE</span></div>
                <div className="color-editor">
                  <div className="wheel-wrap">
                    <div className="color-wheel" role="slider" aria-label="Hue and saturation colour wheel" aria-valuetext={hexOf(color)}
                      onPointerDown={(event) => { event.currentTarget.setPointerCapture(event.pointerId); pickWheel(event, scheduleColor, hueMemory.current) }}
                      onPointerMove={(event) => { if (event.buttons === 1) pickWheel(event, scheduleColor, hueMemory.current) }}>
                      <div className="wheel-center" />
                      <div className="wheel-marker" style={markerPosition(currentHSV.s > 0.02 ? currentHSV.h : hueMemory.current, currentHSV.s)}><span style={{ background: hexOf(color) }} /></div>
                    </div>
                  </div>
                  <div className="color-details">
                    <div className="color-preview-row"><div className="color-swatch-large" style={{ '--swatch': hexOf(color), '--brightness': `${brightness}%` } as React.CSSProperties}><span /></div>
                      <div><span className="muted-label">SELECTED COLOR</span><strong className="hex-large">{hexOf(color)}</strong><span className="selected-sub">R {color.r} <b>·</b> G {color.g} <b>·</b> B {color.b}</span></div>
                    </div>
                    <div className="field-label-row"><label htmlFor="hex-value">HEX VALUE</label><span className="field-hint">#RRGGBB</span></div>
                    <div className="hex-input-wrap"><span>#</span><input id="hex-value" value={hexDraft.replace(/^#/, '')} maxLength={6} spellCheck={false}
                      onChange={(e) => { const raw = e.target.value.replace(/[^0-9a-f]/gi, '').slice(0, 6); setHexDraft(`#${raw.toUpperCase()}`); const parsed = parseHex(raw); if (parsed) scheduleColor(parsed) }}
                      onBlur={() => { const parsed = parseHex(hexDraft); setHexDraft(hexOf(parsed ?? color)) }} /></div>
                    <div className="rgb-inputs"><RGBField label="R" value={color.r} onChange={(value) => scheduleColor({ ...color, r: value })} />
                      <RGBField label="G" value={color.g} onChange={(value) => scheduleColor({ ...color, g: value })} />
                      <RGBField label="B" value={color.b} onChange={(value) => scheduleColor({ ...color, b: value })} /></div>
                  </div>
                </div>
                <div className="brightness-control"><div className="brightness-label"><div><Sun size={16} /><span>Brightness</span></div><strong>{brightness}<small>%</small></strong></div>
                  <input className="range-input brightness-range" type="range" min="0" max="100" value={brightness} style={{ '--range-progress': `${brightness}%`, '--range-color': hexOf(color) } as React.CSSProperties}
                    onChange={(e) => scheduleBrightness(Number(e.target.value))} aria-label="Brightness" />
                  <div className="range-hints"><span>DIM</span><span>FULL POWER</span></div>
                </div>
                <div className="panel-footer power-footer">
                  <div className="power-info"><span className={`power-indicator ${state.powered ? 'on' : ''}`}><Zap size={15} /></span><div><strong>{state.powered ? 'Lights are on' : 'Lights are off'}</strong><span>{state.powered ? 'Your space is glowing' : 'Ready when you are'}</span></div></div>
                  <button className={`power-button ${state.powered ? 'active' : ''}`} onClick={() => perform(() => api().SetPower(!state.powered), state.powered ? 'Lights turned off' : 'Lights turned on')} disabled={busy}>
                    <Power size={16} /><span>{state.powered ? 'Turn off' : 'Turn on'}</span>
                  </button>
                </div>
              </div>

              <aside className="right-column">
                <div className="panel controller-panel">
                  <div className="controller-panel-icon"><Bluetooth size={18} /></div>
                  <span className="section-kicker">YOUR DEVICES</span>
                  <h2>{connectedDevices.length ? `${connectedDevices.length} active ${connectedDevices.length === 1 ? 'device' : 'devices'}` : 'No active devices'}</h2>
                  {!connectedDevices.length && <p className="controller-address">Scan to add one or more Bluetooth LED controllers.</p>}
                  <div className="fleet-device-list">
                    {devices.map((device) => {
                      const saved = (settings?.savedDevices ?? []).some((item) => item.address.toUpperCase() === device.address.toUpperCase())
                      const selected = settings?.syncDevices || (settings?.selectedDevices ?? []).some((address) => address.toUpperCase() === device.address.toUpperCase())
                      return <div className={`fleet-device-row ${device.connected ? 'connected' : ''}`} key={device.address}>
                        <input type="checkbox" aria-label={`Select ${device.name} for lighting controls`} checked={selected} disabled={!device.connected} onChange={(event) => selectDevice(device.address, event.target.checked)} />
                        <span className={`connection-dot ${device.connected ? 'online' : ''}`} />
                        <div className="fleet-device-name"><strong>{device.name}</strong><span>{device.address}</span></div>
                        {device.connected
                          ? <button className="fleet-action" onClick={() => disconnectDevice(device)}>Disconnect</button>
                          : <button className="fleet-action connect" onClick={() => saved ? perform(() => api().ReconnectController(device.address), `Connected to ${device.name}`) : connect(device)}>{saved ? 'Connect' : 'Add'}</button>}
                        {saved && <button className="icon-button tiny remove-device" title={`Remove ${device.name}`} onClick={() => removeDevice(device)}><X size={14} /></button>}
                      </div>
                    })}
                    {!devices.length && <div className="fleet-empty">No controllers added yet. Scan nearby to add one.</div>}
                  </div>
                  <div className="fleet-controls">
                    <label className="fleet-sync"><span><strong>Sync all devices</strong><small>Apply color and power to every saved strip.</small></span><button className={`toggle ${settings?.syncDevices ? 'on' : ''}`} role="switch" aria-checked={settings?.syncDevices ?? false} onClick={() => toggleSync(!settings?.syncDevices)}><i /></button></label>
                    <button className="button-secondary fleet-scan" onClick={() => setDeviceMenu(true)}><Search size={14} /> Scan / add device</button>
                  </div>
                </div>

                <div className="panel effects-panel visual-effects-panel">
                  <div className="effect-icon"><Sparkles size={17} /></div>
                  <div className="effect-copy"><div className="effect-title">Smooth lighting effects <span className="coming-badge">{state.visualEffectsAvailable ? 'RGB TRANSITIONS' : 'SELECT A DEVICE'}</span></div>
                    <p>{state.visualEffectsAvailable
                      ? 'Changes RGB directly, so the strip stays lit between colors. Hardware blink and jump modes are not used.'
                      : 'Choose a compatible RGB controller to start an effect. You can set up custom colors now.'}</p>
                    <div className="visual-effect-actions">
                      <button className="button-secondary" disabled={busy || !state.visualEffectsAvailable} onClick={() => void startVisualEffect('rainbow')}><Sparkles size={13} /> Rainbow</button>
                      {state.activeEffect && <button className="button-quiet stop-effect" disabled={busy} onClick={() => void stopVisualEffect()}><X size={13} /> Stop</button>}
                    </div>
                    <div className="crossfade-heading"><strong>Custom crossfade</strong><span className="crossfade-hint">Edits apply on start</span><button className="button-quiet add-color" disabled={crossfadeColors.length >= 8} onClick={() => setCrossfadeColors((current) => [...current, { r: 255, g: 255, b: 255 }])}><Plus size={12} /> Add color</button></div>
                    <div className="crossfade-preview" style={{ background: `linear-gradient(90deg, ${crossfadeColors.map((stop) => hexOf(stop)).join(', ')})` }} />
                    <div className="crossfade-colors">{crossfadeColors.map((stop, index) => <div className="crossfade-stop" key={index} title={`Crossfade color ${index + 1}`}>
                      <input type="color" value={hexOf(stop)} aria-label={`Crossfade color ${index + 1}`} onChange={(event) => updateCrossfadeColor(index, event.target.value)} />
                      <span>{hexOf(stop)}</span>
                      <button className="icon-button tiny remove-stop" title={`Remove color ${index + 1}`} disabled={crossfadeColors.length <= 2} onClick={() => setCrossfadeColors((current) => current.filter((_, i) => i !== index))}><X size={12} /></button>
                    </div>)}</div>
                    <button className="button-secondary start-crossfade" disabled={busy || !state.visualEffectsAvailable || crossfadeColors.length < 2} onClick={() => void startVisualEffect('custom-crossfade')}><Sparkles size={13} /> Start custom crossfade</button>
                    <div className="effect-speed-control"><div><span>Transition speed</span><strong>{visualSpeed}</strong></div>
                      <input className="range-input" type="range" min="1" max="100" value={visualSpeed} disabled={busy} style={{ '--range-progress': `${visualSpeed}%` } as React.CSSProperties} onChange={(event) => scheduleVisualSpeed(Number(event.target.value))} aria-label="Visual effect speed" />
                      <div className="range-hints"><span>SLOW</span><span>FAST</span></div>
                    </div>
                  </div>
                </div>
              </aside>
            </section>

            <section className="quick-colors-section">
              <div className="section-title-row"><div><div className="section-kicker">READY WHEN YOU ARE</div><h2>Quick colors</h2></div><span className="quick-colors-note">RGB primaries + popular colors · click to apply</span></div>
              <div className="quick-color-grid">{quickColors.map((item) => <button key={item.name} className="quick-color-card" onClick={() => perform(() => api().ApplyColor(item.color, 100), `${item.name} applied`)} title={`Apply ${item.name} (${hexOf(item.color)})`}>
                <span className="quick-color-dot" style={{ '--quick-color': hexOf(item.color) } as React.CSSProperties} />
                <span className="quick-color-name">{item.name}</span><span className="quick-color-hex">{hexOf(item.color)}</span>
              </button>)}</div>
            </section>

            <section className="presets-section">
              <input ref={importRef} type="file" accept="application/json,.json" hidden onChange={(e) => importPresets(e.target.files?.[0])} />
              <div className="section-title-row presets-main-heading"><div><div className="section-kicker">YOUR COLOR LIBRARY</div><h2>Saved presets <span className="count-bubble">{presets.length}</span></h2></div>
                <div className="preset-toolbar">
                  <div className="search-field"><Search size={14} /><input aria-label="Search saved presets" placeholder="Find a preset" value={presetSearch} onChange={(e) => setPresetSearch(e.target.value)} /></div>
                  <button className="button-secondary" onClick={() => importRef.current?.click()}><Upload size={14} /> Import</button>
                  <button className="button-secondary" onClick={exportPresets}><Download size={14} /> Export</button>
                  <button className="button-primary compact" onClick={() => setPresetDialog('new')}><Plus size={15} /> Save current</button>
                </div>
              </div>
              {filteredPresets.length ? <div className="preset-grid main-preset-grid">{filteredPresets.map((preset) => <PresetCard key={preset.id} preset={preset} onApply={() => applyPreset(preset)} onEdit={() => setPresetDialog(preset)} onFavorite={() => perform(() => api().SetFavoritePreset(preset.favorite ? '' : preset.id), preset.favorite ? 'Favourite removed' : 'Favourite saved')} onDelete={() => deletePreset(preset, showToast, acceptSnapshot)} onMove={(direction) => perform(() => api().MovePreset(preset.id, direction), 'Preset order updated')} />)}</div>
                : <EmptyPresets onAdd={() => setPresetDialog('new')} emptySearch={Boolean(presetSearch)} />}
            </section>
          </>}

          {page === 'automation' && settings && <AutomationPage settings={settings} presets={presets} startupAvailable={snapshot?.startupAvailable ?? false} onChange={updateSettings} showToast={showToast} />}

          <footer className="page-footer"><span>© LumiDesk</span><span><i /> Everything stays on this PC</span><button onClick={() => showToast('Controller: LotusLantern / TinyGo BLE · Local data is stored in your Windows user profile.', 'info')}><CircleHelp size={13} /> About LumiDesk</button></footer>
        </div>
      </main>

      {deviceMenu && <DevicePopover devices={devices} scanning={state.scanning} settings={settings} onScan={scan} onConnect={connect} onClose={() => setDeviceMenu(false)} />}
      {presetDialog && <PresetModal preset={presetDialog === 'new' ? null : presetDialog} currentColor={color} currentBrightness={brightness} onClose={() => setPresetDialog(null)} onSave={async (name, nextColor, nextBrightness) => {
        const saved = presetDialog === 'new'
          ? await perform(() => api().CreatePreset(name, nextColor, nextBrightness), 'Preset created')
          : await perform(() => api().EditPreset(presetDialog.id, name, nextColor, nextBrightness), 'Preset updated')
        if (saved) setPresetDialog(null)
      }} />}
      {toast && <div className={`toast toast-${toast.kind}`}><span className="toast-mark">{toast.kind === 'success' ? <Check size={15} /> : toast.kind === 'error' ? <Info size={15} /> : <Info size={15} />}</span><span>{toast.message}</span><button onClick={() => setToast(null)}><X size={14} /></button></div>}
    </div>
  )
}

function pageTitle(page: Page) { return page === 'studio' ? 'Lighting studio' : 'Automation' }

function friendlyMessage(error: unknown) {
  const message = error instanceof Error ? error.message : String(error)
  return message.replace(/^Error invoking .*?:\s*/i, '').replace(/^Error:\s*/i, '')
}

function markerPosition(hue: number, saturation: number): React.CSSProperties {
  const angle = (hue - 90) * Math.PI / 180
  const radius = 44 * Math.max(0.06, Math.min(1, saturation))
  return { left: `${50 + Math.cos(angle) * radius}%`, top: `${50 + Math.sin(angle) * radius}%` }
}

function pickWheel(event: React.PointerEvent<HTMLDivElement>, change: (color: Color) => void, hueFallback: number) {
  const rect = event.currentTarget.getBoundingClientRect()
  const dx = event.clientX - rect.left - rect.width / 2
  const dy = event.clientY - rect.top - rect.height / 2
  const radius = Math.min(rect.width, rect.height) * 0.46
  const distance = Math.min(radius, Math.hypot(dx, dy))
  let hue = (Math.atan2(dy, dx) * 180 / Math.PI + 90 + 360) % 360
  if (distance < radius * 0.025) hue = hueFallback
  change(hsvToRgb(hue, distance / radius))
}

function NavButton({ icon, label, count, active, onClick }: { icon: React.ReactNode; label: string; count?: number; active: boolean; onClick: () => void }) {
  return <button className={`nav-button ${active ? 'active' : ''}`} onClick={onClick}><span className="nav-icon">{icon}</span><span>{label}</span>{count !== undefined && count > 0 && <span className="nav-count">{count}</span>}</button>
}

function RGBField({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) {
  return <label className="rgb-field"><span>{label}</span><input type="number" min={0} max={255} value={value} onChange={(e) => { const n = Number(e.target.value); if (e.target.value !== '') onChange(Math.max(0, Math.min(255, n))) }} /></label>
}

function PresetCard({ preset, onApply, onEdit, onFavorite, onDelete, onMove, compact = false }: {
  preset: Preset; onApply: () => void; onEdit: () => void; onFavorite: () => void; onDelete: () => void; onMove?: (direction: number) => void; compact?: boolean
}) {
  const [menu, setMenu] = useState(false)
  return <article className={`preset-card ${compact ? 'preset-card-compact' : ''}`}>
    <button className="preset-color" onClick={onApply} title={`Apply ${preset.name}`} style={{ '--preset-color': hexOf(preset.color) } as React.CSSProperties}>
      <span className="preset-color-glow" /><span className="preset-orb" /><span className="apply-overlay"><Zap size={16} /> Apply</span>
    </button>
    <div className="preset-card-body"><button className="preset-name-button" onClick={onApply}><strong>{preset.name}</strong><span>{hexOf(preset.color)} <i /> {preset.brightness}%</span></button>
      <button className={`favorite-button ${preset.favorite ? 'selected' : ''}`} onClick={onFavorite} title={preset.favorite ? 'Remove favourite' : 'Set as favourite'}><Heart size={16} fill={preset.favorite ? 'currentColor' : 'none'} /></button>
      <div className="preset-menu-wrap"><button className="icon-button tiny preset-more" onClick={() => setMenu((v) => !v)} title="Preset options"><MoreHorizontal size={17} /></button>
        {menu && <><button className="menu-dismiss" onClick={() => setMenu(false)} aria-label="Close preset menu" /><div className="preset-menu"><button onClick={() => { setMenu(false); onEdit() }}><Settings2 size={14} /> Edit preset</button>
          {onMove && <><button onClick={() => { setMenu(false); onMove(-1) }}><ArrowUp size={14} /> Move up</button><button onClick={() => { setMenu(false); onMove(1) }}><ArrowDown size={14} /> Move down</button></>}
          <button className="danger" onClick={() => { setMenu(false); onDelete() }}><X size={14} /> Delete</button></div></>}
      </div>
    </div>
  </article>
}

function EmptyPresets({ onAdd, emptySearch = false }: { onAdd: () => void; emptySearch?: boolean }) {
  return <div className="empty-presets"><div className="empty-icon"><Palette size={20} /></div><strong>{emptySearch ? 'No matching presets' : 'Your saved colors will appear here'}</strong><p>{emptySearch ? 'Try a different search.' : 'Choose a quick color above, then save your own named preset.'}</p>{!emptySearch && <button className="button-secondary" onClick={onAdd}><Plus size={15} /> Save current color</button>}</div>
}

function DevicePopover({ devices, scanning, settings, onScan, onConnect, onClose }: { devices: Device[]; scanning: boolean; settings?: Settings; onScan: () => void; onConnect: (device: Device) => void; onClose: () => void }) {
  const scannedDevices = Array.isArray(devices) ? devices : []
  const allDevices = scannedDevices
  return <div className="popover-layer" onPointerDown={(e) => { if (e.target === e.currentTarget) onClose() }}><div className="device-popover">
    <div className="popover-head"><div><strong>Bluetooth controllers</strong><span>Choose a nearby LED controller</span></div><button className="icon-button tiny" onClick={onClose}><X size={16} /></button></div>
    <button className="scan-button" onClick={onScan} disabled={scanning}>{scanning ? <LoaderCircle size={15} className="spin" /> : <Search size={15} />}{scanning ? 'Scanning nearby…' : 'Scan for devices'}</button>
    <div className="device-results">{allDevices.length ? allDevices.map((device) => <button key={device.address} className="device-option" onClick={() => onConnect(device)}>
      <span className="device-option-icon"><Bluetooth size={17} /></span><span className="device-option-copy"><strong>{device.name}</strong><span>{device.address}</span></span><span className="device-rssi">{device.connected ? 'Connected' : device.rssi ? `${device.rssi} dBm` : (settings?.savedDevices ?? []).some((saved) => saved.address.toUpperCase() === device.address.toUpperCase()) ? 'Saved' : ''}</span><ArrowRight size={14} /></button>)
      : <div className="device-empty">No controllers found yet. Turn on the LED strip and scan again.</div>}</div>
    <div className="popover-note"><Info size={13} /> Only ELK / BLEDOM-compatible controllers are listed.</div>
  </div></div>
}

function PresetModal({ preset, currentColor, currentBrightness, onClose, onSave }: { preset: Preset | null; currentColor: Color; currentBrightness: number; onClose: () => void; onSave: (name: string, color: Color, brightness: number) => Promise<void> }) {
  const [name, setName] = useState(preset?.name ?? '')
  const [hex, setHex] = useState(hexOf(preset?.color ?? currentColor))
  const [color, setColor] = useState(preset?.color ?? currentColor)
  const [brightness, setBrightness] = useState(preset?.brightness ?? currentBrightness)
  const [saving, setSaving] = useState(false)
  const save = async () => {
    if (!name.trim()) return
    setSaving(true)
    try { await onSave(name.trim(), color, brightness) } finally { setSaving(false) }
  }
  return <div className="modal-backdrop" onPointerDown={(e) => { if (e.target === e.currentTarget) onClose() }}><div className="preset-modal">
    <div className="modal-top"><div><span className="section-kicker">COLOR LIBRARY</span><h2>{preset ? 'Edit preset' : 'Create a preset'}</h2></div><button className="icon-button" onClick={onClose}><X size={18} /></button></div>
    <p className="modal-description">Give this colour a name you’ll remember. You can apply it instantly from your library.</p>
    <label className="modal-label">PRESET NAME<input autoFocus maxLength={48} placeholder="e.g. After hours" value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') void save() }} /></label>
    <div className="modal-color-row"><label className="modal-label">COLOR<input className="color-picker-input" type="color" value={hex} onChange={(e) => { setHex(e.target.value.toUpperCase()); const c = parseHex(e.target.value); if (c) setColor(c) }} /></label>
      <label className="modal-label hex-modal">HEX<input value={hex} onChange={(e) => { const raw = e.target.value.toUpperCase(); setHex(raw); const c = parseHex(raw); if (c) setColor(c) }} onBlur={() => setHex(hexOf(color))} /></label><div className="modal-swatch" style={{ background: hexOf(color) }} />
    </div>
    <label className="modal-label brightness-modal">BRIGHTNESS <strong>{brightness}%</strong><input className="range-input" type="range" min="0" max="100" value={brightness} onChange={(e) => setBrightness(Number(e.target.value))} /></label>
    <div className="modal-actions"><button className="button-secondary" onClick={onClose}>Cancel</button><button className="button-primary" disabled={!name.trim() || saving} onClick={() => void save()}>{saving && <LoaderCircle size={15} className="spin" />}{preset ? 'Save changes' : 'Save preset'}</button></div>
  </div></div>
}

function AutomationPage({ settings, presets, startupAvailable, onChange, showToast }: { settings: Settings; presets: Preset[]; startupAvailable: boolean; onChange: (patch: Partial<Settings>) => void; showToast: (message: string, kind: Toast['kind']) => void }) {
  return <section className="automation-page">
    <div className="page-intro"><div><div className="eyebrow"><span className="eyebrow-line" /> SET IT AND FORGET IT</div><h1>Always in <span>sync</span></h1><p>Choose what LumiDesk does when your PC starts, sleeps, or shuts down.</p></div><div className="automation-badge"><Activity size={16} /> LOCAL AUTOMATION</div></div>
    <div className="automation-layout">
      <div className="automation-main">
        <section className="panel automation-panel"><div className="automation-heading"><div className="automation-icon purple"><Power size={17} /></div><div><h2>Windows startup</h2><p>Bring your lighting online when you sign in.</p></div></div>
          <SettingRow title="Launch LumiDesk when Windows starts" detail="Starts quietly in the system tray." enabled={settings.launchAtLogin} disabled={!startupAvailable} onToggle={(value) => onChange({ launchAtLogin: value })} />
          <SettingRow title="Turn LEDs on at startup" detail="Reconnect and restore your chosen light state." enabled={settings.startupLightsOn} onToggle={(value) => onChange({ startupLightsOn: value })} />
          <SettingRow title="Restore the last used colour" detail="Use your most recent RGB colour and brightness." enabled={settings.restoreLastColor} onToggle={(value) => onChange({ restoreLastColor: value, startupColorMode: value ? 'last' : settings.startupColorMode === 'last' ? (settings.favoritePresetId ? 'favorite' : 'preset') : settings.startupColorMode })} />
          <div className="setting-subrow"><span>Startup colour</span><select value={settings.startupColorMode} onChange={(e) => onChange({ startupColorMode: e.target.value as Settings['startupColorMode'] })}>
            <option value="last">Last used colour</option><option value="favorite">Favourite preset</option><option value="preset">Choose a preset</option></select></div>
          {settings.startupColorMode === 'preset' && <div className="setting-subrow nested"><span>Selected preset</span><select value={settings.startupPresetId} onChange={(e) => onChange({ startupPresetId: e.target.value })}><option value="">Choose a preset…</option>{presets.map((preset) => <option key={preset.id} value={preset.id}>{preset.name}</option>)}</select></div>}
          {settings.startupColorMode === 'favorite' && !settings.favoritePresetId && <div className="setting-note"><Info size={14} /> Choose a favourite in your preset library to use this option.</div>}
          {settings.startupColorMode === 'preset' && !presets.length && <div className="setting-note"><Info size={14} /> Save a preset before choosing a startup colour.</div>}
          {!startupAvailable && <div className="setting-note"><Info size={14} /> Windows startup registration is only available in the Windows desktop build.</div>}
        </section>

        <section className="panel automation-panel"><div className="automation-heading"><div className="automation-icon cyan"><Moon size={17} /></div><div><h2>Shutdown & reconnect</h2><p>Keep your lights and controller in step with Windows.</p></div></div>
          <SettingRow title="Turn LEDs off on Windows shutdown" detail="Best effort before Bluetooth is stopped. Forced shutdowns may skip it." enabled={settings.shutdownLightsOff} onToggle={(value) => onChange({ shutdownLightsOff: value })} />
          <SettingRow title="Reconnect automatically" detail="Retry the preferred controller with increasing delays." enabled={settings.autoReconnect} onToggle={(value) => onChange({ autoReconnect: value })} />
          <div className="shutdown-caveat"><Info size={15} /><span>Windows gives desktop apps a limited shutdown window. If Bluetooth is unavailable or the PC loses power suddenly, the controller may stay on.</span></div>
        </section>

        <section className="panel automation-panel"><div className="automation-heading"><div className="automation-icon blue"><Settings2 size={17} /></div><div><h2>App behaviour</h2><p>Decide how LumiDesk behaves when you close or minimize it.</p></div></div>
          <SettingRow title="Close to system tray" detail="Keep LumiDesk running when the window is closed." enabled={settings.closeToTray} onToggle={(value) => onChange({ closeToTray: value })} />
          <SettingRow title="Start minimized to tray" detail="Hide the dashboard on Windows sign-in." enabled={settings.minimizeToTray} onToggle={(value) => onChange({ minimizeToTray: value })} />
          <SettingRow title="Turn LEDs off when LumiDesk exits" detail="Separate from Windows shutdown; off by default." enabled={settings.turnOffOnAppExit} onToggle={(value) => onChange({ turnOffOnAppExit: value })} />
          <div className="setting-subrow"><span>Appearance</span><select value={settings.theme} onChange={(e) => onChange({ theme: e.target.value })}><option value="dark">Dark</option><option value="light">Light</option></select></div>
        </section>
      </div>
      <aside className="automation-aside"><div className="panel automation-summary"><div className="summary-orbit"><span /><i /></div><span className="section-kicker">YOUR SETUP</span><h3>Lights that follow your lead.</h3><p>All automations run locally on your PC. Your presets and preferences never leave this device.</p>
        <div className="summary-items"><div><span className={settings.launchAtLogin ? 'summary-check on' : 'summary-check'}>{settings.launchAtLogin && <Check size={11} />}</span><span>Start with Windows</span></div><div><span className={settings.shutdownLightsOff ? 'summary-check on' : 'summary-check'}>{settings.shutdownLightsOff && <Check size={11} />}</span><span>Power off on shutdown</span></div><div><span className={settings.autoReconnect ? 'summary-check on' : 'summary-check'}>{settings.autoReconnect && <Check size={11} />}</span><span>Automatic reconnect</span></div></div>
      </div>
        <div className="panel note-card"><div className="note-icon"><Info size={16} /></div><strong>About shutdown power-off</strong><p>LumiDesk listens for the Windows session-ending notification and sends the controller’s native OFF command while the Bluetooth link is still available. It can’t guarantee delivery during forced shutdown or adapter failures.</p><button className="text-link" onClick={() => showToast('For hardware-backed power-off, use a switched outlet or a relay designed for your LED supply. LumiDesk does not control mains power.', 'info')}>Need guaranteed power off? <ArrowRight size={13} /></button></div>
      </aside>
    </div>
  </section>
}

function SettingRow({ title, detail, enabled, disabled, onToggle }: { title: string; detail: string; enabled: boolean; disabled?: boolean; onToggle: (value: boolean) => void }) {
  return <div className={`setting-row ${disabled ? 'disabled' : ''}`}><div><strong>{title}</strong><span>{detail}</span></div><button className={`toggle ${enabled ? 'on' : ''}`} disabled={disabled} aria-checked={enabled} role="switch" onClick={() => onToggle(!enabled)}><i /></button></div>
}

async function deletePreset(preset: Preset, showToast: (message: string, kind: Toast['kind']) => void, acceptSnapshot: (snapshot: Snapshot) => void) {
  if (!window.confirm(`Delete “${preset.name}”? This can’t be undone.`)) return
  try {
    await api().DeletePreset(preset.id)
    acceptSnapshot(await api().GetSnapshot())
    showToast(`${preset.name} deleted`, 'success')
  } catch (error) { showToast(friendlyMessage(error), 'error') }
}
