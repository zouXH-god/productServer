$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$env:npm_config_cache = Join-Path $root 'frontend/.npm-cache'
Push-Location (Join-Path $root 'frontend')
try { npm ci; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; npm test; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; npm run build; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE } } finally { Pop-Location }
Push-Location $root
try { go test ./...; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; New-Item -ItemType Directory -Force -Path 'bin' | Out-Null; go build -trimpath -ldflags '-s -w' -o 'bin/productserver.exe' .; exit $LASTEXITCODE } finally { Pop-Location }
