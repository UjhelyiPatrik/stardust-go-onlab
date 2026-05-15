# ==========================================================
# Stardust Orchestration Batch Runner (PowerShell)
# ==========================================================

$Strategies = @("coldest", "dark", "sunny", "anywhere")

$SimConfig = "./resources/configs/simulationManualConfig-1000.yaml"
$IslConfig = "./resources/configs/islMstConfig.yaml"
$GlConfig = "./resources/configs/groundLinkNearestConfig.yaml"
$CompConfig = "./resources/configs/computingConfig.yaml"
$RouterConfig = "./resources/configs/routerAStarConfig.yaml"
$SimPlugins = "PhysicalPluginCoordinator"
$StatePlugins = "ThermalEnvironmentStatePlugin"
$OutFile = ".\results\precomputed\physical_state_output.json"

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " Stardust Szimulációs Kötegelt Futtató Indítása" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

foreach ($Strategy in $Strategies) {
    Write-Host "[INFO] Szimuláció indítása: ---> $Strategy <---" -ForegroundColor Yellow
    
    # A szimuláció futtatása
    go run .\cmd\stardust\main.go `
        -simulationConfig $SimConfig `
        -islConfig $IslConfig `
        -groundLinkConfig $GlConfig `
        -computingConfig $CompConfig `
        -routerConfig $RouterConfig `
        -simulationPlugins $SimPlugins `
        -statePlugins $StatePlugins `
        -simulationStateOutputFile $OutFile `
        -orchestrator $Strategy
        
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[SIKER] A(z) $Strategy szimuláció lefutott. Adatok mentve." -ForegroundColor Green
    } else {
        Write-Host "[HIBA] A(z) $Strategy szimuláció során kritikus hiba lépett fel!" -ForegroundColor Red
        exit 1
    }
    Write-Host "----------------------------------------------------------" -ForegroundColor DarkGray
}

Write-Host "[KÉSZ] Minden orchestrációs stratégia sikeresen lefutott!" -ForegroundColor Cyan
Write-Host "Futtathatja a Python adatelemző scriptet a grafikonok elkészítéséhez." -ForegroundColor Cyan