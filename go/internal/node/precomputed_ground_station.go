package node

import (
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
}

func NewPrecomputedGroundStation(name string, router types.Router, computing types.Computing, linkProtocol types.LinkNodeProtocol) *PrecomputedGroundStation {
	groundStation := &PrecomputedGroundStation{
		BaseNode:              BaseNode{Name: name, Router: router, Computing: computing},
		LinkProtocol:          linkProtocol,
		positions:             make(map[time.Time]types.Vector),
		visibleSatelliteNames: make(map[time.Time][]string),
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
