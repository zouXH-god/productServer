$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$frontend = Join-Path $root 'frontend'
$env:npm_config_cache = Join-Path $frontend '.npm-cache'
$envFile = Join-Path $root '.env'
if (Test-Path $envFile) {
    Get-Content $envFile | ForEach-Object {
        $line = $_.Trim()
        if ($line -and -not $line.StartsWith('#')) {
            $parts = $line -split '=', 2
            if ($parts.Count -eq 2) { Set-Item -Path "Env:$($parts[0].Trim())" -Value $parts[1].Trim() }
        }
    }
}
if (-not (Test-Path (Join-Path $frontend 'node_modules'))) {
    Write-Host '[dev] Installing frontend dependencies...'
    Push-Location $frontend
    try { npm install } finally { Pop-Location }
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
Write-Host '[dev] Backend: http://localhost:8080'
Write-Host '[dev] Frontend: http://localhost:5173'
$backend = Start-Process -FilePath 'go' -ArgumentList @('run', '.') -WorkingDirectory $root -NoNewWindow -PassThru
Start-Sleep -Seconds 2
$worker = Start-Process -FilePath 'go' -ArgumentList @('run', '.', 'worker') -WorkingDirectory $root -NoNewWindow -PassThru
$web = Start-Process -FilePath 'npm.cmd' -ArgumentList @('run', 'dev') -WorkingDirectory $frontend -NoNewWindow -PassThru
try {
    while (-not $backend.HasExited -and -not $worker.HasExited -and -not $web.HasExited) { Start-Sleep -Milliseconds 500 }
    if ($backend.HasExited) { $code = $backend.ExitCode; Write-Host "[dev] Backend exited ($code)." }
    else { $code = $web.ExitCode; Write-Host "[dev] Frontend exited ($code)." }
} finally {
    foreach ($process in @($backend, $worker, $web)) {
        if ($process -and -not $process.HasExited) { Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue }
    }
}
if ($null -eq $code) { $code = 1 }
exit $code
