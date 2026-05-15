package deployment

import (
	"log"

	"github.com/polaris-slo-cloud/stardust-go/internal/network"
	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/helper"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.SimulationPlugin = (*GroundStationOrchestratorPlugin)(nil)

// GroundStationOrchestratorPlugin executes local deployment and eviction logic for each Ground Station.
type GroundStationOrchestratorPlugin struct {
	strategyName   string
	Strategy       OrchestrationStrategy
	thermalPlugin  *simplugin.ThermalSimPlugin
	batteryPlugin  *simplugin.BatterySimPlugin
	networkService *network.NetworkService
}

// NewGroundStationOrchestratorPlugin initializes the orchestrator with the selected strategy.
func NewGroundStationOrchestratorPlugin(strategyName string, physicalPlugins []types.SimulationPlugin) *GroundStationOrchestratorPlugin {
	orch := &GroundStationOrchestratorPlugin{
		strategyName:   strategyName,
		Strategy:       GetStrategy(strategyName),
		networkService: network.NewNetworkService(),
	}

	for _, p := range physicalPlugins {
		if tp, ok := p.(*simplugin.ThermalSimPlugin); ok {
			orch.thermalPlugin = tp
		}
		if bp, ok := p.(*simplugin.BatterySimPlugin); ok {
			orch.batteryPlugin = bp
		}
	}
	return orch
}

func (p *GroundStationOrchestratorPlugin) Name() string {
	return "GroundStationOrchestratorPlugin"
}

// PostSimulationStep runs the localized orchestration for all ground stations concurrently.
func (p *GroundStationOrchestratorPlugin) PostSimulationStep(sim types.SimulationController) error {
	gss := sim.GetGroundStations()

	var sunPlugin stateplugin.SunStatePlugin
	repo := sim.GetStatePluginRepository()
	if repo != nil {
		for _, sp := range repo.GetAllPlugins() {
			if envPlugin, ok := sp.(stateplugin.SunStatePlugin); ok {
				sunPlugin = envPlugin
				break
			}
		}
	}

	// ==========================================
	// CONCURRENCY OPTIMIZATION
	// Execute each Ground Station's orchestration in a separate Goroutine.
	// ==========================================
	helper.ParallelFor(gss, func(gs types.GroundStation) {
		p.processGroundStation(gs, sunPlugin)
	})

	return nil
}

func (p *GroundStationOrchestratorPlugin) processGroundStation(gs types.GroundStation, sunPlugin stateplugin.SunStatePlugin) {
	visibleSats := gs.GetVisibleSatellites()

	// OPTIMALIZÁCIÓ 1: Ha a GS felett épp nincs aktív műhold, nincs értelme számolni.
	// A queue-ban lévő taskok megvárják, amíg jön egy műhold (Store & Forward).
	if len(visibleSats) == 0 {
		return
	}

	// ==========================================
	// A) Proactive Offloading (Evaluation)
	// ==========================================
	for _, sat := range visibleSats {
		comp := sat.GetComputing()
		if comp == nil {
			continue
		}
		services := comp.GetServices()
		if len(services) == 0 {
			continue
		}

		evictionReason := ""
		isCritical := false

		if p.thermalPlugin != nil && p.thermalPlugin.IsOverheating(sat) {
			isCritical = true
			evictionReason = "THERMAL_OVERLOAD"
		}
		if !isCritical && p.batteryPlugin != nil && p.batteryPlugin.IsCritical(sat) {
			isCritical = true
			evictionReason = "BATTERY_CRITICAL"
		}

		// Strictly environmental health-check (Task is nil)
		if !isCritical {
			score := p.Strategy.Evaluate(gs, sat, nil, sunPlugin, p.thermalPlugin, p.batteryPlugin)
			if score < 0 {
				isCritical = true
				evictionReason = "PROACTIVE_STRATEGY_VIOLATION"
			}
		}

		if isCritical {
			for _, task := range services {
				bestSat := p.findBestSatellite(sat, task, visibleSats, sunPlugin)
				if bestSat != nil && bestSat.GetName() != sat.GetName() {
					_, err := p.networkService.Transmit(sat, bestSat, task)
					if err == nil {
						comp.RemoveDeploymentAsync(task)
						bestSat.GetComputing().TryPlaceDeploymentAsync(task)
						// BŐVÍTETT LOGOLÁS
						log.Printf("[Eviction] GS %s migrated %s from %s to %s (Reason: %s)", gs.GetName(), task.GetServiceName(), sat.GetName(), bestSat.GetName(), evictionReason)
					}
				}
			}
		}
	}

	// ==========================================
	// B) Deployment of New Tasks
	// ==========================================
	queue := gs.GetTaskQueue()
	gs.ClearTaskQueue()

	for _, task := range queue {
		// OPTIMALIZÁCIÓ 3: Új feladatot is CSAK a lokális poolból (visibleSats) választott műholdra küldünk!
		bestSat := p.findBestSatellite(gs, task, visibleSats, sunPlugin)
		if bestSat != nil {
			_, err := p.networkService.Transmit(gs, bestSat, task)
			if err == nil {
				placed, _ := bestSat.GetComputing().TryPlaceDeploymentAsync(task)
				if placed {
					log.Printf("[Deployment] GS %s placed %s on %s", gs.GetName(), task.GetServiceName(), bestSat.GetName())
					continue
				}
			}
		}
		// Ha nincs megfelelő lokális célpont, a feladat visszakerül a sorba (megvárja a következő műholdat)
		gs.EnqueueTask(task)
	}
}

// findBestSatellite evaluates local candidates using Strategy, Resources, and Network Cost.
func (p *GroundStationOrchestratorPlugin) findBestSatellite(source types.Node, task types.DeployableService, candidateSats []types.Satellite, sunPlugin stateplugin.SunStatePlugin) types.Satellite {
	var bestSat types.Satellite
	bestCombinedScore := -1.0

	for _, targetSat := range candidateSats {
		if !targetSat.GetComputing().CanPlace(task) {
			continue
		}
		if p.thermalPlugin != nil && p.thermalPlugin.IsOverheating(targetSat) {
			continue
		}
		if p.batteryPlugin != nil && p.batteryPlugin.IsCritical(targetSat) {
			continue
		}

		strategyScore := p.Strategy.Evaluate(source, targetSat, task, sunPlugin, p.thermalPlugin, p.batteryPlugin)
		if strategyScore < 0 {
			continue
		}

		router := source.GetRouter()
		if router == nil {
			continue
		}

		// OPTIMALIZÁCIÓ 4: Mivel a targetSat garantáltan a lokális zónában (visibleSats) van,
		// az útvonalkeresés (A*/Dijkstra) elképesztően gyors lesz, hiszen nagyon kevés ugrásból (hop) elérhető!
		route, err := router.RouteToNode(targetSat)
		if err != nil || !route.Reachable() {
			continue
		}

		latency := route.Latency()
		networkScore := 1.0
		if latency > 0 {
			networkScore = 100.0 / float64(100+latency)
		}

		resourceScore := targetSat.GetComputing().CpuAvailable() / 512.0
		if resourceScore > 1.0 {
			resourceScore = 1.0
		}

		combinedScore := (strategyScore * 0.5) + (networkScore * 0.3) + (resourceScore * 0.2)

		if combinedScore > bestCombinedScore {
			bestCombinedScore = combinedScore
			bestSat = targetSat
		}
	}

	return bestSat
}
