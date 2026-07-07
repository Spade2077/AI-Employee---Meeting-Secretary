param(
  [switch]$NoOpen
)

$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$MeetingUrl = "http://127.0.0.1:8090/meeting"

function Stop-PortProcess {
  param([int]$Port)

  $processIds = @(Get-NetTCPConnection -LocalPort $Port -ErrorAction SilentlyContinue |
    Where-Object { $_.State -eq "Listen" -or $_.State -eq "Established" } |
    Select-Object -ExpandProperty OwningProcess -Unique)

  foreach ($processId in $processIds) {
    if ($processId -and $processId -ne $PID) {
      Stop-Process -Id $processId -Force -ErrorAction SilentlyContinue
    }
  }
}

function Wait-Url {
  param(
    [string]$Url,
    [int]$Seconds = 30
  )

  $deadline = (Get-Date).AddSeconds($Seconds)
  while ((Get-Date) -lt $deadline) {
    try {
      $response = Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec 2
      if ($response.StatusCode -ge 200 -and $response.StatusCode -lt 500) {
        return $true
      }
    } catch {
      Start-Sleep -Milliseconds 600
    }
  }
  return $false
}

function Start-HiddenScript {
  param([string]$ScriptPath)

  $args = "-NoProfile -ExecutionPolicy Bypass -File `"$ScriptPath`""
  Start-Process -FilePath "powershell.exe" `
    -ArgumentList $args `
    -WorkingDirectory $Root `
    -WindowStyle Hidden `
    -PassThru
}

Set-Location $Root

Write-Host "Restarting local CAM++ ASR and meeting backend..."
Stop-PortProcess -Port 9001
Stop-PortProcess -Port 8090
Start-Sleep -Seconds 1

$asrScript = Join-Path $Root "scripts\run-sensevoice-local.ps1"
$apiScript = Join-Path $Root "scripts\run-meeting-core-local-asr.ps1"

$asrProcess = Start-HiddenScript -ScriptPath $asrScript
if (-not (Wait-Url -Url "http://127.0.0.1:9001/health" -Seconds 40)) {
  throw "Local ASR did not become healthy. Check local-asr-service\.tmp\sensevoice-local-task.log"
}

$apiProcess = Start-HiddenScript -ScriptPath $apiScript
if (-not (Wait-Url -Url $MeetingUrl -Seconds 20)) {
  throw "Meeting backend did not become ready. Check meeting-core-service.local-asr.task.log"
}

Write-Host "ASR PID: $($asrProcess.Id)"
Write-Host "Backend launcher PID: $($apiProcess.Id)"
if (-not $NoOpen) {
  Write-Host "Opening $MeetingUrl"
  Start-Process $MeetingUrl
} else {
  Write-Host "Ready: $MeetingUrl"
}
