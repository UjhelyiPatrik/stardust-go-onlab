package simulation

import (
	"log"
	"sync"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/internal/computing"
	"github.com/polaris-slo-cloud/stardust-go/internal/network"
	"github.com/polaris-slo-cloud/stardust-go/internal/routing"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.SimulationController = (*SimulationService)(nil)

// SimulationService handles simulation lifecycle and state updates
type SimulationService struct {
	BaseSimulationService

	routerBuilder    *routing.RouterBuilder
	computingBuilder *computing.DefaultComputingBuilder

	simplugins      []types.SimulationPlugin
	statePluginRepo *types.StatePluginRepository
	running         bool

	simulationStateSerializer *SimulationStateSerializer
}

// NewSimulationService initializes the simulation service
func NewSimulationService(
	config *configs.SimulationConfig,
	router *routing.RouterBuilder,
	computing *computing.DefaultComputingBuilder,
	simplugins []types.SimulationPlugin,
	statePluginRepo *types.StatePluginRepository,
	simualtionStateOutputFile *string,
) *SimulationService {
	simService := &SimulationService{
		routerBuilder:    router,
		computingBuilder: computing,
		simplugins:       simplugins,
		statePluginRepo:  statePluginRepo,
	}
	simService.BaseSimulationService = NewBaseSimulationService(config, simService.runSimulationStep)

	if *simualtionStateOutputFile != "" {
		simService.simulationStateSerializer = NewSimulationStateSerializer(*simualtionStateOutputFile, statePluginRepo.GetAllPlugins())
		log.Printf("Simulation state will be serialized to %s", *simualtionStateOutputFile)
	}

	return simService
}

func (s *SimulationService) GetStatePluginRepository() *types.StatePluginRepository {
	return s.statePluginRepo
}

func (s *SimulationService) Close() {
	if s.simulationStateSerializer != nil {
		s.simulationStateSerializer.Save(s)
	}
}

// runSimulationStep is the core loop to simulate node and orchestrator logic
func (s *SimulationService) runSimulationStep(nextTime func(time.Time) time.Time) {
	if s.running {
		return
	}
	s.lock.Lock()
	if s.running {
		s.lock.Unlock()
		return
	}
	s.running = true
	s.lock.Unlock()

	// 1. Stepping time forward, calculate deltaT
	oldTime := s.simTime
	s.setSimulationTime(nextTime(s.GetSimulationTime()))
	deltaT := s.simTime.Sub(oldTime).Seconds()

	log.Printf("Simulation time is %s", s.simTime.Format(time.RFC3339))

	// Safety exit if no time has elapsed
	if deltaT <= 0 {
		s.running = false
		return
	}

	var wg sync.WaitGroup

	// 2. Update node positions (Orbital mechanics)
	for _, n := range s.all {
		wg.Add(1)
		go func(node types.Node) {
			defer wg.Done()
			node.UpdatePosition(s.simTime)
		}(n)
	}
	wg.Wait()

	// 3. Update links (Network topology changes)
	for _, n := range s.all {
		wg.Add(1)
		go func(node types.Node) {
			defer wg.Done()
			node.GetLinkNodeProtocol().UpdateLinks()
		}(n)
	}
	wg.Wait()

	// 4. CONTROL PLANE UPDATE (Orchestrator mapping)
	// Assign satellites to ground stations based on current positions and visibility
	coordinator := network.NewControlPlaneCoordinator()
	coordinator.UpdateMappings(s.GetSatellites(), s.GetGroundStations(), s.simTime)

	// 5. COMPUTATION TASK PROCESSING (CPU Duty Cycle)
	for _, n := range s.all {
		comp := n.GetComputing()
		if comp != nil {
			wg.Add(1)
			go func(c types.Computing) {
				defer wg.Done()
				c.Tick(deltaT)
			}(comp)
		}
	}
	wg.Wait()

	// 6. Routing tables update (if enabled)
	if s.config.UsePreRouteCalc {
		for _, n := range s.all {
			wg.Add(1)
			go func(node types.Node) {
				defer wg.Done()
				node.GetRouter().CalculateRoutingTable()
			}(n)
		}
		wg.Wait()
	}

	// 7. Running state plugins
	for _, plugin := range s.statePluginRepo.GetAllPlugins() {
		plugin.PostSimulationStep(s)
	}

	// 8. Simulation plugins run (Orchestrators, Generators, Accumulators)
	for _, plugin := range s.simplugins {
		if err := plugin.PostSimulationStep(s); err != nil {
			log.Printf("Plugin %s PostSimulationStep error: %v", plugin.Name(), err)
		}
	}

	// 9. State serialization (Precomputed Output)
	if s.simulationStateSerializer != nil {
		s.simulationStateSerializer.AddState(s)
	}

	s.running = false
}
