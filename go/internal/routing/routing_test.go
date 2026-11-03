package routing

import (
	"math"
	"testing"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// routeMockNode implements types.Node and types.GroundStation
type routeMockNode struct {
	name      string
	pos       types.Vector
	router    types.Router
	computing types.Computing
	protocol  *routeMockProto
}

func newRouteMockNode(name string, x, y, z float64) *routeMockNode {
	n := &routeMockNode{
		name:      name,
		pos:       types.NewVector(x, y, z),
		protocol:  &routeMockProto{},
		computing: &routeMockComputing{serviceNames: make(map[string]bool)},
	}
	return n
}

func (n *routeMockNode) GetName() string                          { return n.name }
func (n *routeMockNode) GetPosition() types.Vector                { return n.pos }
func (n *routeMockNode) DistanceTo(other types.Node) float64 {
	dx := other.GetPosition().X - n.pos.X
	dy := other.GetPosition().Y - n.pos.Y
	dz := other.GetPosition().Z - n.pos.Z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}
func (n *routeMockNode) GetRouter() types.Router                  { return n.router }
func (n *routeMockNode) GetComputing() types.Computing            { return n.computing }
func (n *routeMockNode) GetLinkNodeProtocol() types.LinkNodeProtocol {
	return n.protocol
}
func (n *routeMockNode) UpdatePosition(simTime time.Time) {}

type routeMockProto struct {
	links []types.Link
}

func (p *routeMockProto) Mount(node types.Node)                  {}
func (p *routeMockProto) AddLink(link types.Link)                { p.links = append(p.links, link) }
func (p *routeMockProto) ConnectLink(link types.Link) error       { return nil }
func (p *routeMockProto) DisconnectLink(link types.Link) error    { return nil }
func (p *routeMockProto) UpdateLinks() ([]types.Link, error)     { return p.links, nil }
func (p *routeMockProto) Established() []types.Link              { return p.links }
func (p *routeMockProto) Links() []types.Link                    { return p.links }

type routeMockLink struct {
	n1      types.Node
	n2      types.Node
	latency float64
	dist    float64
}

func newRouteMockLink(n1, n2 types.Node, latencyMs float64) *routeMockLink {
	l := &routeMockLink{
		n1:      n1,
		n2:      n2,
		latency: latencyMs,
		dist:    latencyMs * configs.SpeedOfLight / 1000.0,
	}
	if p, ok := n1.GetLinkNodeProtocol().(*routeMockProto); ok {
		p.AddLink(l)
	}
	if p, ok := n2.GetLinkNodeProtocol().(*routeMockProto); ok {
		p.AddLink(l)
	}
	return l
}

func (l *routeMockLink) Distance() float64                   { return l.dist }
func (l *routeMockLink) Latency() float64                    { return l.latency }
func (l *routeMockLink) Bandwidth() float64                  { return 1e9 }
func (l *routeMockLink) IsReachable() bool                   { return true }
func (l *routeMockLink) GetOther(self types.Node) types.Node {
	if self.GetName() == l.n1.GetName() {
		return l.n2
	}
	if self.GetName() == l.n2.GetName() {
		return l.n1
	}
	return nil
}
func (l *routeMockLink) Involves(node types.Node) bool   { return l.n1 == node || l.n2 == node }
func (l *routeMockLink) Nodes() (types.Node, types.Node) { return l.n1, l.n2 }
func (l *routeMockLink) AddTraffic(bytes uint64)         {}
func (l *routeMockLink) GetTraffic() uint64              { return 0 }
func (l *routeMockLink) ResetTraffic()                   {}

type routeMockComputing struct {
	serviceNames map[string]bool
}

func (m *routeMockComputing) Mount(node types.Node) error                                    { return nil }
func (m *routeMockComputing) TryPlaceDeploymentAsync(s types.DeployableService) (bool, error) { return true, nil }
func (m *routeMockComputing) RemoveDeploymentAsync(s types.DeployableService) error          { return nil }
func (m *routeMockComputing) Tick(deltaT float64, currentTime time.Time)                     {}
func (m *routeMockComputing) CanPlace(s types.DeployableService) bool                        { return true }
func (m *routeMockComputing) HostsService(serviceName string) bool                           { return m.serviceNames[serviceName] }
func (m *routeMockComputing) CpuAvailable() float64                                          { return 0 }
func (m *routeMockComputing) MemoryAvailable() float64                                       { return 0 }
func (m *routeMockComputing) Clone() types.Computing                                         { return m }
func (m *routeMockComputing) GetComputingType() types.ComputingType                          { return types.Edge }
func (m *routeMockComputing) GetCpuUtilization() float64                                     { return 0 }
func (m *routeMockComputing) GetServices() []types.DeployableService                         { return nil }
func (m *routeMockComputing) GetActiveServiceCount() int                                     { return 0 }

// =============================================================================
// Heuristic Tests
// =============================================================================

func TestAStarHeuristic(t *testing.T) {
	nodeA := newRouteMockNode("A", 0, 0, 0)
	nodeB := newRouteMockNode("B", float64(configs.SpeedOfLight), 0, 0) // distance = c meters
	nodeSame := newRouteMockNode("Same", 0, 0, 0)

	// At distance = speed of light (299,792,000 m), latency should be exactly 1000 ms (1 second)
	h := heuristic(nodeA, nodeB)
	expectedH := 1000.0
	if math.Abs(h-expectedH) > 1e-4 {
		t.Errorf("heuristic(): expected %f ms, got %f ms", expectedH, h)
	}

	// At distance = 0, heuristic should be 0 ms
	hZero := heuristic(nodeA, nodeSame)
	if hZero != 0.0 {
		t.Errorf("heuristic() for same position: expected 0, got %f", hZero)
	}
}

// =============================================================================
// Dijkstra Router Path & Latency Calculation Tests
// =============================================================================

func TestDijkstraRouterCalculations(t *testing.T) {
	// Topology:
	// A --(10ms)--> B --(20ms)--> C
	// A -----------(50ms)-------> C (direct but slower)
	// D (disconnected island)

	nodeA := newRouteMockNode("A", 0, 0, 0)
	nodeB := newRouteMockNode("B", 1000, 0, 0)
	nodeC := newRouteMockNode("C", 2000, 0, 0)
	nodeD := newRouteMockNode("D", 5000, 5000, 0)

	nodeC.computing = &routeMockComputing{serviceNames: map[string]bool{"MyService": true}}

	routerA := NewDijkstraRouter()
	nodeA.router = routerA
	_ = routerA.Mount(nodeA)

	routerB := NewDijkstraRouter()
	nodeB.router = routerB
	_ = routerB.Mount(nodeB)

	routerC := NewDijkstraRouter()
	nodeC.router = routerC
	_ = routerC.Mount(nodeC)

	// Create links
	newRouteMockLink(nodeA, nodeB, 10.0) // A-B: 10 ms
	newRouteMockLink(nodeB, nodeC, 20.0) // B-C: 20 ms
	newRouteMockLink(nodeA, nodeC, 50.0) // A-C: 50 ms

	err := routerA.CalculateRoutingTable()
	if err != nil {
		t.Fatalf("CalculateRoutingTable failed: %v", err)
	}

	t.Run("Shortest path chosen over direct slower path", func(t *testing.T) {
		route, err := routerA.RouteToNode(nodeC)
		if err != nil {
			t.Fatalf("RouteToNode(C) error: %v", err)
		}
		if !route.Reachable() {
			t.Fatalf("expected node C to be reachable")
		}

		// Expected shortest path is A -> B -> C with latency 10 + 20 = 30 ms (NOT direct 50 ms)
		expectedLatency := 30
		if route.Latency() != expectedLatency {
			t.Errorf("expected route latency %d ms, got %d ms", expectedLatency, route.Latency())
		}
		if len(route.Path()) != 2 {
			t.Errorf("expected 2 hops (A->B->C), got %d", len(route.Path()))
		}
	})

	t.Run("Route to self has 0 latency", func(t *testing.T) {
		route, err := routerA.RouteToNode(nodeA)
		if err != nil || route.Latency() != 0 {
			t.Errorf("expected 0 latency to self, got %d, err %v", route.Latency(), err)
		}
	})

	t.Run("Unreachable node returns unreachable result", func(t *testing.T) {
		route, err := routerA.RouteToNode(nodeD)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if route.Reachable() {
			t.Errorf("expected node D to be unreachable")
		}
	})
}

// =============================================================================
// A* Router Path & Latency Calculation Tests
// =============================================================================

func TestAStarRouterCalculations(t *testing.T) {
	// Topology:
	// A --(15ms)--> B --(10ms)--> C
	// A -----------(40ms)-------> C
	// Island (isolated)

	nodeA := newRouteMockNode("SatA", 0, 0, 0)
	nodeB := newRouteMockNode("SatB", 1000, 0, 0)
	nodeC := newRouteMockNode("SatC", 2000, 0, 0)
	nodeIsland := newRouteMockNode("SatIsland", 9000, 9000, 0)

	routerA := NewAStarRouter()
	nodeA.router = routerA
	_ = routerA.Mount(nodeA)

	routerB := NewAStarRouter()
	nodeB.router = routerB
	_ = routerB.Mount(nodeB)

	routerC := NewAStarRouter()
	nodeC.router = routerC
	_ = routerC.Mount(nodeC)

	newRouteMockLink(nodeA, nodeB, 15.0) // A-B: 15 ms
	newRouteMockLink(nodeB, nodeC, 10.0) // B-C: 10 ms
	newRouteMockLink(nodeA, nodeC, 40.0) // A-C: 40 ms

	t.Run("A* finds optimal shortest path (15+10=25ms vs 40ms)", func(t *testing.T) {
		route, err := routerA.RouteToNode(nodeC)
		if err != nil {
			t.Fatalf("RouteToNode(C) error: %v", err)
		}
		if !route.Reachable() {
			t.Fatalf("expected node C to be reachable")
		}

		expectedLatency := 25
		if route.Latency() != expectedLatency {
			t.Errorf("expected latency %d ms, got %d ms", expectedLatency, route.Latency())
		}
		if len(route.Path()) != 2 {
			t.Errorf("expected 2 hops (SatA -> SatB -> SatC), got %d hops", len(route.Path()))
		}
	})

	t.Run("A* route to self", func(t *testing.T) {
		route, err := routerA.RouteToNode(nodeA)
		if err != nil || route.Latency() != 0 || len(route.Path()) != 0 {
			t.Errorf("expected 0 latency and 0 hops to self, got lat=%d, hops=%d, err=%v", route.Latency(), len(route.Path()), err)
		}
	})

	t.Run("A* to unreachable node", func(t *testing.T) {
		route, err := routerA.RouteToNode(nodeIsland)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if route.Reachable() {
			t.Errorf("expected nodeIsland to be unreachable")
		}
	})
}
