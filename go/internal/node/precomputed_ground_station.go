package node

import (
	"sync"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.GroundStation = (*PrecomputedGroundStation)(nil)

type PrecomputedGroundStation struct {
	BaseNode

	LinkProtocol types.LinkNodeProtocol
	positions    map[time.Time]types.Vector

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
	}

	router.Mount(groundStation)
	computing.Mount(groundStation)
	linkProtocol.Mount(groundStation)
	return groundStation
}

func (s *PrecomputedGroundStation) UpdatePosition(time time.Time) {
	s.Position = s.positions[time]
}

func (s *PrecomputedGroundStation) GetLinkNodeProtocol() types.LinkNodeProtocol {
	return s.LinkProtocol
}

func (s *PrecomputedGroundStation) AddPositionState(time time.Time, position types.Vector) {
	s.positions[time] = position
}

// SetVisibleSatellites updates the local registry of overseen satellites.
func (s *PrecomputedGroundStation) SetVisibleSatellites(sats []types.Satellite) {
	s.visibleSatellites = sats
}

// GetVisibleSatellites returns the overseen satellites.
func (s *PrecomputedGroundStation) GetVisibleSatellites() []types.Satellite {
	return s.visibleSatellites
}

// AddVisibleSatelliteNames stores the precomputed visible satellites for a specific simulation tick.
func (s *PrecomputedGroundStation) AddVisibleSatelliteNames(time time.Time, names []string) {
	s.visibleSatelliteNames[time] = names
}

// GetPrecomputedVisibleSatelliteNames retrieves the visible satellites for the given tick.
func (s *PrecomputedGroundStation) GetPrecomputedVisibleSatelliteNames(time time.Time) []string {
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

// ClearTaskQueue empties the local task queue.
func (gs *PrecomputedGroundStation) ClearTaskQueue() {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.taskQueue = make([]types.DeployableService, 0)
}
