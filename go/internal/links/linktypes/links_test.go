package linktypes_test

import (
	"math"
	"testing"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/internal/links/linktypes"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// mockNode implements types.Node for link tests
type mockNode struct {
	name     string
	position types.Vector
}

func (m *mockNode) GetName() string                          { return m.name }
func (m *mockNode) GetPosition() types.Vector                { return m.position }
func (m *mockNode) DistanceTo(other types.Node) float64 {
	dx := other.GetPosition().X - m.position.X
	dy := other.GetPosition().Y - m.position.Y
	dz := other.GetPosition().Z - m.position.Z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}
func (m *mockNode) GetRouter() types.Router                  { return nil }
func (m *mockNode) GetComputing() types.Computing            { return nil }
func (m *mockNode) GetLinkNodeProtocol() types.LinkNodeProtocol { return nil }
func (m *mockNode) UpdatePosition(simTime time.Time)         {}

// =============================================================================
// IslLink Calculation Tests
// =============================================================================

func TestIslLinkDistanceAndLatency(t *testing.T) {
	n1 := &mockNode{name: "Sat1", position: types.NewVector(7000000, 0, 0)}
	n2 := &mockNode{name: "Sat2", position: types.NewVector(7000000, 1000000, 0)}

	link := linktypes.NewIslLink(n1, n2)

	expectedDist := 1000000.0 // 1,000 km
	actualDist := link.Distance()
	if math.Abs(actualDist-expectedDist) > 1e-6 {
		t.Errorf("IslLink.Distance(): expected %f, got %f", expectedDist, actualDist)
	}

	// linkSpeed = configs.SpeedOfLight * 0.99
	expectedSpeed := configs.SpeedOfLight * 0.99
	expectedLatencyMs := (expectedDist / expectedSpeed) * 1000.0
	actualLatency := link.Latency()
	if math.Abs(actualLatency-expectedLatencyMs) > 1e-6 {
		t.Errorf("IslLink.Latency(): expected %f ms, got %f ms", expectedLatencyMs, actualLatency)
	}

	expectedBw := 200_000_000_000.0 // 200 Gbps
	if link.Bandwidth() != expectedBw {
		t.Errorf("IslLink.Bandwidth(): expected %f, got %f", expectedBw, link.Bandwidth())
	}
}

func TestIslLinkReachabilityLineOfSight(t *testing.T) {
	rLEO := float64(configs.EarthRadius + 550000) // ~6928 km orbital radius
	rEarthThreshold := float64(configs.EarthRadius + 10000)

	t.Run("Satellites on same side of Earth have clear LOS", func(t *testing.T) {
		// Satellites separated by 30 degrees
		angle := types.DegreesToRadians(30.0)
		n1 := &mockNode{name: "Sat1", position: types.NewVector(rLEO, 0, 0)}
		n2 := &mockNode{name: "Sat2", position: types.NewVector(rLEO*math.Cos(angle), rLEO*math.Sin(angle), 0)}

		link := linktypes.NewIslLink(n1, n2)
		if !link.IsReachable() {
			t.Errorf("expected link between satellites with 30 deg separation to be reachable")
		}
	})

	t.Run("Satellites on opposite sides of Earth are blocked by Earth", func(t *testing.T) {
		// Sat1 at +X, Sat2 at -X (line passes straight through center of Earth, distance = 0)
		n1 := &mockNode{name: "Sat1", position: types.NewVector(rLEO, 0, 0)}
		n2 := &mockNode{name: "Sat2", position: types.NewVector(-rLEO, 0, 0)}

		link := linktypes.NewIslLink(n1, n2)
		if link.IsReachable() {
			t.Errorf("expected link through center of Earth to NOT be reachable")
		}
	})

	t.Run("Grazing line of sight threshold calculation", func(t *testing.T) {
		// Satellites symmetric across Y axis at X = +d and X = -d, with Y = y0.
		// The closest approach to Earth center (0,0) is y0.
		// If y0 > rEarthThreshold -> reachable
		// If y0 < rEarthThreshold -> blocked

		yAbove := rEarthThreshold + 5000.0 // 5 km above threshold
		satA1 := &mockNode{name: "A1", position: types.NewVector(2000000, yAbove, 0)}
		satA2 := &mockNode{name: "A2", position: types.NewVector(-2000000, yAbove, 0)}
		linkAbove := linktypes.NewIslLink(satA1, satA2)
		if !linkAbove.IsReachable() {
			t.Errorf("expected grazing link above threshold to be reachable")
		}

		yBelow := rEarthThreshold - 5000.0 // 5 km below threshold
		satB1 := &mockNode{name: "B1", position: types.NewVector(2000000, yBelow, 0)}
		satB2 := &mockNode{name: "B2", position: types.NewVector(-2000000, yBelow, 0)}
		linkBelow := linktypes.NewIslLink(satB1, satB2)
		if linkBelow.IsReachable() {
			t.Errorf("expected grazing link below threshold to be blocked")
		}
	})
}

func TestIslLinkGetOtherAndInvolves(t *testing.T) {
	n1 := &mockNode{name: "Sat1"}
	n2 := &mockNode{name: "Sat2"}
	n3 := &mockNode{name: "Sat3"}

	link := linktypes.NewIslLink(n1, n2)

	if link.GetOther(n1) != n2 {
		t.Errorf("expected GetOther(n1) to be n2")
	}
	if link.GetOther(n2) != n1 {
		t.Errorf("expected GetOther(n2) to be n1")
	}
	if link.GetOther(n3) != nil {
		t.Errorf("expected GetOther(n3) to be nil")
	}

	if !link.Involves(n1) || !link.Involves(n2) {
		t.Errorf("expected link to involve n1 and n2")
	}
	if link.Involves(n3) {
		t.Errorf("expected link to not involve n3")
	}
}

// =============================================================================
// GroundLink Calculation Tests
// =============================================================================

func TestGroundLinkDistanceLatencyAndHorizonReachability(t *testing.T) {
	rEarth := float64(configs.EarthRadius)
	gs := &mockNode{name: "GS", position: types.NewVector(rEarth, 0, 0)}

	t.Run("Satellite directly overhead (Zenith) is reachable", func(t *testing.T) {
		// Satellite directly above GS at 550 km altitude
		satOverhead := &mockNode{name: "SatOverhead", position: types.NewVector(rEarth+550000, 0, 0)}
		gl := linktypes.NewGroundLink(gs, satOverhead)

		expectedDist := 550000.0
		if math.Abs(gl.Distance()-expectedDist) > 1e-6 {
			t.Errorf("GroundLink.Distance(): expected %f, got %f", expectedDist, gl.Distance())
		}

		expectedSpeed := configs.SpeedOfLight * 0.99
		expectedLat := (expectedDist / expectedSpeed) * 1000.0
		if math.Abs(gl.Latency()-expectedLat) > 1e-6 {
			t.Errorf("GroundLink.Latency(): expected %f ms, got %f ms", expectedLat, gl.Latency())
		}

		if !gl.IsReachable() {
			t.Errorf("expected satellite directly overhead to be reachable (above horizon)")
		}
	})

	t.Run("Satellite below horizon is NOT reachable", func(t *testing.T) {
		// Ground station is at (+rEarth, 0, 0).
		// Direction pointing into Earth or below horizon (e.g. at (-rEarth - 550000, 0, 0))
		satBelow := &mockNode{name: "SatBelow", position: types.NewVector(-rEarth-550000, 0, 0)}
		gl := linktypes.NewGroundLink(gs, satBelow)

		if gl.IsReachable() {
			t.Errorf("expected satellite on opposite side of Earth to NOT be reachable from GS")
		}
	})

	t.Run("Satellite slightly above vs slightly below horizon", func(t *testing.T) {
		// GS is at (rEarth, 0, 0). Horizon plane tangent at GS is X = rEarth.
		// Above horizon: sat.X > rEarth (dot product gs . (sat - gs) = rEarth * (sat.X - rEarth) > 0)
		// Below horizon: sat.X < rEarth (dot product < 0)
		satAbove := &mockNode{name: "SatAbove", position: types.NewVector(rEarth+100, 1000000, 0)}
		glAbove := linktypes.NewGroundLink(gs, satAbove)
		if !glAbove.IsReachable() {
			t.Errorf("expected sat with sat.X > gs.X to be above horizon")
		}

		satBelow := &mockNode{name: "SatBelow", position: types.NewVector(rEarth-100, 1000000, 0)}
		glBelow := linktypes.NewGroundLink(gs, satBelow)
		if glBelow.IsReachable() {
			t.Errorf("expected sat with sat.X < gs.X to be below horizon")
		}
	})
}

// =============================================================================
// LinkPriorityQueue Min-Heap Sorting Tests
// =============================================================================

func TestLinkPriorityQueue(t *testing.T) {
	pq := linktypes.NewLinkPriorityQueue()
	pq.Init()

	if pq.Len() != 0 {
		t.Errorf("expected initial len 0, got %d", pq.Len())
	}
	if pq.Dequeue() != nil {
		t.Errorf("expected Dequeue on empty queue to return nil")
	}

	n0 := &mockNode{name: "Center", position: types.NewVector(0, 0, 0)}
	s1 := &mockNode{name: "S1", position: types.NewVector(5000, 0, 0)}
	s2 := &mockNode{name: "S2", position: types.NewVector(1000, 0, 0)}
	s3 := &mockNode{name: "S3", position: types.NewVector(3000, 0, 0)}
	s4 := &mockNode{name: "S4", position: types.NewVector(2000, 0, 0)}

	l1 := linktypes.NewIslLink(n0, s1) // dist 5000
	l2 := linktypes.NewIslLink(n0, s2) // dist 1000
	l3 := linktypes.NewIslLink(n0, s3) // dist 3000
	l4 := linktypes.NewIslLink(n0, s4) // dist 2000

	pq.Enqueue(l1, 5000)
	pq.Enqueue(l2, 1000)
	pq.Enqueue(l3, 3000)
	pq.Enqueue(l4, 2000)

	// Duplicate enqueue should be ignored
	pq.Enqueue(l2, 1000)
	if pq.Len() != 4 {
		t.Errorf("expected len 4 after duplicate enqueue, got %d", pq.Len())
	}

	// Dequeue should return in strictly increasing order of priority
	first := pq.Dequeue()
	if first != l2 {
		t.Errorf("expected 1st dequeue to be l2 (dist 1000), got %v", first)
	}

	second := pq.Dequeue()
	if second != l4 {
		t.Errorf("expected 2nd dequeue to be l4 (dist 2000), got %v", second)
	}

	third := pq.Dequeue()
	if third != l3 {
		t.Errorf("expected 3rd dequeue to be l3 (dist 3000), got %v", third)
	}

	fourth := pq.Dequeue()
	if fourth != l1 {
		t.Errorf("expected 4th dequeue to be l1 (dist 5000), got %v", fourth)
	}

	if pq.Len() != 0 {
		t.Errorf("expected empty queue, got len %d", pq.Len())
	}

	// Test Clear
	pq.Enqueue(l1, 5000)
	pq.Clear()
	if pq.Len() != 0 {
		t.Errorf("expected len 0 after Clear(), got %d", pq.Len())
	}
}
