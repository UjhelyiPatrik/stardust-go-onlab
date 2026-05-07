package routing

import (
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

type PreRouteResult struct {
	latency int
	path    []types.Link
}

// NewPreRouteResult creates a new PreRouteResult with the specified latency
func NewPreRouteResult(latency int, path []types.Link) types.RouteResult {
	if latency < 0 {
		return nil // Return nil if latency is negative
	}
	return &PreRouteResult{latency: latency, path: path}
}

// Reachable returns whether the route is reachable (PreRoute is always reachable)
func (r *PreRouteResult) Reachable() bool {
	return true
}

// Latency returns the calculated latency for the route
func (r *PreRouteResult) Latency() int {
	return r.latency
}

// WaitLatencyAsync simulates waiting for the latency (asynchronous operation)
func (r *PreRouteResult) WaitLatencyAsync() error {
	return delayMilliseconds(r.latency)
}

// AddCalculationDuration adds additional calculation duration to the route and returns the updated result
func (r *PreRouteResult) AddCalculationDuration(calculationDuration int) types.RouteResult {
	return NewOnRouteResult(r.latency, calculationDuration, r.path)
}

// Path returns the path of the calculated route
func (r *PreRouteResult) Path() []types.Link {
	return r.path
}
