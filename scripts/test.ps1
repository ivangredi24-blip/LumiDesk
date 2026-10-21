$ErrorActionPreference = 'Stop'
$ProjectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $ProjectRoot
try {
    Write-Host 'Running Go tests...'
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw "go test failed with exit code $LASTEXITCODE" }

    Write-Host 'Building the React frontend...'
    Push-Location (Join-Path $ProjectRoot 'frontend')
    try {
        npm ci
        if ($LASTEXITCODE -ne 0) { throw "npm ci failed with exit code $LASTEXITCODE" }
        npm run build
        if ($LASTEXITCODE -ne 0) { throw "frontend build failed with exit code $LASTEXITCODE" }
    }
    finally { Pop-Location }

    Write-Host 'All automated test/build checks passed.'
}
finally { Pop-Location }
