package types

// Payload represents a simulation payload interface.
type Payload interface {
	// SizeBytes returns the size of the payload in bytes.
	// This is required to calculate network energy consumption.
	SizeBytes() uint64
}
