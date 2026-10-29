package deployment

import (
	"math"
	"sort"

	"github.com/polaris-slo-cloud/stardust-go/internal/metrics"
	"github.com/polaris-slo-cloud/stardust-go/internal/network"
	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/helper"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.SimulationPlugin = (*GroundStationOrchestratorPlugin)(nil)

// rankedSatelliteCandidate holds a pre-evaluated, pre-routed candidate satellite and its composite fitness score.
type rankedSatelliteCandidate struct {
	satellite     types.Satellite
	computing     types.Computing
	combinedScore float64
}

type inPlaceTaskQueueGS interface {
	PeekTaskQueue() []types.DeployableService
	SetTaskQueue(tasks []types.DeployableService)
}

// GroundStationOrchestratorPlugin executes localized task placement and proactive eviction for each Ground Station.
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

// PostSimulationStep runs localized orchestration for all ground stations concurrently across CPU cores.
func (p *GroundStationOrchestratorPlugin) PostSimulationStep(sim types.SimulationController) error {
	gss := sim.GetGroundStations()
	if len(gss) == 0 {
		return nil
	}

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

	helper.ParallelFor(gss, func(gs types.GroundStation) {
		p.processGroundStation(gs, sunPlugin)
	})

	return nil
}

func (p *GroundStationOrchestratorPlugin) processGroundStation(gs types.GroundStation, sunPlugin stateplugin.SunStatePlugin) {
	visibleSats := gs.GetVisibleSatellites()
	if len(visibleSats) == 0 {
		return
	}

	var evictedCount uint64
	var deployedCount uint64

	// ==========================================
	// A) Proactive Offloading (Health Evaluation)
	// ==========================================
	for _, sat := range visibleSats {
		comp := sat.GetComputing()
		if comp == nil || comp.GetActiveServiceCount() == 0 {
			continue
		}

		score := p.Strategy.Evaluate(gs, sat, nil, sunPlugin, p.thermalPlugin, p.batteryPlugin)
		if score < 0 {
			services := comp.GetServices()
			if len(services) == 0 {
				continue
			}

			minServiceMem := math.MaxFloat64
			for _, task := range services {
				if m := task.GetMemoryUsage(); m < minServiceMem {
					minServiceMem = m
				}
			}

			candidates := p.buildRankedCandidatesWithMinMem(sat, visibleSats, sunPlugin, minServiceMem)
			if len(candidates) == 0 {
				continue
			}

			minUnplaceableMem := math.MaxFloat64
			for _, task := range services {
				reqMem := task.GetMemoryUsage()
				if reqMem >= minUnplaceableMem {
					continue
				}
				bestSat := selectBestFitFromRanked(task, candidates)
				if bestSat != nil && bestSat.GetName() != sat.GetName() {
					_, err := p.networkService.Transmit(sat, bestSat, task)
					if err == nil {
						_ = comp.RemoveDeploymentAsync(task)
						placed, _ := bestSat.GetComputing().TryPlaceDeploymentAsync(task)
						if placed {
							evictedCount++
						}
					}
				} else if bestSat == nil {
					minUnplaceableMem = reqMem
				}
			}
		}
	}

	if evictedCount > 0 {
		metrics.RecordTasksEvicted(evictedCount)
	}

	// ==========================================
	// B) Deployment of New Tasks (Zero-Copy In-Place Queue Dispatch)
	// ==========================================
	if fastQ, ok := gs.(inPlaceTaskQueueGS); ok {
		queue := fastQ.PeekTaskQueue()
		if len(queue) == 0 {
			return
		}

		minQueueMem := math.MaxFloat64
		for _, task := range queue {
			if m := task.GetMemoryUsage(); m < minQueueMem {
				minQueueMem = m
			}
		}

		// Evaluate and rank visible satellites that have at least minQueueMem free RAM
		rankedCandidates := p.buildRankedCandidatesWithMinMem(gs, visibleSats, sunPlugin, minQueueMem)
		if len(rankedCandidates) == 0 {
			return
		}

		minUnplaceableMem := math.MaxFloat64
		writeIdx := 0

		for i, task := range queue {
			reqMem := task.GetMemoryUsage()
			if reqMem < minUnplaceableMem {
				bestSat := selectBestFitFromRanked(task, rankedCandidates)
				if bestSat != nil {
					_, err := p.networkService.Transmit(gs, bestSat, task)
					if err == nil {
						placed, _ := bestSat.GetComputing().TryPlaceDeploymentAsync(task)
						if placed {
							deployedCount++
							continue
						}
					}
				} else {
					minUnplaceableMem = reqMem
				}
			}
			queue[writeIdx] = task
			if writeIdx != i {
				queue[i] = nil
			}
			writeIdx++
		}

		for i := writeIdx; i < len(queue); i++ {
			queue[i] = nil
		}
		fastQ.SetTaskQueue(queue[:writeIdx])

		if deployedCount > 0 {
			metrics.RecordTasksDeployed(deployedCount)
		}
		return
	}

	queue := gs.GetTaskQueue()
	if len(queue) == 0 {
		return
	}

	minQueueMem := math.MaxFloat64
	for _, task := range queue {
		if m := task.GetMemoryUsage(); m < minQueueMem {
			minQueueMem = m
		}
	}

	rankedCandidates := p.buildRankedCandidatesWithMinMem(gs, visibleSats, sunPlugin, minQueueMem)
	if len(rankedCandidates) == 0 {
		return
	}

	gs.ClearTaskQueue()
	minUnplaceableMem := math.MaxFloat64

	for _, task := range queue {
		reqMem := task.GetMemoryUsage()
		if reqMem < minUnplaceableMem {
			bestSat := selectBestFitFromRanked(task, rankedCandidates)
			if bestSat != nil {
				_, err := p.networkService.Transmit(gs, bestSat, task)
				if err == nil {
					placed, _ := bestSat.GetComputing().TryPlaceDeploymentAsync(task)
					if placed {
						deployedCount++
						continue
					}
				}
			} else {
				minUnplaceableMem = reqMem
			}
		}
		gs.EnqueueTask(task)
	}

	if deployedCount > 0 {
		metrics.RecordTasksDeployed(deployedCount)
	}
}

// buildRankedCandidatesWithMinMem evaluates health, strategy score, minimum memory, and network latency once per candidate satellite,
// returning eligible candidates sorted in descending order of their composite score.
func (p *GroundStationOrchestratorPlugin) buildRankedCandidatesWithMinMem(source types.Node, candidateSats []types.Satellite, sunPlugin stateplugin.SunStatePlugin, minReqMem float64) []rankedSatelliteCandidate {
	router := source.GetRouter()
	if router == nil {
		return nil
	}
	if minReqMem <= 0 {
		minReqMem = 1e-9
	}

	var ranked []rankedSatelliteCandidate

	for _, targetSat := range candidateSats {
		if targetSat == source {
			continue
		}
		comp := targetSat.GetComputing()
		if comp == nil || comp.MemoryAvailable() < minReqMem {
			continue
		}
		if p.thermalPlugin != nil && p.thermalPlugin.IsOverheating(targetSat) {
			continue
		}
		if p.batteryPlugin != nil && p.batteryPlugin.IsCritical(targetSat) {
			continue
		}

		strategyScore := p.Strategy.Evaluate(source, targetSat, nil, sunPlugin, p.thermalPlugin, p.batteryPlugin)
		if strategyScore < 0 {
			continue
		}

		route, err := router.RouteToNode(targetSat)
		if err != nil || !route.Reachable() {
			continue
		}

		if ranked == nil {
			ranked = make([]rankedSatelliteCandidate, 0, len(candidateSats))
		}

		latency := route.Latency()
		networkScore := 1.0
		if latency > 0 {
			networkScore = 100.0 / float64(100+latency)
		}

		resourceScore := comp.CpuAvailable() / 512.0
		if resourceScore > 1.0 {
			resourceScore = 1.0
		}

		combinedScore := (strategyScore * 0.5) + (networkScore * 0.3) + (resourceScore * 0.2)
		ranked = append(ranked, rankedSatelliteCandidate{
			satellite:     targetSat,
			computing:     comp,
			combinedScore: combinedScore,
		})
	}

	if len(ranked) > 1 {
		sort.Slice(ranked, func(i, j int) bool {
			return ranked[i].combinedScore > ranked[j].combinedScore
		})
	}

	return ranked
}

// selectBestFitFromRanked returns the highest-scoring pre-ranked satellite that has enough memory for the task.
func selectBestFitFromRanked(task types.DeployableService, ranked []rankedSatelliteCandidate) types.Satellite {
	for i := range ranked {
		if ranked[i].computing.CanPlace(task) {
			return ranked[i].satellite
		}
	}
	return nil
}
