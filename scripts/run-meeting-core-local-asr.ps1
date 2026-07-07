$ErrorActionPreference = "Continue"

$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

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

if (-not $env:STT_PROVIDER) {
  $env:STT_PROVIDER = "local"
}
if (-not $env:LOCAL_ASR_URL) {
  $env:LOCAL_ASR_URL = "http://127.0.0.1:9001"
}

& (Join-Path $Root "meeting-core-service-next.exe") `
  *> (Join-Path $Root "meeting-core-service.local-asr.task.log")
