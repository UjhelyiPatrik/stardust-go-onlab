package simulation

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

type BaseSimulationService struct {
	config *configs.SimulationConfig

	all         []types.Node
	satellites  []types.Satellite
	groundNodes []types.GroundStation

	stepCount     int
	maxStepCount  int
	autorun       bool
	precomputed   bool
	startTime     time.Time
	simTime       time.Time
	stepStartTime time.Time

	lock              sync.Mutex
	runSimulationStep func(func(time.Time) time.Time)
}

func NewBaseSimulationService(config *configs.SimulationConfig, runSimulationStep func(func(time.Time) time.Time)) BaseSimulationService {
	return BaseSimulationService{
		config:            config,
		all:               []types.Node{},
		satellites:        []types.Satellite{},
		groundNodes:       []types.GroundStation{},
		stepCount:         0,
		maxStepCount:      config.StepCount,
		startTime:         config.SimulationStartTime,
		simTime:           config.SimulationStartTime,
		runSimulationStep: runSimulationStep,
	}
}

// InjectSatellites adds the loaded satellites to the simulation scope
func (s *BaseSimulationService) InjectSatellites(satellites []types.Node) error {
	s.satellites = make([]types.Satellite, len(satellites))
	for i, n := range satellites {
		sat, ok := n.(types.Satellite)
		if !ok {
			return fmt.Errorf("InjectSatellites: expected types.Satellite but got %T", n)
		}
		s.satellites[i] = sat
		s.all = append(s.all, sat)
	}

	log.Printf("Injected %d satellites into simulation", len(s.satellites))
	return nil
}

// InjectGroundStations adds the loaded ground stations to the simulation scope
func (s *BaseSimulationService) InjectGroundStations(groundStations []types.Node) error {
	s.groundNodes = make([]types.GroundStation, len(groundStations))
	for i, n := range groundStations {
		gs, ok := n.(types.GroundStation)
		if !ok {
			return fmt.Errorf("InjectGroundStations: expected types.GroundStation but got %T", n)
		}
		s.groundNodes[i] = gs
		s.all = append(s.all, gs)
	}

	log.Printf("Injected %d ground stations into simulation", len(s.groundNodes))
	return nil
}

// StartAutorun begins the simulation loop in autorun mode
func (s *BaseSimulationService) StartAutorun() <-chan struct{} {
	s.lock.Lock()
	if s.autorun {
		s.lock.Unlock()
		done := make(chan struct{})
		close(done)
		return done
	}
	s.autorun = true
	s.lock.Unlock()

	done := make(chan struct{})
	go func() {
		for {
			if !s.autorun || s.stepCount >= s.maxStepCount {
				break
			}

			s.runSimulationStep(func(prev time.Time) time.Time {
				return prev.Add(time.Duration(s.config.StepMultiplier) * time.Second)
			})
		}
		close(done)
	}()

	return done
}

func (s *BaseSimulationService) GetAllNodes() []types.Node {
	return s.all
}

func (s *BaseSimulationService) GetSatellites() []types.Satellite {
	return s.satellites
}

func (s *BaseSimulationService) GetGroundStations() []types.GroundStation {
	return s.groundNodes
}

func (s *BaseSimulationService) GetSimulationTime() time.Time {
	return s.simTime
}

func (s *BaseSimulationService) GetSimulationStartTime() time.Time {
	return s.startTime
}

func (s *BaseSimulationService) GetCurrentStep() int {
	return s.stepCount
}

func (s *BaseSimulationService) GetMaxStepCount() int {
	return s.maxStepCount
}

func (s *BaseSimulationService) GetStepWallDuration() time.Duration {
	if s.stepStartTime.IsZero() {
		return 0
	}
	return time.Since(s.stepStartTime)
}

func (s *BaseSimulationService) IsPrecomputed() bool {
	return s.precomputed
}

// StopAutorun disables autorun mode
func (s *BaseSimulationService) StopAutorun() {
	s.autorun = false
}

// StepBySeconds executes a single step manually (e.g. UI trigger)
func (s *BaseSimulationService) StepBySeconds(seconds float64) {
	s.runSimulationStep(func(prev time.Time) time.Time {
		return prev.Add(time.Duration(seconds * float64(time.Second)))
	})
}

// StepByTime executes a single step manually (e.g. UI trigger)
func (s *BaseSimulationService) StepByTime(newTime time.Time) {
	s.runSimulationStep(func(prev time.Time) time.Time {
		return newTime
	})
}

func (s *BaseSimulationService) setSimulationTime(time time.Time) {
	s.simTime = time
}
