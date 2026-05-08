package network

import (
	"math"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/internal/node"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// ControlPlaneCoordinator manages dynamic mapping between satellites and ground stations.
type ControlPlaneCoordinator struct{}

func NewControlPlaneCoordinator() *ControlPlaneCoordinator {
	return &ControlPlaneCoordinator{}
}

// UpdateMappings evaluates the physical topology and assigns logical control plane ownership.
// It checks if precomputed mappings exist for the current tick; if so, it resolves them via names.
// Otherwise, it calculates them on the fly based on geometric distances.
func (c *ControlPlaneCoordinator) UpdateMappings(sats []types.Satellite, gss []types.GroundStation, simTime time.Time) {
	// Build a fast lookup map for ground stations by Name (O(1) retrieval)
	gsMap := make(map[string]types.GroundStation)
	for _, gs := range gss {
		gsMap[gs.GetName()] = gs
	}

	// Map to aggregate visible satellites for each GS: GS Name -> List of Satellites
	visibilityMap := make(map[string][]types.Satellite)
	for _, gs := range gss {
		visibilityMap[gs.GetName()] = []types.Satellite{}
	}

	// Process each satellite to find its overseeing Ground Station
	for _, sat := range sats {
		var nearestGS types.GroundStation

		// 1. Precomputed Mode Resolution
		// Check if the node is a PrecomputedSatellite and has data for the current tick.
		if pSat, ok := sat.(*node.PrecomputedSatellite); ok {
			precomputedGsName := pSat.GetPrecomputedGSName(simTime)
			if precomputedGsName != "" {
				if gs, exists := gsMap[precomputedGsName]; exists {
					nearestGS = gs
				}
			}
		}

		// 2. Simulated Mode (Fallback Calculation)
		// If no precomputed data is available, find the closest GS geometrically.
		if nearestGS == nil {
			minDist := math.MaxFloat64
			for _, gs := range gss {
				dist := sat.DistanceTo(gs)
				if dist < minDist {
					minDist = dist
					nearestGS = gs
				}
			}
		}

		// Assign the logical pointer to the satellite
		sat.SetCoordinatingGS(nearestGS)

		// Aggregate back to the assigned Ground Station
		if nearestGS != nil {
			visibilityMap[nearestGS.GetName()] = append(visibilityMap[nearestGS.GetName()], sat)
		}
	}

	// 3. Apply the aggregated satellite lists to the Ground Stations
	for _, gs := range gss {
		gs.SetVisibleSatellites(visibilityMap[gs.GetName()])
	}
}
