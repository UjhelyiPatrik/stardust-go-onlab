package node

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.GroundStation = (*PrecomputedGroundStation)(nil)

type PrecomputedGroundStation struct {
	BaseNode

	GSIndex      int
	LinkProtocol types.LinkNodeProtocol
	positions    map[time.Time]types.Vector

	// Flat O(1) precomputed state arrays indexed by stepIdx
	stepStates      []types.PrecomputedGSState
	stepVisibleSats [][]types.Satellite
	currentStep     int32

	// Control Plane fields
	visibleSatellites     []types.Satellite      // Pointers to currently overseen satellites
	visibleSatelliteNames map[time.Time][]string // Precomputed satellite names over time

	taskQueue []types.DeployableService // Local Task Queue
	mu        sync.Mutex
}

func NewPrecomputedGroundStation(name string, router types.Router, computing types.Computing, linkProtocol types.LinkNodeProtocol) *PrecomputedGroundStation {
	groundStation := &PrecomputedGroundStation{
		BaseNode:              BaseNode{Name: name, Router: router, Computing: computing},
		LinkProtocol:          linkProtocol,
		positions:             make(map[time.Time]types.Vector),
		visibleSatelliteNames: make(map[time.Time][]string),
		taskQueue:             make([]types.DeployableService, 0),
		currentStep:           -1,
	}

	router.Mount(groundStation)
	computing.Mount(groundStation)
	linkProtocol.Mount(groundStation)
	return groundStation
}

// InitStepBuffers pre-allocates flat slices for totalSteps to enable lock-free O(1) step transitions.
func (s *PrecomputedGroundStation) InitStepBuffers(gsIndex int, totalSteps int) {
	s.GSIndex = gsIndex
	s.stepStates = make([]types.PrecomputedGSState, totalSteps)
	s.stepVisibleSats = make([][]types.Satellite, totalSteps)
}

// GetStepVisibleSatsBuffer returns a reusable slice buffer for stepIdx to avoid allocations across chunks.
func (s *PrecomputedGroundStation) GetStepVisibleSatsBuffer(stepIdx int, capHint int) []types.Satellite {
	if stepIdx >= 0 && stepIdx < len(s.stepVisibleSats) {
		buf := s.stepVisibleSats[stepIdx]
		if cap(buf) >= capHint {
			return buf[:0]
		}
	}
	return make([]types.Satellite, 0, capHint)
}

// SetStepState sets the precomputed state and resolved visible satellite slice for stepIdx.
func (s *PrecomputedGroundStation) SetStepState(stepIdx int, state types.PrecomputedGSState, visibleSats []types.Satellite) {
	if stepIdx >= 0 && stepIdx < len(s.stepStates) {
		s.stepStates[stepIdx] = state
		s.stepVisibleSats[stepIdx] = visibleSats
	}
}

// AdvanceToStep updates position, visible satellites, and active links in O(1) using the precomputed step index.
func (s *PrecomputedGroundStation) AdvanceToStep(stepIdx int) {
	atomic.StoreInt32(&s.currentStep, int32(stepIdx))
	if stepIdx >= 0 && stepIdx < len(s.stepStates) {
		s.Position = s.stepStates[stepIdx].Position
		if s.stepVisibleSats[stepIdx] != nil {
			s.visibleSatellites = s.stepVisibleSats[stepIdx]
		}
	}
	if stepper, ok := s.LinkProtocol.(interface{ SetCurrentStep(int) }); ok {
		stepper.SetCurrentStep(stepIdx)
	}
}

// GetCurrentPrecomputedState returns the PrecomputedGSState for the active simulation step.
func (s *PrecomputedGroundStation) GetCurrentPrecomputedState() (types.PrecomputedGSState, bool) {
	step := int(atomic.LoadInt32(&s.currentStep))
	if step >= 0 && step < len(s.stepStates) {
		return s.stepStates[step], true
	}
	return types.PrecomputedGSState{}, false
}

func (s *PrecomputedGroundStation) UpdatePosition(time time.Time) {
	step := int(atomic.LoadInt32(&s.currentStep))
	if step >= 0 && step < len(s.stepStates) {
		s.Position = s.stepStates[step].Position
		return
	}
	if pos, exists := s.positions[time]; exists {
		s.Position = pos
	}
}

func (s *PrecomputedGroundStation) GetLinkNodeProtocol() types.LinkNodeProtocol {
	return s.LinkProtocol
}

func (s *PrecomputedGroundStation) AddPositionState(time time.Time, position types.Vector) {
	s.positions[time] = position
}

// SetVisibleSatellites updates the local registry of overseen satellites.
func (s *PrecomputedGroundStation) SetVisibleSatellites(sats []types.Satellite) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.visibleSatellites = sats
}

// GetVisibleSatellites returns the overseen satellites.
func (s *PrecomputedGroundStation) GetVisibleSatellites() []types.Satellite {
	step := int(atomic.LoadInt32(&s.currentStep))
	if step >= 0 && step < len(s.stepVisibleSats) && s.stepVisibleSats[step] != nil {
		return s.stepVisibleSats[step]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.visibleSatellites
}

// AddVisibleSatelliteNames stores the precomputed visible satellites for a specific simulation tick.
func (s *PrecomputedGroundStation) AddVisibleSatelliteNames(time time.Time, names []string) {
	s.visibleSatelliteNames[time] = names
}

// GetPrecomputedVisibleSatelliteNames retrieves the visible satellites for the given tick.
func (s *PrecomputedGroundStation) GetPrecomputedVisibleSatelliteNames(time time.Time) []string {
	step := int(atomic.LoadInt32(&s.currentStep))
	if step >= 0 && step < len(s.stepStates) && len(s.stepStates[step].VisibleSatNames) > 0 {
		return s.stepStates[step].VisibleSatNames
	}
	return s.visibleSatelliteNames[time]
}

// EnqueueTask adds a new task to the local queue in a thread-safe manner.
func (gs *PrecomputedGroundStation) EnqueueTask(task types.DeployableService) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.taskQueue = append(gs.taskQueue, task)
}

// GetTaskQueue returns a copy of pending tasks to prevent race conditions during orchestration.
func (gs *PrecomputedGroundStation) GetTaskQueue() []types.DeployableService {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	queueCopy := make([]types.DeployableService, len(gs.taskQueue))
	copy(queueCopy, gs.taskQueue)
	return queueCopy
}

// ClearTaskQueue empties the local task queue while retaining backing slice capacity.
func (gs *PrecomputedGroundStation) ClearTaskQueue() {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.taskQueue = gs.taskQueue[:0]
}

// GetTaskQueueLen returns the number of pending tasks without allocating a slice copy.
func (gs *PrecomputedGroundStation) GetTaskQueueLen() int {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	return len(gs.taskQueue)
}

// PeekTaskQueue returns the underlying task slice directly (for single-owner tick orchestration).
func (gs *PrecomputedGroundStation) PeekTaskQueue() []types.DeployableService {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	return gs.taskQueue
}

// SetTaskQueue updates the task queue slice in-place.
func (gs *PrecomputedGroundStation) SetTaskQueue(tasks []types.DeployableService) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.taskQueue = tasks
}
