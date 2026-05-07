package types

// RouteResult represents the result of a routing calculation.
type RouteResult interface {
	// Reachable returns true if the target was reachable over a route, otherwise false
	Reachable() bool

	// Latency returns the latency of the calculated route
	Latency() int

	// Path returns the path of the calculated route
	Path() []Link

	// TODO remove
	AddCalculationDuration(duration int) RouteResult

	// TODO remove
	WaitLatencyAsync() error
}
