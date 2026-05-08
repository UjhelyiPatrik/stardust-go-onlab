package types

// GroundStation represents a ground station node
type GroundStation interface {
	Node

	// SetVisibleSatellites assigns the list of satellites currently under this GS's control.
	SetVisibleSatellites(sats []Satellite)

	// GetVisibleSatellites returns the list of satellites currently overseen by this GS.
	GetVisibleSatellites() []Satellite
}
