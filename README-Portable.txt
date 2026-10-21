LumiDesk Portable (Windows x64)

Run LumiDesk.exe. It does not require Go, Node.js, Git, or a terminal.
The portable executable still requires the Microsoft Edge WebView2 Runtime.
WebView2 is included with Windows 11 and is present on most Windows 10 PCs;
if it is missing, install it from Microsoft's official WebView2 Runtime page:
https://developer.microsoft.com/microsoft-edge/webview2/

LumiDesk stores settings and presets in your Windows user profile:
  %APPDATA%\LumiDesk\settings.json
  %APPDATA%\LumiDesk\presets.json

Diagnostic logs:
  %LOCALAPPDATA%\LumiDesk\logs

See LICENSE, LICENSE-NOTICES.md, and third_party\licenses for notices. The
portable executable does not change the Windows startup registry unless you
enable that setting.
