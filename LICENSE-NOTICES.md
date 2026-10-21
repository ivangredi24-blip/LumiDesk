# License and third-party notices

LumiDesk's own source is licensed under the MIT License; see `LICENSE`.

## Direct software dependencies

- **LotusLantern** (`github.com/Rxflex/LotusLantern`), MIT License. Copyright
  © 2026 the LotusLantern contributors. LumiDesk imports the module and does
  not modify its source. <https://github.com/Rxflex/LotusLantern>
- **TinyGo Bluetooth** (`tinygo.org/x/bluetooth`), BSD 3-Clause License.
  <https://github.com/tinygo-org/bluetooth>
- **Wails** (`github.com/wailsapp/wails/v2`), MIT License.
  <https://github.com/wailsapp/wails>
- **getlantern/systray**, Apache License 2.0.
  <https://github.com/getlantern/systray>
- **golang.org/x/sys**, BSD 3-Clause License.
  <https://pkg.go.dev/golang.org/x/sys>
- **React** and **React DOM**, MIT License.
  <https://github.com/facebook/react>
- **Vite**, MIT License. <https://github.com/vitejs/vite>
- **TypeScript**, Apache License 2.0.
  <https://github.com/microsoft/TypeScript>
- **lucide-react**, ISC License. <https://github.com/lucide-icons/lucide>
- **ELK-BLEDOM effect ID reference**, MIT License. LumiDesk uses the public
  ELK-BLEDOB effect names/IDs as a cross-reference; no Home Assistant source
  code is bundled. <https://github.com/dave-code-ruiz/elkbledom>

The Go and npm lockfiles pin the complete transitive dependency graph used for
the release build. Go dependency license texts are included under
`third_party/licenses/` (including the LotusLantern, TinyGo, Wails, and
transitive Go module notices). License texts for the production npm bundle are
under `third_party/licenses/npm/`. The bundled Go licenses include Apache-2.0
notices for getlantern/systray and several Wails dependencies, BSD-2/3-Clause
notices, ISC, and MIT notices. No LotusLantern repository files are copied or
altered.

The Vite, TypeScript, and React Vite plugin are build-time-only dependencies;
their versions are pinned in `frontend/package-lock.json`. They are not
included as standalone runtime libraries in the installed application.

## Microsoft WebView2 Runtime

The installer includes Microsoft's small Evergreen WebView2 bootstrapper and
uses it only when the WebView2 Runtime is not already registered. It downloads
and installs the runtime from Microsoft. The runtime is Microsoft's software
and is subject to Microsoft's license and privacy terms. The bootstrapper and
runtime are not part of LumiDesk's MIT-licensed source.

## Build tooling

Wails' build tools and Inno Setup's compiler are build-time dependencies. The
Inno Setup compiler is not bundled in the installed application. Inno Setup
remains under its upstream license: <https://jrsoftware.org/isinfo.php>.
