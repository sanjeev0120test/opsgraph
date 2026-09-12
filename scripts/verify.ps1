#!/usr/bin/env pwsh
# Fast local validation for Windows - keeps your laptop responsive.
# Heavy checks (race, 3-OS matrix, cross-compile) run in GitHub Actions instead.
$ErrorActionPreference = "Stop"
$env:CGO_ENABLED = "0"

Set-Location (Split-Path $PSScriptRoot -Parent)

Write-Host "==> gofmt" -ForegroundColor Cyan
$unformatted = (gofmt -l .)
if ($unformatted) { Write-Host "Unformatted files:`n$unformatted" -ForegroundColor Red; exit 1 }

Write-Host "==> go vet" -ForegroundColor Cyan
go vet ./...
if ($LASTEXITCODE -ne 0) { exit 1 }

Write-Host "==> go tool staticcheck" -ForegroundColor Cyan
go tool staticcheck ./...
if ($LASTEXITCODE -ne 0) { exit 1 }

Write-Host "==> go mod tidy (check)" -ForegroundColor Cyan
go mod tidy
if ($LASTEXITCODE -ne 0) { exit 1 }
git diff --exit-code go.mod go.sum
if ($LASTEXITCODE -ne 0) { Write-Host "go.mod/go.sum dirty after tidy" -ForegroundColor Red; exit 1 }

Write-Host "==> go build" -ForegroundColor Cyan
go build -trimpath -ldflags="-s -w -buildid=" -o bin/opsgraph.exe ./cmd/opsgraph
if ($LASTEXITCODE -ne 0) { exit 1 }

Write-Host "==> go test (no race, short)" -ForegroundColor Cyan
go test -short ./...
if ($LASTEXITCODE -ne 0) { exit 1 }

if (Test-Path ./fixtures/incident_checkout) {
    Write-Host "==> validate-fixture" -ForegroundColor Cyan
    & ./bin/opsgraph.exe validate-fixture ./fixtures/incident_checkout
    if ($LASTEXITCODE -ne 0) { exit 1 }

    Write-Host "==> fixture/golden test" -ForegroundColor Cyan
    & ./bin/opsgraph.exe test ./fixtures/incident_checkout
    if ($LASTEXITCODE -ne 0) { exit 1 }

    Write-Host "==> demo" -ForegroundColor Cyan
    & ./bin/opsgraph.exe demo --format json | Out-Null
    if ($LASTEXITCODE -ne 0) { exit 1 }
}

if (Test-Path ./fixtures/fleet_healthy) {
    Write-Host "==> fleet_healthy" -ForegroundColor Cyan
    & ./bin/opsgraph.exe validate-fixture ./fixtures/fleet_healthy
    if ($LASTEXITCODE -ne 0) { exit 1 }
    & ./bin/opsgraph.exe health --fixture ./fixtures/fleet_healthy --strict | Out-Null
    if ($LASTEXITCODE -ne 0) { exit 1 }
}

if (Test-Path ./fixtures/ci_live_k8s/.opsgraph.yaml) {
    Write-Host "==> live k8s smoke" -ForegroundColor Cyan
    & ./bin/opsgraph.exe ask checkout --config ./fixtures/ci_live_k8s/.opsgraph.yaml --format json | Out-Null
    if ($LASTEXITCODE -ne 0) { exit 1 }
}

Write-Host "==> graph mermaid stdout" -ForegroundColor Cyan
$errFile = Join-Path $env:TEMP ("opsgraph-mermaid-err-" + [guid]::NewGuid().ToString() + ".txt")
$mermaid = & ./bin/opsgraph.exe graph --fixture ./fixtures/incident_checkout --format mermaid 2>$errFile
if ($LASTEXITCODE -ne 0) { exit 1 }
if ((Test-Path $errFile) -and (Get-Item $errFile).Length -gt 0) {
    Write-Host "graph wrote to stderr:" -ForegroundColor Red
    Get-Content $errFile | Write-Host
    Remove-Item $errFile -ErrorAction SilentlyContinue
    exit 1
}
Remove-Item $errFile -ErrorAction SilentlyContinue
if ($mermaid -notmatch 'flowchart') {
    Write-Host "mermaid output missing flowchart" -ForegroundColor Red
    exit 1
}

Write-Host "OK - local validation passed" -ForegroundColor Green
