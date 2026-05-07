package routing

import "github.com/polaris-slo-cloud/stardust-go/pkg/types"

type UnreachableRouteResult struct{}

var UnreachableRouteResultInstance = &UnreachableRouteResult{}

func (r *UnreachableRouteResult) Reachable() bool {
	return false
}

func (r *UnreachableRouteResult) Latency() int {
	return -1
}

func (r *UnreachableRouteResult) WaitLatencyAsync() error {
	return nil
}

func (r *UnreachableRouteResult) AddCalculationDuration(ms int) types.RouteResult {
	return r
}

func (r *UnreachableRouteResult) Path() []types.Link {
	return nil
}
