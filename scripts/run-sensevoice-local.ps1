$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$ServiceDir = Join-Path $Root "local-asr-service"
$LogDir = Join-Path $ServiceDir ".tmp"
New-Item -ItemType Directory -Force -Path $LogDir | Out-Null

$EnvFile = Join-Path $Root ".env"
if (Test-Path $EnvFile) {
  Get-Content $EnvFile | ForEach-Object {
    $line = $_.Trim()
    if ($line -and -not $line.StartsWith("#") -and $line.Contains("=")) {
      $parts = $line.Split("=", 2)
      [Environment]::SetEnvironmentVariable($parts[0].Trim(), $parts[1].Trim(), "Process")
    }
  }
}

Set-Location $ServiceDir

& (Join-Path $Root ".venv\Scripts\python.exe") "sensevoice_server.py" --port 9001 `
  *> (Join-Path $LogDir "sensevoice-local-task.log")
