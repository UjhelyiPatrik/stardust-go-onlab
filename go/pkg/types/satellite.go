package types

// Satellite represents a satellite node
type Satellite interface {
	Node

	// GetISLProtocol returns the ISL protocol
	GetISLProtocol() InterSatelliteLinkProtocol

	// SetCoordinatingGS assigns the nearest active ground station to oversee this satellite.
	SetCoordinatingGS(gs GroundStation)

	// GetCoordinatingGS returns the current coordinating ground station.
	GetCoordinatingGS() GroundStation
}
