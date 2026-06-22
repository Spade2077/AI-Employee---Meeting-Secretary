$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$cacheRoot = Join-Path $root ".cache"
$env:GOCACHE = Join-Path $cacheRoot "go-build"
$env:GOTMPDIR = Join-Path $cacheRoot "tmp"
New-Item -ItemType Directory -Force -Path $env:GOCACHE, $env:GOTMPDIR | Out-Null

go test ./...
if ($LASTEXITCODE -ne 0) {
  throw "go test failed with exit code $LASTEXITCODE"
}

go build -buildvcs=false -trimpath -ldflags="-s -w" -o meeting-core-service.exe .
if ($LASTEXITCODE -ne 0) {
  throw "go build failed with exit code $LASTEXITCODE"
}

Write-Host "Built meeting-core-service.exe"
