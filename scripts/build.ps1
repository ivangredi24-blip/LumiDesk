param(
    [switch]$SkipTests,
    [switch]$SkipInstaller
)

$ErrorActionPreference = 'Stop'
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$BuildDir = Join-Path $ProjectRoot 'build'
Push-Location $ProjectRoot
try {
    $env:CGO_ENABLED = '0'
    if (-not $SkipTests) {
        Write-Host 'Running automated tests...'
        go test ./...
        if ($LASTEXITCODE -ne 0) { throw "go test failed with exit code $LASTEXITCODE" }
    }

    Write-Host 'Building LumiDesk for Windows x64...'
    $WailsPath = (Get-Command wails -ErrorAction SilentlyContinue).Source
    if (-not $WailsPath) {
        $GoBin = Join-Path (go env GOPATH) 'bin'
        $Candidate = Join-Path $GoBin 'wails.exe'
        if (Test-Path $Candidate) { $WailsPath = $Candidate }
    }
    if (-not $WailsPath) { throw 'Wails CLI was not found. Install the version in README.md or add it to PATH.' }
    # LumiDesk calls the Wails bridge directly through window.go and maintains
    # its API types in frontend/src/types.ts; generated wrapper files aren't used.
    # Skipping their generator also avoids a Wails 2.17 hang in this environment.
    & $WailsPath build -skipbindings -clean -platform windows/amd64 -o LumiDesk.exe -trimpath -ldflags '-s -w' -webview2 download
    if ($LASTEXITCODE -ne 0) { throw "Wails build failed with exit code $LASTEXITCODE" }

    $BuiltExe = Join-Path $BuildDir 'bin\LumiDesk.exe'
    if (-not (Test-Path $BuiltExe) -or (Get-Item $BuiltExe).Length -lt 1MB) { throw 'Wails did not produce a valid executable.' }
    Copy-Item $BuiltExe (Join-Path $BuildDir 'LumiDesk.exe') -Force
    Copy-Item $BuiltExe (Join-Path $BuildDir 'LumiDesk-Portable-x64.exe') -Force

    $PortableDir = Join-Path $BuildDir 'portable'
    New-Item -ItemType Directory -Force -Path $PortableDir | Out-Null
    Copy-Item $BuiltExe (Join-Path $PortableDir 'LumiDesk.exe') -Force
    Copy-Item (Join-Path $ProjectRoot 'README-Portable.txt') $PortableDir -Force
    Copy-Item (Join-Path $ProjectRoot 'LICENSE') $PortableDir -Force
    Copy-Item (Join-Path $ProjectRoot 'LICENSE-NOTICES.md') $PortableDir -Force
    $PortableLicenses = Join-Path $PortableDir 'third_party\licenses'
    New-Item -ItemType Directory -Force -Path $PortableLicenses | Out-Null
    Copy-Item (Join-Path $ProjectRoot 'third_party\licenses\*') $PortableLicenses -Recurse -Force
    Compress-Archive -Path (Join-Path $PortableDir '*') -DestinationPath (Join-Path $BuildDir 'LumiDesk-Portable-x64.zip') -Force

    if (-not $SkipInstaller) {
        $CompilerPath = (Get-Command ISCC.exe -ErrorAction SilentlyContinue).Source
        if (-not $CompilerPath) {
            $KnownPaths = @(
                (Join-Path $env:LOCALAPPDATA 'Programs\Inno Setup 6\ISCC.exe'),
                (Join-Path ${env:ProgramFiles(x86)} 'Inno Setup 6\ISCC.exe'),
                (Join-Path $env:ProgramFiles 'Inno Setup 6\ISCC.exe')
            )
            $CompilerPath = $KnownPaths | Where-Object { Test-Path $_ } | Select-Object -First 1
        }
        if (-not $CompilerPath) { throw 'Inno Setup 6 compiler (ISCC.exe) is required to create LumiDesk-Setup-x64.exe.' }
        Push-Location (Join-Path $ProjectRoot 'installer')
        try {
            & $CompilerPath 'LumiDesk.iss'
            if ($LASTEXITCODE -ne 0) { throw "Inno Setup failed with exit code $LASTEXITCODE" }
        }
        finally { Pop-Location }
    }

    $Artifacts = @('LumiDesk.exe', 'LumiDesk-Portable-x64.exe', 'LumiDesk-Portable-x64.zip')
    if (-not $SkipInstaller) { $Artifacts += 'LumiDesk-Setup-x64.exe' }
    $HashLines = foreach ($Name in $Artifacts) {
        $Path = Join-Path $BuildDir $Name
        if (-not (Test-Path $Path) -or (Get-Item $Path).Length -eq 0) { throw "Expected output missing: $Path" }
        $Hash = Get-FileHash $Path -Algorithm SHA256
        "{0}  {1}" -f $Hash.Hash.ToLowerInvariant(), $Name
    }
    Set-Content -Path (Join-Path $BuildDir 'SHA256SUMS.txt') -Value $HashLines -Encoding ascii
    Write-Host 'Build artifacts:'
    $Artifacts | ForEach-Object { Get-Item (Join-Path $BuildDir $_) | Select-Object Name, Length }
}
finally { Pop-Location }
