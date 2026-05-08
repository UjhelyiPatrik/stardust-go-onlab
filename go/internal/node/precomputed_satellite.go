package node

import (
	"time"

	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.Satellite = (*PrecomputedSatellite)(nil)

type PrecomputedSatellite struct {
	BaseNode

	ISLProtocol types.InterSatelliteLinkProtocol
	positions   map[time.Time]types.Vector

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
	}

	isl.Mount(satellite)
	router.Mount(satellite)
	computing.Mount(satellite)
	return satellite
}

func (s *PrecomputedSatellite) UpdatePosition(simTime time.Time) {
	s.Position = s.positions[simTime]
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
	return s.coordinatingGSNames[time]
}
