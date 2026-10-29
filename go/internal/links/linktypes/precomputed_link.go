package linktypes

import (
	"sync/atomic"

	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.Link = (*PrecomputedLink)(nil)

// PrecomputedLink represents a precomputed link with O(1) lookup for precalculated distance, latency, and bandwidth.
type PrecomputedLink struct {
	Node1   types.Node
	Node2   types.Node
	traffic uint64

	bandwidth      float64
	isGroundLink   bool
	scalarDistance float64
	scalarLatency  float64

	// Flat slice of precomputed distance (m) and latency (ms) indexed by simulation step index.
	stepDistances []float64
	stepLatencies []float64
	currentStep   int32
}

// InitSingleStep initializes a PrecomputedLink in-place for a single window slot without allocating slices.
func (l *PrecomputedLink) InitSingleStep(node1, node2 types.Node, isGroundLink bool, bandwidth, distance, latencyMs float64) {
	if bandwidth <= 0 {
		if isGroundLink {
			bandwidth = 500_000_000 // 500 Mbps GroundLink
		} else {
			bandwidth = 200_000_000_000 // 200 Gbps ISL
		}
	}
	l.Node1 = node1
	l.Node2 = node2
	l.traffic = 0
	l.bandwidth = bandwidth
	l.isGroundLink = isGroundLink
	l.scalarDistance = distance
	l.scalarLatency = latencyMs
	l.currentStep = 0
}

// SetScalarMetrics updates the single-step distance and latency in-place.
func (l *PrecomputedLink) SetScalarMetrics(distance, latencyMs float64) {
	l.scalarDistance = distance
	l.scalarLatency = latencyMs
}

// NewPrecomputedLink creates a new link between precomputed Nodes.
func NewPrecomputedLink(node1 types.Node, node2 types.Node) *PrecomputedLink {
	return &PrecomputedLink{
		Node1:       node1,
		Node2:       node2,
		bandwidth:   200_000_000_000, // Default 200 Gbps ISL
		currentStep: -1,
	}
}

// NewPrecomputedLinkWithMeta creates a precomputed link with preallocated step slices and link metadata.
func NewPrecomputedLinkWithMeta(node1 types.Node, node2 types.Node, isGroundLink bool, bandwidth float64, totalSteps int) *PrecomputedLink {
	if bandwidth <= 0 {
		if isGroundLink {
			bandwidth = 500_000_000 // 500 Mbps GroundLink
		} else {
			bandwidth = 200_000_000_000 // 200 Gbps ISL
		}
	}
	return &PrecomputedLink{
		Node1:         node1,
		Node2:         node2,
		bandwidth:     bandwidth,
		isGroundLink:  isGroundLink,
		stepDistances: make([]float64, totalSteps),
		stepLatencies: make([]float64, totalSteps),
		currentStep:   -1,
	}
}

// SetStepMetrics records the precomputed distance and latency for a given step index.
func (l *PrecomputedLink) SetStepMetrics(stepIdx int, distance float64, latencyMs float64) {
	if stepIdx >= 0 && stepIdx < len(l.stepDistances) {
		l.stepDistances[stepIdx] = distance
		l.stepLatencies[stepIdx] = latencyMs
	}
}

// SetCurrentStep updates the active step index atomically for lock-free O(1) reads.
func (l *PrecomputedLink) SetCurrentStep(stepIdx int) {
	atomic.StoreInt32(&l.currentStep, int32(stepIdx))
}

func (l *PrecomputedLink) Distance() float64 {
	if l.scalarDistance > 0 {
		return l.scalarDistance
	}
	step := int(atomic.LoadInt32(&l.currentStep))
	if step >= 0 && step < len(l.stepDistances) && l.stepDistances[step] > 0 {
		return l.stepDistances[step]
	}
	return l.Node1.DistanceTo(l.Node2)
}

func (l *PrecomputedLink) Latency() float64 {
	if l.scalarLatency > 0 {
		return l.scalarLatency
	}
	step := int(atomic.LoadInt32(&l.currentStep))
	if step >= 0 && step < len(l.stepLatencies) && l.stepLatencies[step] > 0 {
		return l.stepLatencies[step]
	}
	return l.Distance() / linkSpeed * 1000
}

func (l *PrecomputedLink) Bandwidth() float64 {
	if l.bandwidth > 0 {
		return l.bandwidth
	}
	return 200_000_000_000 // 200 Gbps
}

func (l *PrecomputedLink) IsGroundLink() bool {
	return l.isGroundLink
}

func (l *PrecomputedLink) IsReachable() bool {
	if l.scalarDistance > 0 {
		return true
	}
	step := int(atomic.LoadInt32(&l.currentStep))
	if step >= 0 && step < len(l.stepDistances) && l.stepDistances[step] > 0 {
		return true
	}
	if l.isGroundLink {
		gsPos := l.Node1.GetPosition()
		satPos := l.Node2.GetPosition()
		if _, ok := l.Node1.(types.Satellite); ok {
			gsPos, satPos = satPos, gsPos
		}
		dirX := satPos.X - gsPos.X
		dirY := satPos.Y - gsPos.Y
		dirZ := satPos.Z - gsPos.Z
		return (gsPos.X*dirX)+(gsPos.Y*dirY)+(gsPos.Z*dirZ) > 0
	}
	v := l.Node2.GetPosition().Subtract(l.Node1.GetPosition())
	cross := v.Cross(l.Node1.GetPosition())
	d := cross.Magnitude() / v.Magnitude()
	return d > configs.EarthRadius+10_000 // 10 km buffer
}

func (l *PrecomputedLink) GetOther(self types.Node) types.Node {
	if self == l.Node1 || (self != nil && l.Node1 != nil && self.GetName() == l.Node1.GetName()) {
		return l.Node2
	}
	if self == l.Node2 || (self != nil && l.Node2 != nil && self.GetName() == l.Node2.GetName()) {
		return l.Node1
	}
	return nil
}

func (l *PrecomputedLink) Nodes() (types.Node, types.Node) {
	return l.Node1, l.Node2
}

func (l *PrecomputedLink) AddTraffic(bytes uint64) {
	atomic.AddUint64(&l.traffic, bytes)
}

func (l *PrecomputedLink) GetTraffic() uint64 {
	return atomic.LoadUint64(&l.traffic)
}

func (l *PrecomputedLink) ResetTraffic() {
	atomic.StoreUint64(&l.traffic, 0)
}
