package network

import (
	"errors"
	"fmt"

	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

type NetworkService struct{}

func NewNetworkService() *NetworkService {
	return &NetworkService{}
}

// Transmit simulates the actual data movement through the network.
func (s *NetworkService) Transmit(src, dst types.Node, payload types.Payload) (int, error) {
	if src == nil || dst == nil || payload == nil {
		return 0, errors.New("invalid transmission parameters")
	}

	router := src.GetRouter()
	if router == nil {
		return 0, fmt.Errorf("no router found on node %s", src.GetName())
	}

	// 1. Ask for the route (The router only does pathfinding)
	route, err := router.RouteToNode(dst)
	if err != nil || !route.Reachable() {
		return -1, errors.New("destination unreachable")
	}

	// 2. Register traffic on the calculated path (Network accounting)
	path := route.Path()
	payloadSize := payload.SizeBytes()

	if payloadSize > 0 {
		for _, link := range path {
			if link != nil {
				// This call eventually triggers battery consumption in BatterySimPlugin
				link.AddTraffic(payloadSize)
			}
		}
	}

	// 3. Return the calculated latency
	return route.Latency(), nil
}
