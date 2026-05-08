package deployment

import (
	"log"

	"github.com/polaris-slo-cloud/stardust-go/internal/network"
	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
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

	// Resolve hardware plugins for strategy evaluation
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

// PostSimulationStep runs the localized orchestration for all ground stations.
func (p *GroundStationOrchestratorPlugin) PostSimulationStep(sim types.SimulationController) error {
	gss := sim.GetGroundStations()
	allSats := sim.GetSatellites()

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

	// Each Ground Station acts independently
	for _, gs := range gss {
		p.processGroundStation(gs, allSats, sunPlugin)
	}

	return nil
}

func (p *GroundStationOrchestratorPlugin) processGroundStation(gs types.GroundStation, allSats []types.Satellite, sunPlugin stateplugin.SunStatePlugin) {
	// ==========================================
	// A) Proactive Offloading (Evaluation)
	// ==========================================
	visibleSats := gs.GetVisibleSatellites()
	for _, sat := range visibleSats {
		comp := sat.GetComputing()
		if comp == nil {
			continue
		}
		services := comp.GetServices()
		if len(services) == 0 {
			continue
		}

		isCritical := false
		if p.thermalPlugin != nil && p.thermalPlugin.IsOverheating(sat) {
			isCritical = true
		}
		if p.batteryPlugin != nil && p.batteryPlugin.IsCritical(sat) {
			isCritical = true
		}

		// Check if the strategy strictly rejects the satellite (Score < 0)
		if !isCritical {
			score := p.Strategy.Evaluate(gs, sat, nil, sunPlugin, p.thermalPlugin, p.batteryPlugin)
			if score < 0 {
				isCritical = true
			}
		}

		if isCritical {
			// Migrate all tasks from the critical satellite
			for _, task := range services {
				bestSat := p.findBestSatellite(sat, task, allSats, sunPlugin)
				if bestSat != nil && bestSat.GetName() != sat.GetName() {
					// 1. ISL Network Transmission (Charges Battery based on payload size!)
					_, err := p.networkService.Transmit(sat, bestSat, task)
					if err == nil {
						// 2. Logical Handoff
						comp.RemoveDeploymentAsync(task)
						bestSat.GetComputing().TryPlaceDeploymentAsync(task)
						log.Printf("[Eviction] GS %s migrated %s from %s to %s", gs.GetName(), task.GetServiceName(), sat.GetName(), bestSat.GetName())
					}
				}
			}
		}
	}

	// ==========================================
	// B) Deployment of New Tasks
	// ==========================================
	queue := gs.GetTaskQueue()
	gs.ClearTaskQueue() // Clear local queue, unplaced tasks will be re-enqueued

	for _, task := range queue {
		bestSat := p.findBestSatellite(gs, task, allSats, sunPlugin)
		if bestSat != nil {
			// 1. ISL Network Transmission from GS to Sat
			_, err := p.networkService.Transmit(gs, bestSat, task)
			if err == nil {
				// 2. Placement on Computing Node
				placed, _ := bestSat.GetComputing().TryPlaceDeploymentAsync(task)
				if placed {
					log.Printf("[Deployment] GS %s placed %s on %s", gs.GetName(), task.GetServiceName(), bestSat.GetName())
					continue // Success
				}
			}
		}
		// If unreachable or placement failed, keep in local queue for the next tick
		gs.EnqueueTask(task)
	}
}

// findBestSatellite evaluates all candidates using Strategy, Resources, and Network Cost.
func (p *GroundStationOrchestratorPlugin) findBestSatellite(source types.Node, task types.DeployableService, allSats []types.Satellite, sunPlugin stateplugin.SunStatePlugin) types.Satellite {
	var bestSat types.Satellite
	bestCombinedScore := -1.0

	for _, targetSat := range allSats {
		// 1. Hard Constraints
		if !targetSat.GetComputing().CanPlace(task) {
			continue
		}
		if p.thermalPlugin != nil && p.thermalPlugin.IsOverheating(targetSat) {
			continue
		}
		if p.batteryPlugin != nil && p.batteryPlugin.IsCritical(targetSat) {
			continue
		}

		// 2. Strategy Score (e.g. Coldest, Dark, etc.)
		strategyScore := p.Strategy.Evaluate(source, targetSat, task, sunPlugin, p.thermalPlugin, p.batteryPlugin)
		if strategyScore < 0 {
			continue // Strict rejection by strategy
		}

		// 3. Network Routing Cost (Penalize distant satellites)
		router := source.GetRouter()
		if router == nil {
			continue
		}
		route, err := router.RouteToNode(targetSat)
		if err != nil || !route.Reachable() {
			continue // Physically unreachable
		}

		latency := route.Latency()
		networkScore := 1.0
		if latency > 0 {
			// Inverse latency function: closer satellites score closer to 1.0
			networkScore = 100.0 / float64(100+latency)
		}

		// 4. Resource Score
		resourceScore := targetSat.GetComputing().CpuAvailable() / 512.0
		if resourceScore > 1.0 {
			resourceScore = 1.0
		}

		// 5. Combined Score Weighting
		combinedScore := (strategyScore * 0.5) + (networkScore * 0.3) + (resourceScore * 0.2)

		if combinedScore > bestCombinedScore {
			bestCombinedScore = combinedScore
			bestSat = targetSat
		}
	}

	return bestSat
}
