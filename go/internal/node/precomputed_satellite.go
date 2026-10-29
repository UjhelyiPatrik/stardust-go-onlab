package node

import (
	"sync/atomic"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.Satellite = (*PrecomputedSatellite)(nil)

type PrecomputedSatellite struct {
	BaseNode

	SatIndex    int
	ISLProtocol types.InterSatelliteLinkProtocol
	positions   map[time.Time]types.Vector

	// Flat O(1) precomputed state arrays indexed by stepIdx
	stepStates         []types.PrecomputedSatState
	stepCoordinatingGS []types.GroundStation
	currentStep        int32

	// Control Plane fields
	coordinatingGS      types.GroundStation  // Logical pointer to the overseeing GS
	coordinatingGSNames map[time.Time]string // Precomputed GS names over time
}

func NewPrecomputedSatellite(name string, router types.Router, computing types.Computing, isl types.InterSatelliteLinkProtocol) *PrecomputedSatellite {
	satellite := &PrecomputedSatellite{
		BaseNode:            BaseNode{Name: name, Router: router, Computing: computing},
		ISLProtocol:         isl,
		positions:           make(map[time.Time]types.Vector),
		coordinatingGSNames: make(map[time.Time]string),
		currentStep:         -1,
	}

	isl.Mount(satellite)
	router.Mount(satellite)
	computing.Mount(satellite)
	return satellite
}

// InitStepBuffers pre-allocates flat slices for totalSteps to enable lock-free O(1) step transitions.
func (s *PrecomputedSatellite) InitStepBuffers(satIndex int, totalSteps int) {
	s.SatIndex = satIndex
	s.stepStates = make([]types.PrecomputedSatState, totalSteps)
	s.stepCoordinatingGS = make([]types.GroundStation, totalSteps)
}

// SetStepState sets the full precomputed state and resolved coordinating GS for stepIdx.
func (s *PrecomputedSatellite) SetStepState(stepIdx int, state types.PrecomputedSatState, coordGS types.GroundStation) {
	if stepIdx >= 0 && stepIdx < len(s.stepStates) {
		s.stepStates[stepIdx] = state
		s.stepCoordinatingGS[stepIdx] = coordGS
	}
}

// AdvanceToStep updates position and control plane pointers in O(1) using the precomputed step index.
func (s *PrecomputedSatellite) AdvanceToStep(stepIdx int) {
	atomic.StoreInt32(&s.currentStep, int32(stepIdx))
	if stepIdx >= 0 && stepIdx < len(s.stepStates) {
		s.Position = s.stepStates[stepIdx].Position
		if s.stepCoordinatingGS[stepIdx] != nil {
			s.coordinatingGS = s.stepCoordinatingGS[stepIdx]
		}
	}
	if stepper, ok := s.ISLProtocol.(interface{ SetCurrentStep(int) }); ok {
		stepper.SetCurrentStep(stepIdx)
	}
}

// GetCurrentPrecomputedState returns the full PrecomputedSatState for the active simulation step.
func (s *PrecomputedSatellite) GetCurrentPrecomputedState() (types.PrecomputedSatState, bool) {
	step := int(atomic.LoadInt32(&s.currentStep))
	if step >= 0 && step < len(s.stepStates) {
		return s.stepStates[step], true
	}
	return types.PrecomputedSatState{}, false
}

// GetCoordinatingGSDistance returns the precomputed distance (m) to the coordinating Ground Station.
func (s *PrecomputedSatellite) GetCoordinatingGSDistance() float64 {
	if st, ok := s.GetCurrentPrecomputedState(); ok && st.CoordinatingGSDistance > 0 {
		return st.CoordinatingGSDistance
	}
	if s.coordinatingGS != nil {
		return s.DistanceTo(s.coordinatingGS)
	}
	return 0
}

// GetCoordinatingGSLatencyMs returns the precomputed one-way communication latency (ms) to the coordinating Ground Station.
func (s *PrecomputedSatellite) GetCoordinatingGSLatencyMs() float64 {
	if st, ok := s.GetCurrentPrecomputedState(); ok && st.CoordinatingGSLatencyMs > 0 {
		return st.CoordinatingGSLatencyMs
	}
	if s.coordinatingGS != nil {
		return s.DistanceTo(s.coordinatingGS) / (types.SpeedOfLight * 0.99) * 1000.0
	}
	return 0
}

// GetGSDistance returns the precomputed distance (m) to a specific Ground Station by index.
func (s *PrecomputedSatellite) GetGSDistance(gsIndex int) float64 {
	if st, ok := s.GetCurrentPrecomputedState(); ok && gsIndex >= 0 && gsIndex < len(st.GSDistances) {
		return st.GSDistances[gsIndex]
	}
	return 0
}

// GetGSLatencyMs returns the precomputed one-way communication latency (ms) to a specific Ground Station by index.
func (s *PrecomputedSatellite) GetGSLatencyMs(gsIndex int) float64 {
	if st, ok := s.GetCurrentPrecomputedState(); ok && gsIndex >= 0 && gsIndex < len(st.GSLatenciesMs) {
		return st.GSLatenciesMs[gsIndex]
	}
	return 0
}

func (s *PrecomputedSatellite) UpdatePosition(simTime time.Time) {
	step := int(atomic.LoadInt32(&s.currentStep))
	if step >= 0 && step < len(s.stepStates) {
		s.Position = s.stepStates[step].Position
		return
	}
	if pos, exists := s.positions[simTime]; exists {
		s.Position = pos
	}
}

func (s *PrecomputedSatellite) GetLinkNodeProtocol() types.LinkNodeProtocol {
	return s.ISLProtocol
}

func (s *PrecomputedSatellite) GetISLProtocol() types.InterSatelliteLinkProtocol {
	return s.ISLProtocol
}

func (s *PrecomputedSatellite) AddPositionState(time time.Time, position types.Vector) {
	s.positions[time] = position
}

// SetCoordinatingGS sets the logical controller for this satellite.
func (s *PrecomputedSatellite) SetCoordinatingGS(gs types.GroundStation) {
	s.coordinatingGS = gs
}

// GetCoordinatingGS returns the logical controller.
func (s *PrecomputedSatellite) GetCoordinatingGS() types.GroundStation {
	return s.coordinatingGS
}

// AddCoordinatingGSName stores the precomputed overseeing Ground Station's name for a specific simulation tick.
func (s *PrecomputedSatellite) AddCoordinatingGSName(time time.Time, gsName string) {
	s.coordinatingGSNames[time] = gsName
}

// GetPrecomputedGSName retrieves the overseeing Ground Station's name for the given tick.
func (s *PrecomputedSatellite) GetPrecomputedGSName(time time.Time) string {
	step := int(atomic.LoadInt32(&s.currentStep))
	if step >= 0 && step < len(s.stepStates) && s.stepStates[step].CoordinatingGSName != "" {
		return s.stepStates[step].CoordinatingGSName
	}
	return s.coordinatingGSNames[time]
}
