package simulation

import (
	"log"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/internal/links/linktypes"
	"github.com/polaris-slo-cloud/stardust-go/internal/network"
	"github.com/polaris-slo-cloud/stardust-go/internal/node"
	"github.com/polaris-slo-cloud/stardust-go/pkg/helper"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.SimulationController = (*SimulationIteratorService)(nil)

type SimulationIteratorService struct {
	BaseSimulationService

	simulationStates      []types.SimulationState
	simPlugins            []types.SimulationPlugin
	statePluginRepository types.StatePluginRepository
	precompSats           []*node.PrecomputedSatellite
	precompGSs            []*node.PrecomputedGroundStation
	precompLinks          []*linktypes.PrecomputedLink
	stepWindowPrepare     func(stepIdx int) (slotIdx int, stepTime time.Time)
	running               bool
	currentIx             int
}

func NewSimulationIteratorService(config *configs.SimulationConfig, simulationStates []types.SimulationState, simPlugins []types.SimulationPlugin, statePluginRepository types.StatePluginRepository) *SimulationIteratorService {
	service := &SimulationIteratorService{
		simulationStates:      simulationStates,
		simPlugins:            simPlugins,
		statePluginRepository: statePluginRepository,
		running:               false,
		currentIx:             -1,
	}
	service.BaseSimulationService = NewBaseSimulationService(config, service.runSimulationStep)
	service.precomputed = true
	if len(simulationStates) > 0 && (service.maxStepCount <= 0 || service.maxStepCount > len(simulationStates)) {
		service.maxStepCount = len(simulationStates)
	}
	return service
}

func (s *SimulationIteratorService) SetPrecomputedEntities(sats []*node.PrecomputedSatellite, gss []*node.PrecomputedGroundStation, links []*linktypes.PrecomputedLink) {
	s.precompSats = sats
	s.precompGSs = gss
	s.precompLinks = links
}

func (s *SimulationIteratorService) SetStepWindowPrepare(totalSteps int, fn func(stepIdx int) (int, time.Time)) {
	s.stepWindowPrepare = fn
	if totalSteps > 0 && (s.maxStepCount <= 0 || s.maxStepCount > totalSteps) {
		s.maxStepCount = totalSteps
	}
}

func (s *SimulationIteratorService) GetStatePluginRepository() *types.StatePluginRepository {
	return &s.statePluginRepository
}

func (s *SimulationIteratorService) Close() {
	for _, p := range s.simPlugins {
		if closer, ok := p.(interface{ Close() }); ok {
			closer.Close()
		}
	}
}

func (s *SimulationIteratorService) runSimulationStep(nextTime func(time.Time) time.Time) {
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

	s.currentIx++
	if s.stepWindowPrepare == nil && s.currentIx >= len(s.simulationStates) {
		s.running = false
		return
	}
	if s.stepWindowPrepare != nil && s.currentIx >= s.maxStepCount {
		s.running = false
		return
	}

	stepIdx := s.currentIx
	s.stepStartTime = time.Now()
	s.stepCount = stepIdx + 1
	oldTime := s.simTime

	slotIdx := stepIdx
	var newTime time.Time
	if s.stepWindowPrepare != nil {
		slotIdx, newTime = s.stepWindowPrepare(stepIdx)
	} else {
		newTime = s.simulationStates[stepIdx].Time
	}
	s.setSimulationTime(newTime)

	deltaT := newTime.Sub(oldTime).Seconds()
	if deltaT <= 0 && s.config != nil && s.config.StepMultiplier > 0 {
		deltaT = float64(s.config.StepMultiplier)
	}

	if len(s.simPlugins) == 0 {
		log.Printf("Simulation time is %s (Precomputed Step %d/%d)", s.simTime.Format(time.RFC3339), s.maxStepCount, s.stepCount)
	}

	// 1. O(1) Parallel Step Advance for all Precomputed Links, Satellites, and Ground Stations
	if len(s.precompLinks) > 0 {
		helper.ParallelFor(s.precompLinks, func(pl *linktypes.PrecomputedLink) {
			pl.SetCurrentStep(slotIdx)
		})
	}

	if len(s.precompSats) > 0 {
		helper.ParallelFor(s.precompSats, func(sat *node.PrecomputedSatellite) {
			sat.AdvanceToStep(slotIdx)
		})
	} else {
		helper.ParallelFor(s.all, func(n types.Node) {
			n.UpdatePosition(s.simTime)
		})
	}

	if len(s.precompGSs) > 0 {
		helper.ParallelFor(s.precompGSs, func(gs *node.PrecomputedGroundStation) {
			gs.AdvanceToStep(slotIdx)
		})
	}

	// 2. Control Plane Update (Instant O(1) if precomputed step buffers are populated)
	coordinator := network.NewControlPlaneCoordinator()
	coordinator.UpdateMappings(s.GetSatellites(), s.GetGroundStations(), s.simTime)

	// 3. Computation Task Processing (CPU Duty Cycle) in parallel across all nodes
	if deltaT > 0 {
		helper.ParallelFor(s.all, func(n types.Node) {
			if comp := n.GetComputing(); comp != nil {
				comp.Tick(deltaT, s.simTime)
			}
		})
	}

	// 4. Routing tables update (if enabled)
	if s.config.UsePreRouteCalc {
		helper.ParallelFor(s.all, func(n types.Node) {
			_ = n.GetRouter().CalculateRoutingTable()
		})
	}

	// 5. Execute post-step state plugins
	for _, plugin := range s.statePluginRepository.GetAllPlugins() {
		plugin.PostSimulationStep(s)
	}

	// 6. Execute post-step simulation plugins (Battery, Thermal, Workload, Orchestrator, Telemetry)
	for _, plugin := range s.simPlugins {
		if err := plugin.PostSimulationStep(s); err != nil {
			log.Printf("Plugin %s PostSimulationStep error: %v", plugin.Name(), err)
		}
	}

	s.running = false
}
