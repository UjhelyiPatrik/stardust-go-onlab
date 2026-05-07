package types

// Link represents a generic network link.
type Link interface {
	// Distance returns the link distance in meters.
	Distance() float64

	// Latency returns the link latency in milliseconds.
	Latency() float64

	// Bandwidth returns the bandwidth in bits per second.
	Bandwidth() float64

	// GetOther returns the opposite node from the provided one.
	GetOther(self Node) Node

	// IsReachable returns true if the link is physically/line-of-sight reachable.
	IsReachable() bool

	// Nodes returns the two nodes connected by this link.
	Nodes() (Node, Node)

	// AddTraffic adds transferred bytes to the link's traffic counter.
	AddTraffic(bytes uint64)

	// GetTraffic returns the total transferred bytes in the current step.
	GetTraffic() uint64

	// ResetTraffic clears the traffic counter for the next step.
	ResetTraffic()
}
