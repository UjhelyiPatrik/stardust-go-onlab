package linktypes

import (
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.Link = (*GroundLink)(nil)

type GroundLink struct {
	GroundStation types.Node
	Satellite     types.Node
}

// NewGroundLink constructs a link between a ground station and a satellite.
func NewGroundLink(gs types.Node, sat types.Node) *GroundLink {
	return &GroundLink{
		GroundStation: gs,
		Satellite:     sat,
	}
}

// Distance returns the distance in meters between the ground station and satellite.
func (gl *GroundLink) Distance() float64 {
	return gl.GroundStation.DistanceTo(gl.Satellite)
}

// Latency returns the one-way latency in milliseconds.
func (gl *GroundLink) Latency() float64 {
	return gl.Distance() / linkSpeed * 1000
}

// Bandwidth returns the link bandwidth in bits per second.
func (gl *GroundLink) Bandwidth() float64 {
	return 500_000_000 // 500 Mbps
}

// GetOther returns the opposite node of the link.
func (gl *GroundLink) GetOther(self types.Node) types.Node {
	if self.GetName() == gl.Satellite.GetName() {
		return gl.GroundStation
	}
	if self.GetName() == gl.GroundStation.GetName() {
		return gl.Satellite
	}
	return nil
}

// IsReachable checks if the satellite is above the horizon from the ground station's perspective.
// It uses the Dot Product between the Ground Station's normal vector (Zenith) and the direction vector to the satellite.
func (gl *GroundLink) IsReachable() bool {
	gsPos := gl.GroundStation.GetPosition()
	satPos := gl.Satellite.GetPosition()

	// Direction vector pointing from the Ground Station to the Satellite
	dirX := satPos.X - gsPos.X
	dirY := satPos.Y - gsPos.Y
	dirZ := satPos.Z - gsPos.Z

	// The position vector of the ground station (from Earth's center) acts as the Zenith (Up) normal vector.
	// We calculate the dot product of the Zenith vector and the Direction vector.
	dotProduct := (gsPos.X * dirX) + (gsPos.Y * dirY) + (gsPos.Z * dirZ)

	// If the dot product is positive, the angle is < 90 degrees, meaning the satellite is above the mathematical horizon.
	return dotProduct > 0
}

// Nodes returns both nodes participating in the link.
func (gl *GroundLink) Nodes() (types.Node, types.Node) {
	return gl.GroundStation, gl.Satellite
}
