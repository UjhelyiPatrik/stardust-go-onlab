package links_test

import (
	"math"
	"testing"
	"time"

	configmod "github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/internal/links"
	"github.com/polaris-slo-cloud/stardust-go/internal/links/linktypes"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

type protoMockNode struct {
	name     string
	position types.Vector
	protocol types.LinkNodeProtocol
}

func (m *protoMockNode) GetName() string                          { return m.name }
func (m *protoMockNode) GetPosition() types.Vector                { return m.position }
func (m *protoMockNode) DistanceTo(other types.Node) float64 {
	dx := other.GetPosition().X - m.position.X
	dy := other.GetPosition().Y - m.position.Y
	dz := other.GetPosition().Z - m.position.Z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}
func (m *protoMockNode) GetRouter() types.Router                  { return nil }
func (m *protoMockNode) GetComputing() types.Computing            { return nil }
func (m *protoMockNode) GetLinkNodeProtocol() types.LinkNodeProtocol {
	if m.protocol != nil {
		return m.protocol
	}
	return &dummyProto{}
}
func (m *protoMockNode) UpdatePosition(simTime time.Time)         {}

type dummyProto struct{}

func (d *dummyProto) Mount(node types.Node)                  {}
func (d *dummyProto) AddLink(link types.Link)                {}
func (d *dummyProto) ConnectLink(link types.Link) error       { return nil }
func (d *dummyProto) DisconnectLink(link types.Link) error    { return nil }
func (d *dummyProto) UpdateLinks() ([]types.Link, error)     { return nil, nil }
func (d *dummyProto) Established() []types.Link              { return nil }
func (d *dummyProto) Links() []types.Link                    { return nil }

type dummySatellite struct {
	*protoMockNode
}

func (s *dummySatellite) GetISLProtocol() types.InterSatelliteLinkProtocol { return nil }
func (s *dummySatellite) SetCoordinatingGS(gs types.GroundStation)         {}
func (s *dummySatellite) GetCoordinatingGS() types.GroundStation          { return nil }

func TestIslNearestProtocolSelection(t *testing.T) {
	// Self satellite at +X axis in LEO
	rLEO := float64(configmod.EarthRadius + 550000)
	self := &protoMockNode{name: "Self", position: types.NewVector(rLEO, 0, 0)}

	// Configure protocol with Neighbours = 2
	cfg := configmod.InterSatelliteLinkConfig{
		Protocol:   "nearest",
		Neighbours: 2,
	}
	proto := links.NewIslNearestProtocol(cfg)
	proto.Mount(self)

	// Create 4 candidates:
	// - Cand1: very close (distance ~100 km)
	// - Cand2: medium close (distance ~300 km)
	// - Cand3: further away (distance ~800 km)
	// - CandBlocked: on opposite side of Earth (-rLEO), blocked by Earth!
	cand1 := &protoMockNode{name: "Cand1", position: types.NewVector(rLEO, 100000, 0)}
	cand2 := &protoMockNode{name: "Cand2", position: types.NewVector(rLEO, 300000, 0)}
	cand3 := &protoMockNode{name: "Cand3", position: types.NewVector(rLEO, 800000, 0)}
	candBlocked := &protoMockNode{name: "CandBlocked", position: types.NewVector(-rLEO, 0, 0)}

	proto.AddLink(linktypes.NewIslLink(self, cand3))
	proto.AddLink(linktypes.NewIslLink(self, cand1))
	proto.AddLink(linktypes.NewIslLink(self, candBlocked))
	proto.AddLink(linktypes.NewIslLink(self, cand2))

	established, err := proto.UpdateLinks()
	if err != nil {
		t.Fatalf("UpdateLinks failed: %v", err)
	}

	// Should select exactly 2 neighbors (Cand1 and Cand2)
	if len(established) != 2 {
		t.Fatalf("expected 2 established links, got %d", len(established))
	}

	// Verify that Cand1 and Cand2 are the ones selected, and CandBlocked / Cand3 are not
	hasCand1 := false
	hasCand2 := false
	for _, l := range established {
		other := l.GetOther(self)
		if other.GetName() == "Cand1" {
			hasCand1 = true
		}
		if other.GetName() == "Cand2" {
			hasCand2 = true
		}
		if other.GetName() == "CandBlocked" {
			t.Errorf("CandBlocked should not have been selected (blocked by Earth)")
		}
		if other.GetName() == "Cand3" {
			t.Errorf("Cand3 should not have been selected (rank 3, but limit is 2)")
		}
	}

	if !hasCand1 || !hasCand2 {
		t.Errorf("expected Cand1 and Cand2 to be established, got hasCand1=%v, hasCand2=%v", hasCand1, hasCand2)
	}
}

func TestGroundSatelliteNearestProtocol(t *testing.T) {
	rEarth := float64(configmod.EarthRadius)
	// GS at Equator (rEarth, 0, 0)
	gs := &protoMockNode{name: "GS", position: types.NewVector(rEarth, 0, 0)}

	// Satellites:
	// - SatOverhead: 550 km above GS -> dist = 550 km
	// - SatHigh: 1000 km above GS -> dist = 1000 km
	// - SatBelowHorizon: on opposite side -> blocked by Earth horizon
	satOverhead := &dummySatellite{protoMockNode: &protoMockNode{name: "SatOverhead", position: types.NewVector(rEarth+550000, 0, 0)}}
	satHigh := &dummySatellite{protoMockNode: &protoMockNode{name: "SatHigh", position: types.NewVector(rEarth+1000000, 0, 0)}}
	satBelow := &dummySatellite{protoMockNode: &protoMockNode{name: "SatBelow", position: types.NewVector(-rEarth-550000, 0, 0)}}

	sats := []types.Satellite{satBelow, satHigh, satOverhead}
	gsProto := links.NewGroundSatelliteNearestProtocol(sats)
	gsProto.Mount(gs)

	linksEst, err := gsProto.UpdateLinks()
	if err != nil {
		t.Fatalf("UpdateLinks failed: %v", err)
	}

	if len(linksEst) != 1 {
		t.Fatalf("expected 1 active ground link, got %d", len(linksEst))
	}

	chosen := linksEst[0].GetOther(gs)
	if chosen.GetName() != "SatOverhead" {
		t.Errorf("expected nearest visible satellite to be SatOverhead, got %s", chosen.GetName())
	}
}
