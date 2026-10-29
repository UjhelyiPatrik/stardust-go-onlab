package links

import (
	"errors"
	"math"
	"sync"

	"github.com/polaris-slo-cloud/stardust-go/internal/links/linktypes"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.GroundSatelliteLinkProtocol = (*GroundSatelliteNearestProtocol)(nil)

// GroundSatelliteNearestProtocol maintains a single active link from the ground station
// to the nearest VISIBLE satellite at any given time.
type GroundSatelliteNearestProtocol struct {
	link          *linktypes.GroundLink // Current active ground link
	satellites    []types.Satellite     // Available satellites
	groundStation types.Node            // The ground station node
	mu            sync.RWMutex
}

// NewGroundSatelliteNearestProtocol creates a new protocol with an initial list of satellites.
func NewGroundSatelliteNearestProtocol(satellites []types.Satellite) types.GroundSatelliteLinkProtocol {
	return &GroundSatelliteNearestProtocol{
		satellites: satellites,
	}
}

// Mount binds this protocol to a ground station.
func (p *GroundSatelliteNearestProtocol) Mount(gs types.Node) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.groundStation == nil {
		p.groundStation = gs
	}
}

// AddLink is a no-op for this protocol.
func (p *GroundSatelliteNearestProtocol) AddLink(link types.Link) {}

// ConnectLink is a no-op for this protocol.
func (p *GroundSatelliteNearestProtocol) ConnectLink(link types.Link) error {
	return nil
}

// DisconnectLink is a no-op for this protocol.
func (p *GroundSatelliteNearestProtocol) DisconnectLink(link types.Link) error {
	return nil
}

// UpdateLinks selects the closest satellite that is ABOVE the horizon and sets up the ground link.
// Uses zero-allocation inline horizon dot-product and squared-distance comparisons.
func (p *GroundSatelliteNearestProtocol) UpdateLinks() ([]types.Link, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.groundStation == nil {
		return nil, errors.New("protocol not mounted to ground station")
	}

	if len(p.satellites) == 0 {
		return nil, errors.New("no satellites available")
	}

	gsPos := p.groundStation.GetPosition()
	var nearest types.Satellite
	minDistSq := math.MaxFloat64

	for _, sat := range p.satellites {
		satPos := sat.GetPosition()
		dx := satPos.X - gsPos.X
		dy := satPos.Y - gsPos.Y
		dz := satPos.Z - gsPos.Z

		// Horizon line-of-sight check: dot product of Ground Station Zenith vector and direction vector > 0
		if (gsPos.X*dx)+(gsPos.Y*dy)+(gsPos.Z*dz) <= 0 {
			continue
		}

		distSq := dx*dx + dy*dy + dz*dz
		if distSq < minDistSq {
			minDistSq = distSq
			nearest = sat
		}
	}

	if nearest == nil {
		if p.link != nil {
			_ = p.link.Satellite.GetLinkNodeProtocol().DisconnectLink(p.link)
			p.link = nil
		}
		return nil, errors.New("no visible satellites above the horizon")
	}

	if p.link == nil || p.link.Satellite != nearest {
		old := p.link
		p.link = linktypes.NewGroundLink(p.groundStation, nearest)

		_ = nearest.GetLinkNodeProtocol().ConnectLink(p.link)

		if old != nil {
			_ = old.Satellite.GetLinkNodeProtocol().DisconnectLink(old)
		}
	}

	return []types.Link{p.link}, nil
}

// Links returns the current active link if any.
func (p *GroundSatelliteNearestProtocol) Links() []types.Link {
	p.mu.RLock()
	l := p.link
	p.mu.RUnlock()
	if l != nil {
		return []types.Link{l}
	}
	return nil
}

// Established returns the current active link if any.
func (p *GroundSatelliteNearestProtocol) Established() []types.Link {
	return p.Links()
}

// Link returns the currently active GroundLink.
func (p *GroundSatelliteNearestProtocol) Link() types.Link {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.link
}

// AddSatellite adds a satellite to the trackable list.
func (p *GroundSatelliteNearestProtocol) AddSatellite(sat types.Node) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if satellite, ok := sat.(types.Satellite); ok {
		p.satellites = append(p.satellites, satellite)
	}
}

// RemoveSatellite removes a satellite from the list and resets the link if needed.
func (p *GroundSatelliteNearestProtocol) RemoveSatellite(toRemove types.Node) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if satellite, ok := toRemove.(types.Satellite); ok {
		filtered := make([]types.Satellite, 0, len(p.satellites))
		for _, s := range p.satellites {
			if s.GetName() != satellite.GetName() {
				filtered = append(filtered, s)
			}
		}
		p.satellites = filtered

		if p.link != nil && p.link.Satellite.GetName() == satellite.GetName() {
			_ = satellite.GetLinkNodeProtocol().DisconnectLink(p.link)
			p.link = nil
		}
	}
}
