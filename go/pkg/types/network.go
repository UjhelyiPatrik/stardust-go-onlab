package types

// NetworkService handles the end-to-end transmission of payloads
// between nodes and manages network-level accounting.
type NetworkService interface {
	// Transmit simulates sending a payload from src to dst.
	// It calculates the route, registers traffic on links, and returns latency.
	Transmit(src, dst Node, payload Payload) (int, error)
}
