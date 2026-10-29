param (
    [string]$SimConfig = "./resources/configs/simulationManualConfig-6000.yaml"
)

# ==========================================================
# Stardust Orchestration Batch Runner (PowerShell)
# ==========================================================

[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$Strategies = @("coldest", "dark", "sunny", "anywhere")

$IslConfig = "./resources/configs/islMstConfig.yaml"
$GlConfig = "./resources/configs/groundLinkNearestConfig.yaml"
$CompConfig = "./resources/configs/computingConfig.yaml"
$RouterConfig = "./resources/configs/routerAStarConfig.yaml"
$SimPlugins = "PhysicalPluginCoordinator"
$StatePlugins = "ThermalEnvironmentStatePlugin"
$PrecompDir = ".\results\precomputed"

$configBase = [System.IO.Path]::GetFileNameWithoutExtension($SimConfig)
$PrecompFile = "$PrecompDir\physical_state_$configBase.gob"

if (-not (Test-Path $PrecompDir)) {
    New-Item -ItemType Directory -Force -Path $PrecompDir | Out-Null
}

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " Stardust Simulation Batch Runner" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

Write-Host "[BUILD] Compiling stardust.exe..." -ForegroundColor Cyan
go build -o stardust.exe .\cmd\stardust\main.go
if ($LASTEXITCODE -ne 0) {
    Write-Host "[ERROR] Build failed!" -ForegroundColor Red
    exit 1
}

$FirstRun = $true

foreach ($Strategy in $Strategies) {
    if ($FirstRun -and -not (Test-Path $PrecompFile)) {
        Write-Host "[INFO] 1st Run ($Strategy) + Precomputing constant physical and network states..." -ForegroundColor Yellow
        .\stardust.exe `
            -simulationConfig $SimConfig `
            -islConfig $IslConfig `
            -groundLinkConfig $GlConfig `
            -computingConfig $CompConfig `
            -routerConfig $RouterConfig `
            -simulationPlugins $SimPlugins `
            -statePlugins $StatePlugins `
            -simulationStateOutputFile $PrecompFile `
            -orchestrator $Strategy
        $FirstRun = $false
    } else {
        Write-Host "[INFO] Starting Precomputed (Multi-threaded Streaming) simulation: ---> $Strategy <---" -ForegroundColor Yellow
        .\stardust.exe `
            -simulationConfig $SimConfig `
            -computingConfig $CompConfig `
            -routerConfig $RouterConfig `
            -simulationPlugins $SimPlugins `
            -simulationStateInputFile $PrecompFile `
            -orchestrator $Strategy
    }

    if ($LASTEXITCODE -eq 0) {
        Write-Host "[SUCCESS] Strategy '$Strategy' completed and telemetry saved." -ForegroundColor Green
    } else {
        Write-Host "[ERROR] Strategy '$Strategy' encountered an error!" -ForegroundColor Red
        exit 1
    }
    Write-Host "----------------------------------------------------------" -ForegroundColor DarkGray
}

Write-Host "[DONE] All orchestration strategies completed successfully!" -ForegroundColor Cyan
