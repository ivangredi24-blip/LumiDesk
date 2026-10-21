# LumiDesk

LumiDesk is a local Windows desktop controller for Bluetooth LE RGB LED strips
that use the Lotus Lantern / BLEDOM command protocol. The application is built
with Go, Wails, React and TypeScript, and uses the existing
[LotusLantern](https://github.com/Rxflex/LotusLantern) Go library for BLE
connection and controller commands.

## Download and install

This repository contains the source code; generated installers are not checked
in. When a prebuilt version is published, download it from the
[GitHub Releases page](https://github.com/ivangredi24-blip/LumiDesk/releases).
Otherwise, follow **Build from source** below to create
`build/LumiDesk-Setup-x64.exe`.

The installer provides a normal per-user Windows install with a Start Menu
shortcut, optional desktop shortcut, and uninstaller. Administrator privileges
are not required. It checks for Microsoft Edge WebView2 and runs Microsoft's
Evergreen bootstrapper if the runtime is missing. A network connection is
needed only if that prerequisite must be downloaded. Windows 11 includes
WebView2; most Windows 10 installations already have it as well. Builds are
currently unsigned, so Windows may show a SmartScreen warning.

For portable use, run **`build/LumiDesk-Portable-x64.exe`** or extract
**`build/LumiDesk-Portable-x64.zip`**. The portable build still depends on the
Windows WebView2 Runtime; see `README-Portable.txt`. Neither install path
requires Go, Node.js, Git, or a terminal.

Upgrades preserve the per-user settings and presets. Uninstalling LumiDesk also
preserves those files; remove `%APPDATA%\LumiDesk` manually only if you want to
delete your data.

## First launch

1. Turn on the LED controller and make sure Bluetooth is enabled in Windows.
2. Launch LumiDesk from the Start Menu. If no devices have been saved yet, the
   dashboard says **No active devices**; no controller name or address is
   assumed.
3. Choose **Scan / add device**, scan, and connect a compatible strip. Add each
   additional controller the same way. Close the SmartLED Android app first;
   each strip normally accepts one Bluetooth central connection at a time.
4. In **Your devices**, check the connected strips you want the color controls
   to affect. Enable **Sync all devices** to apply controls to every saved
   strip. With sync off, controls target only the checked strip(s).
5. Use the colour wheel, RGB/hex fields, brightness slider and power button.
   The brightness value is sent through the controller's native 0–255
   brightness command; it is not simulated by repeatedly scaling RGB values.
6. Use the main-screen **Quick colors** for RGB primaries and popular shades.
   **Save current** adds the selected custom colour and brightness to the
   searchable preset list below; saved presets can be renamed, edited,
   favourited, reordered, imported, exported, applied, and deleted there.

Closing the window hides it in the notification area by default. The tray menu
can open LumiDesk, turn the strip on or off, apply the favourite preset,
reconnect, open settings, or exit. Exit does not turn the lights off unless
**Turn LEDs off when LumiDesk exits** is enabled.

## Windows automation

In **Automation**, LumiDesk can register a per-user Windows Run entry, start
hidden in the tray, connect to the preferred controller, and restore the last
colour, a favourite, or a selected preset. The startup registration is stored
under the current user's `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
key and does not need elevation.

When **Turn LEDs off on Windows shutdown** is enabled, a hidden native Windows
window listens for `WM_QUERYENDSESSION` and attempts the controller's native
OFF command before Windows ends the session. The attempt is bounded and does
not reconnect during shutdown. This is best effort, not a guarantee: Windows
can terminate a process early, a forced shutdown or power loss may skip the
notification, and BLE writes can fail if the adapter/controller is unavailable.
If shutdown is cancelled, LumiDesk attempts to restore the previous power state.
For power-off that must work through PC power loss, use an appropriate switched
outlet or an electrically rated hardware relay for the LED supply; LumiDesk
does not switch mains power.

## Compatibility and verified capabilities

The project uses the existing LotusLantern TinyGo BLE library. Its source
defines service `0000FFF0-0000-1000-8000-00805F9B34FB`, write characteristic
`0000FFF3-0000-1000-8000-00805F9B34FB`, native power, RGB, and brightness
commands. The controller's advertised address is treated as changeable: if a
saved address is no longer present, LumiDesk makes one fallback scan for the
same advertised device name and saves a newly resolved address.

This controller uses a live Bluetooth LE GATT connection; it does not need to
be bonded through the Windows **Add a device** pairing wizard. The Windows BLE
backend requests `GattSession.MaintainConnection`, listens for connection
status changes, and schedules automatic reconnection if the strip or Windows
Bluetooth stack drops the link while LumiDesk is running. BLEDOM firmware can
still drop a GATT session, and Windows may take several seconds to rediscover
it; this is a reconnecting BLE session, not a permanent OS-level pairing.

LumiDesk provides software-driven **Rainbow** and **Custom crossfade** effects
for selected compatible RGB controllers. They transition by sending RGB color
frames directly; they do not invoke the controller's hardware blink/jump modes
or send an OFF command between colors. Custom crossfade supports 2–8 chosen
colors, with a 1–100 speed control. BLE write pacing is retained to reduce the
risk of dropped GATT links. The transition behavior still needs physical
verification on each controller. Music-reactive control and microphone/audio
capture are not implemented.

The user confirmed that the existing LotusLantern demo had already changed the
physical strip's colours before LumiDesk development. LumiDesk itself has not
been physically verified against the strip in this session. See **Test status**
below for the distinction between automated and hardware tests.

## Local files and diagnostics

- Settings: `%APPDATA%\LumiDesk\settings.json`
- Presets: `%APPDATA%\LumiDesk\presets.json`
- Rotating logs: `%LOCALAPPDATA%\LumiDesk\logs\lumidesk.log`

Writes use temporary files and atomic replacement on Windows. Invalid settings
or preset JSON is preserved with a `.corrupt-<timestamp>` suffix and recovered
to safe defaults. Log files rotate at 5 MB and retain three previous copies.
LumiDesk does not use a cloud service or upload lighting data.

## Build from source

Requirements: Go 1.26.1 or newer, Node.js/npm, Wails CLI v2.17.0, and Inno
Setup 6 to generate the installer. WebView2 is needed to run the desktop UI but
not to compile it. Use PowerShell from this project directory:

```powershell
go test ./...
cd frontend
npm ci
npm run build
cd ..
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
.\scripts\build.ps1
```

`scripts\test.ps1` runs the automated Go tests and frontend production build.
`scripts\build.ps1 -SkipTests` skips the Go test phase. Use `-SkipInstaller` if
only the executable and portable package are needed. The installer build also
expects `installer\dependencies\MicrosoftEdgeWebView2Setup.exe`, the official
Microsoft Evergreen bootstrapper.

## Test status

Automated checks (`go test ./...`) cover:

- RGB/hex validation and brightness scaling without cumulative error.
- Atomic settings and preset persistence, corrupt-file recovery, export/import,
  and collision-safe IDs.
- Preset create/edit/delete/favourite/reorder and settings persistence.
- BLE manager state, serialized command order, pending colour coalescing,
  manual reconnect, exponential reconnect, and shutdown-off queue behavior
  using a mock transport.
- Multi-controller saved-device state, per-device selection, group sync, and
  disconnect status using mock BLE transports.
- Startup registry command formation and registration failure behavior using a
  mock registrar; shutdown cancellation/restoration logic using mock BLE.
- Log rotation.

The application and installer have **not** had a physical-device integration
run as part of this build. Before relying on shutdown automation, please
physically verify the following with your ELK-BLEDOM 66 and observe each result:

The x64 executable and installer compiled successfully. The installer was
installed, upgraded in place, and uninstalled in a unique temporary directory;
the executable, uninstaller registration, and installed files were checked,
including bundled license notices. That smoke test does not verify Bluetooth,
startup-on-login, or shutdown delivery.

1. Scan, connect, and confirm the displayed name/address.
2. Apply red, green, blue, then a custom RGB value to the actual strip.
3. Move brightness from low to high and confirm the strip changes smoothly.
4. Turn the LEDs off and back on; confirm the last colour returns.
5. Save, restart LumiDesk, and apply a preset.
6. Disconnect/reconnect and confirm automatic recovery.
7. Run Rainbow and confirm it transitions through the spectrum without the
   strip going dark. Choose 2–8 custom crossfade colors, start the effect, and
   adjust its speed; confirm the physical strip changes smoothly.
8. Enable Windows startup and startup lights, restart Windows, and confirm the
   controller reconnects after Bluetooth is ready.
9. Enable shutdown-off, perform a normal Windows shutdown, and confirm the
   externally powered strip turns off. Repeat with a restart. Shutdown success
   must be checked on the actual PC; forced power-off is not covered.
10. Install, upgrade, and uninstall the setup executable. Confirm user data is
    preserved and the startup entry is removed when appropriate.
11. If you have more than one compatible strip, connect both, select one and
    apply a color, then enable **Sync all devices** and confirm both change.

Do not treat the app's connection indicator or a successful GATT write as proof
that physical light output changed; verify the strip itself.

## License

LumiDesk source is MIT-licensed. Third-party notices are in
[`LICENSE-NOTICES.md`](LICENSE-NOTICES.md). The Microsoft WebView2 Runtime has
separate Microsoft terms.
