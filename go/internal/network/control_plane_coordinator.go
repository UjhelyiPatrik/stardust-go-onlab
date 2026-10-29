package network

import (
	"math"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/internal/node"
	"github.com/polaris-slo-cloud/stardust-go/pkg/helper"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// ControlPlaneCoordinator manages dynamic mapping between satellites and ground stations.
type ControlPlaneCoordinator struct{}

func NewControlPlaneCoordinator() *ControlPlaneCoordinator {
	return &ControlPlaneCoordinator{}
}

// UpdateMappings evaluates the physical topology and assigns logical control plane ownership.
// In Precomputed Mode, if step buffers are active, mappings are already resolved in O(1) via AdvanceToStep.
// In Live Mode, distance evaluations across all satellites are executed concurrently via ParallelFor.
func (c *ControlPlaneCoordinator) UpdateMappings(sats []types.Satellite, gss []types.GroundStation, simTime time.Time) {
	if len(sats) == 0 || len(gss) == 0 {
		return
	}

	// Fast path: if running in precomputed mode with O(1) step buffers already populated, return immediately.
	if pSat, ok := sats[0].(*node.PrecomputedSatellite); ok {
		if _, hasStepState := pSat.GetCurrentPrecomputedState(); hasStepState && pSat.GetCoordinatingGS() != nil {
			return
		}
	}

	// Build a fast lookup map for ground stations by Name (O(1) retrieval)
	gsMap := make(map[string]types.GroundStation, len(gss))
	for _, gs := range gss {
		gsMap[gs.GetName()] = gs
	}

	// Parallel evaluation: each satellite finds its coordinating Ground Station concurrently
	assignedGS := make([]types.GroundStation, len(sats))
	satIndices := make([]int, len(sats))
	for i := range sats {
		satIndices[i] = i
	}

	helper.ParallelFor(satIndices, func(i int) {
		sat := sats[i]
		var nearestGS types.GroundStation

		// 1. Precomputed Mode Resolution (Fallback by Name)
		if pSat, ok := sat.(*node.PrecomputedSatellite); ok {
			precomputedGsName := pSat.GetPrecomputedGSName(simTime)
			if precomputedGsName != "" {
				if gs, exists := gsMap[precomputedGsName]; exists {
					nearestGS = gs
				}
			}
		}

		// 2. Simulated Mode (Geometric Distance Calculation)
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

		sat.SetCoordinatingGS(nearestGS)
		assignedGS[i] = nearestGS
	})

	// Aggregate visible satellites per Ground Station (deterministic order)
	visibilityMap := make(map[string][]types.Satellite, len(gss))
	for _, gs := range gss {
		visibilityMap[gs.GetName()] = make([]types.Satellite, 0, len(sats)/len(gss)+8)
	}

	for i, sat := range sats {
		if gs := assignedGS[i]; gs != nil {
			name := gs.GetName()
			visibilityMap[name] = append(visibilityMap[name], sat)
		}
	}

	// Apply the aggregated satellite lists to the Ground Stations
	for _, gs := range gss {
		gs.SetVisibleSatellites(visibilityMap[gs.GetName()])
	}
}
