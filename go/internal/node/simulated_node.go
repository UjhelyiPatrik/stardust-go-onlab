package node

import (
	"time"

	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ PrecomputedNode = (*PrecomputedSatellite)(nil)
var _ PrecomputedNode = (*PrecomputedGroundStation)(nil)

type PrecomputedNode interface {
	types.Node
	AddPositionState(time time.Time, position types.Vector)
}
