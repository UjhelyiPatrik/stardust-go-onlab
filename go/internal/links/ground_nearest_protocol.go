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
	mu            sync.Mutex
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
func (p *GroundSatelliteNearestProtocol) UpdateLinks() ([]types.Link, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.groundStation == nil {
		return nil, errors.New("protocol not mounted to ground station")
	}

	if len(p.satellites) == 0 {
		return nil, errors.New("no satellites available")
	}

	var nearest types.Satellite
	minDist := math.MaxFloat64

	// Iterate through all satellites to find the closest one that is actually visible
	for _, sat := range p.satellites {
		// Create a temporary link purely to evaluate reachability (Line of Sight)
		tempLink := linktypes.NewGroundLink(p.groundStation, sat)

		if !tempLink.IsReachable() {
			continue // Satellite is obstructed by the Earth (below horizon)
		}

		dist := p.groundStation.DistanceTo(sat)
		if dist < minDist {
			minDist = dist
			nearest = sat
		}
	}

	// If no satellites are currently visible in the sky (e.g., sparse constellation)
	if nearest == nil {
		if p.link != nil {
			// Disconnect the existing link because the satellite disappeared below the horizon
			p.link.Satellite.GetLinkNodeProtocol().DisconnectLink(p.link)
			p.link = nil
		}
		return nil, errors.New("no visible satellites above the horizon")
	}

	// If we found a valid nearest satellite and it requires a link update
	if p.link == nil || p.link.Satellite.GetName() != nearest.GetName() {
		old := p.link
		p.link = linktypes.NewGroundLink(p.groundStation, nearest)

		// Connect new link to the satellite's ISL protocol module
		nearest.GetLinkNodeProtocol().ConnectLink(p.link)

		// Clean up the connection to the previous satellite
		if old != nil {
			old.Satellite.GetLinkNodeProtocol().DisconnectLink(old)
		}
	}

	return []types.Link{p.link}, nil
}

// Links returns the current active link if any.
func (p *GroundSatelliteNearestProtocol) Links() []types.Link {
	if p.link != nil {
		return []types.Link{p.link}
	}
	return nil
}

// Established returns the current active link if any.
func (p *GroundSatelliteNearestProtocol) Established() []types.Link {
	return p.Links()
}

// Link returns the currently active GroundLink.
func (p *GroundSatelliteNearestProtocol) Link() types.Link {
	p.mu.Lock()
	defer p.mu.Unlock()
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
		// Filter out the satellite
		filtered := make([]types.Satellite, 0, len(p.satellites))
		for _, s := range p.satellites {
			if s.GetName() != satellite.GetName() {
				filtered = append(filtered, s)
			}
		}
		p.satellites = filtered

		// Remove the link if it was pointing to the removed satellite
		if p.link != nil && p.link.Satellite.GetName() == satellite.GetName() {
			satellite.GetLinkNodeProtocol().DisconnectLink(p.link)
			p.link = nil
		}
	}
}
