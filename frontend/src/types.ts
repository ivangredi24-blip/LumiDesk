export type Color = { r: number; g: number; b: number }
export type Device = { name: string; address: string; rssi: number; connected: boolean }
export type Preset = {
  id: string
  name: string
  color: Color
  hex: string
  brightness: number
  createdAt: string
  updatedAt: string
  favorite: boolean
}
export type Settings = {
  preferredDevice: Device
  savedDevices: Device[]
  selectedDevices: string[]
  syncDevices: boolean
  lastColor: Color
  lastBrightness: number
  lastPowerOn: boolean
  lastEffectMode: number
  lastEffectSpeed: number
  lastVisualEffect: string
  visualEffectColors: Color[]
  visualEffectSpeed: number
  launchAtLogin: boolean
  startupLightsOn: boolean
  preLoginLights: boolean
  shutdownLightsOff: boolean
  restoreLastColor: boolean
  startupColorMode: 'last' | 'favorite' | 'preset'
  startupPresetId: string
  favoritePresetId: string
  autoReconnect: boolean
  minimizeToTray: boolean
  closeToTray: boolean
  turnOffOnAppExit: boolean
  windowPositionSet: boolean
  windowX: number
  windowY: number
  windowWidth: number
  windowHeight: number
  theme: string
}
export type ControllerState = {
  connected: boolean
  connecting: boolean
  scanning: boolean
  deviceName: string
  deviceAddress: string
  devices: Device[]
  connectedDevices: Device[]
  selectedDevices: string[]
  syncDevices: boolean
  color: Color
  brightness: number
  powered: boolean
  effectsSupported: boolean
  visualEffectsAvailable: boolean
  effectMode: number
  effectSpeed: number
  activeEffect: string
  effectColors: Color[]
  visualEffectSpeed: number
  lastError: string
  lastCommandAt: string
}
export type Snapshot = {
  state: ControllerState
  settings: Settings
  presets: Preset[]
  startupAvailable: boolean
  preLoginAvailable: boolean
  recoveryWarnings: string[]
}

export type AppAPI = {
  GetSnapshot(): Promise<Snapshot>
  StartVisualEffect(kind: 'rainbow' | 'custom-crossfade', colors: Color[], speed: number): Promise<void>
  SetVisualEffectSpeed(speed: number): Promise<void>
  StopVisualEffect(): Promise<void>
  ScanDevices(): Promise<Device[]>
  ConnectDevice(address: string, name: string): Promise<void>
  DisconnectDevice(address: string): Promise<void>
  RemoveDevice(address: string): Promise<void>
  ReconnectDevice(): Promise<void>
  ReconnectController(address: string): Promise<void>
  SetColor(r: number, g: number, b: number): Promise<void>
  SetBrightness(level: number): Promise<void>
  SetPower(on: boolean): Promise<void>
  ApplyColor(color: Color, brightness: number): Promise<void>
  SaveSettings(settings: Settings): Promise<void>
  CreatePreset(name: string, color: Color, brightness: number): Promise<Preset>
  EditPreset(id: string, name: string, color: Color, brightness: number): Promise<void>
  DeletePreset(id: string): Promise<void>
  SetFavoritePreset(id: string): Promise<void>
  MovePreset(id: string, direction: number): Promise<void>
  ApplyPreset(id: string): Promise<void>
  ExportPresets(): Promise<string>
  ImportPresets(raw: string, replace: boolean): Promise<void>
  OpenMainWindow(): void
  HideToTray(): void
  ExitApplication(): void
  NavigateToSettings(): void
  ApplyFavoritePreset(): Promise<void>
}

declare global {
  interface Window {
    go?: { main?: { App?: AppAPI } }
    runtime?: {
      EventsOn: (name: string, callback: (...data: unknown[]) => void) => () => void
      EventsOff: (...names: string[]) => void
    }
  }
}
