package network_test

import (
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/internal/network"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

type netMockSat struct {
	name           string
	pos            types.Vector
	coordinatingGS types.GroundStation
}

func (m *netMockSat) GetName() string                          { return m.name }
func (m *netMockSat) GetPosition() types.Vector                { return m.pos }
func (m *netMockSat) DistanceTo(other types.Node) float64 {
	dx := other.GetPosition().X - m.pos.X
	dy := other.GetPosition().Y - m.pos.Y
	dz := other.GetPosition().Z - m.pos.Z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}
func (m *netMockSat) GetRouter() types.Router                     { return nil }
func (m *netMockSat) GetComputing() types.Computing               { return nil }
func (m *netMockSat) GetLinkNodeProtocol() types.LinkNodeProtocol { return nil }
func (m *netMockSat) UpdatePosition(simTime time.Time)            {}
func (m *netMockSat) GetISLProtocol() types.InterSatelliteLinkProtocol {
	return nil
}
func (m *netMockSat) SetCoordinatingGS(gs types.GroundStation) {
	m.coordinatingGS = gs
}
func (m *netMockSat) GetCoordinatingGS() types.GroundStation {
	return m.coordinatingGS
}

type netMockGS struct {
	name              string
	pos               types.Vector
	visibleSatellites []types.Satellite
	router            types.Router
}

func (m *netMockGS) GetName() string                          { return m.name }
func (m *netMockGS) GetPosition() types.Vector                { return m.pos }
func (m *netMockGS) DistanceTo(other types.Node) float64 {
	dx := other.GetPosition().X - m.pos.X
	dy := other.GetPosition().Y - m.pos.Y
	dz := other.GetPosition().Z - m.pos.Z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}
func (m *netMockGS) GetRouter() types.Router                     { return m.router }
func (m *netMockGS) GetComputing() types.Computing               { return nil }
func (m *netMockGS) GetLinkNodeProtocol() types.LinkNodeProtocol { return nil }
func (m *netMockGS) UpdatePosition(simTime time.Time)            {}
func (m *netMockGS) SetVisibleSatellites(sats []types.Satellite) {
	m.visibleSatellites = sats
}
func (m *netMockGS) GetVisibleSatellites() []types.Satellite {
	return m.visibleSatellites
}
func (m *netMockGS) EnqueueTask(task types.DeployableService) {}
func (m *netMockGS) GetTaskQueue() []types.DeployableService  { return nil }
func (m *netMockGS) ClearTaskQueue()                         {}

type netMockLink struct {
	traffic uint64
}

func (l *netMockLink) Distance() float64                   { return 0 }
func (l *netMockLink) Latency() float64                    { return 15 }
func (l *netMockLink) Bandwidth() float64                  { return 1e9 }
func (l *netMockLink) IsReachable() bool                   { return true }
func (l *netMockLink) GetOther(self types.Node) types.Node { return nil }
func (l *netMockLink) Involves(node types.Node) bool       { return false }
func (l *netMockLink) Nodes() (types.Node, types.Node)     { return nil, nil }
func (l *netMockLink) AddTraffic(bytes uint64)             { atomic.AddUint64(&l.traffic, bytes) }
func (l *netMockLink) GetTraffic() uint64                  { return atomic.LoadUint64(&l.traffic) }
func (l *netMockLink) ResetTraffic()                       { atomic.StoreUint64(&l.traffic, 0) }

type netMockRouteResult struct {
	reachable bool
	latency   int
	path      []types.Link
}

func (r *netMockRouteResult) Reachable() bool                          { return r.reachable }
func (r *netMockRouteResult) Latency() int                             { return r.latency }
func (r *netMockRouteResult) Path() []types.Link                       { return r.path }
func (r *netMockRouteResult) AddCalculationDuration(d int) types.RouteResult { return r }
func (r *netMockRouteResult) WaitLatencyAsync() error                  { return nil }

type netMockRouter struct {
	route types.RouteResult
}

func (r *netMockRouter) Mount(node types.Node) error                                    { return nil }
func (r *netMockRouter) RouteToNode(target types.Node) (types.RouteResult, error)      { return r.route, nil }
func (r *netMockRouter) RouteToService(serviceName string) (types.RouteResult, error)  { return r.route, nil }
func (r *netMockRouter) CalculateRoutingTable() error                                  { return nil }
func (r *netMockRouter) CanPreRouteCalc() bool                                         { return false }
func (r *netMockRouter) CanOnRouteCalc() bool                                          { return true }
func (r *netMockRouter) AdvertiseNewServiceAsync(serviceName string) error             { return nil }
func (r *netMockRouter) ReceiveServiceAdvertismentsAsync(serviceName string, outlink types.Link, route types.RouteResult) error {
	return nil
}

type netMockPayload struct {
	size uint64
}

func (p *netMockPayload) SizeBytes() uint64 { return p.size }

func TestControlPlaneCoordinatorUpdateMappings(t *testing.T) {
	coord := network.NewControlPlaneCoordinator()

	// 2 Ground Stations: GS1 at (1000, 0, 0), GS2 at (5000, 0, 0)
	gs1 := &netMockGS{name: "GS1", pos: types.NewVector(1000, 0, 0)}
	gs2 := &netMockGS{name: "GS2", pos: types.NewVector(5000, 0, 0)}
	gss := []types.GroundStation{gs1, gs2}

	// 3 Satellites:
	// SatA at (1200, 0, 0) -> closest to GS1
	// SatB at (4800, 0, 0) -> closest to GS2
	// SatC at (1400, 0, 0) -> closest to GS1
	satA := &netMockSat{name: "SatA", pos: types.NewVector(1200, 0, 0)}
	satB := &netMockSat{name: "SatB", pos: types.NewVector(4800, 0, 0)}
	satC := &netMockSat{name: "SatC", pos: types.NewVector(1400, 0, 0)}
	sats := []types.Satellite{satA, satB, satC}

	coord.UpdateMappings(sats, gss, time.Now())

	// SatA and SatC should be coordinated by GS1
	if satA.GetCoordinatingGS() == nil || satA.GetCoordinatingGS().GetName() != gs1.GetName() {
		t.Errorf("expected SatA coordinated by GS1, got %v", satA.GetCoordinatingGS())
	}
	if satC.GetCoordinatingGS() == nil || satC.GetCoordinatingGS().GetName() != gs1.GetName() {
		t.Errorf("expected SatC coordinated by GS1, got %v", satC.GetCoordinatingGS())
	}

	// SatB should be coordinated by GS2
	if satB.GetCoordinatingGS() == nil || satB.GetCoordinatingGS().GetName() != gs2.GetName() {
		t.Errorf("expected SatB coordinated by GS2, got %v", satB.GetCoordinatingGS())
	}

	// GS1 should have SatA and SatC visible
	if len(gs1.visibleSatellites) != 2 {
		t.Errorf("expected GS1 to have 2 visible satellites, got %d", len(gs1.visibleSatellites))
	}

	// GS2 should have SatB visible
	if len(gs2.visibleSatellites) != 1 {
		t.Errorf("expected GS2 to have 1 visible satellite, got %d", len(gs2.visibleSatellites))
	}
}

func TestNetworkServiceTransmission(t *testing.T) {
	svc := network.NewNetworkService()

	link1 := &netMockLink{}
	link2 := &netMockLink{}
	route := &netMockRouteResult{
		reachable: true,
		latency:   25,
		path:      []types.Link{link1, link2},
	}

	src := &netMockGS{name: "SrcGS", router: &netMockRouter{route: route}}
	dst := &netMockSat{name: "DstSat"}
	payload := &netMockPayload{size: 5000}

	initialTraffic := atomic.LoadUint64(&network.TotalTransmittedBytes)

	lat, err := svc.Transmit(src, dst, payload)
	if err != nil {
		t.Fatalf("Transmit failed: %v", err)
	}

	if lat != 25 {
		t.Errorf("expected latency 25, got %d", lat)
	}

	// Both links in path should have received 5000 bytes
	if link1.GetTraffic() != 5000 {
		t.Errorf("expected link1 traffic 5000, got %d", link1.GetTraffic())
	}
	if link2.GetTraffic() != 5000 {
		t.Errorf("expected link2 traffic 5000, got %d", link2.GetTraffic())
	}

	// Global transmitted bytes should increase by actualNetworkLoad = hops * payloadSize = 2 * 5000 = 10000
	expectedGlobalDelta := uint64(len(route.Path())) * payload.SizeBytes()
	newTraffic := atomic.LoadUint64(&network.TotalTransmittedBytes)
	if newTraffic-initialTraffic != expectedGlobalDelta {
		t.Errorf("expected global traffic delta %d, got %d", expectedGlobalDelta, newTraffic-initialTraffic)
	}

	// Unreachable route returns error
	unreachableRoute := &netMockRouteResult{reachable: false}
	srcUnreach := &netMockGS{name: "SrcUnreach", router: &netMockRouter{route: unreachableRoute}}
	lat, err = svc.Transmit(srcUnreach, dst, payload)
	if err == nil || lat != -1 {
		t.Errorf("expected error and -1 latency for unreachable route, got lat=%d, err=%v", lat, err)
	}
}
