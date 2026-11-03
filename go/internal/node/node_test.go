package node

import (
	"math"
	"testing"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// mockRouter is a minimal dummy implementation of types.Router for testing
type mockRouter struct{}

func (m *mockRouter) Mount(node types.Node) error                                            { return nil }
func (m *mockRouter) RouteToNode(target types.Node) (types.RouteResult, error)              { return nil, nil }
func (m *mockRouter) RouteToService(serviceName string) (types.RouteResult, error)          { return nil, nil }
func (m *mockRouter) CalculateRoutingTable() error                                          { return nil }
func (m *mockRouter) CanPreRouteCalc() bool                                                 { return false }
func (m *mockRouter) CanOnRouteCalc() bool                                                  { return true }
func (m *mockRouter) AdvertiseNewServiceAsync(serviceName string) error                     { return nil }
func (m *mockRouter) ReceiveServiceAdvertismentsAsync(serviceName string, outlink types.Link, route types.RouteResult) error {
	return nil
}

// mockComputing is a minimal dummy implementation of types.Computing for testing
type mockComputing struct {
	cpu    float64
	memory float64
}

func (m *mockComputing) Mount(node types.Node) error                                    { return nil }
func (m *mockComputing) TryPlaceDeploymentAsync(s types.DeployableService) (bool, error) { return true, nil }
func (m *mockComputing) RemoveDeploymentAsync(s types.DeployableService) error          { return nil }
func (m *mockComputing) Tick(deltaT float64, currentTime time.Time)                     {}
func (m *mockComputing) CanPlace(s types.DeployableService) bool                        { return true }
func (m *mockComputing) HostsService(serviceName string) bool                           { return false }
func (m *mockComputing) CpuAvailable() float64                                          { return m.cpu }
func (m *mockComputing) MemoryAvailable() float64                                       { return m.memory }
func (m *mockComputing) Clone() types.Computing                                         { return m }
func (m *mockComputing) GetComputingType() types.ComputingType                          { return types.Edge }
func (m *mockComputing) GetCpuUtilization() float64                                     { return 0 }
func (m *mockComputing) GetServices() []types.DeployableService                         { return nil }
func (m *mockComputing) GetActiveServiceCount() int                                     { return 0 }

// mockLinkProtocol is a minimal dummy implementation of types.GroundSatelliteLinkProtocol
type mockGroundProtocol struct{}

func (m *mockGroundProtocol) Mount(node types.Node)                  {}
func (m *mockGroundProtocol) AddLink(link types.Link)                {}
func (m *mockGroundProtocol) ConnectLink(link types.Link) error       { return nil }
func (m *mockGroundProtocol) DisconnectLink(link types.Link) error    { return nil }
func (m *mockGroundProtocol) UpdateLinks() ([]types.Link, error)     { return nil, nil }
func (m *mockGroundProtocol) Established() []types.Link              { return nil }
func (m *mockGroundProtocol) Links() []types.Link                    { return nil }

type mockISLProtocol struct{}

func (m *mockISLProtocol) Mount(node types.Node)               {}
func (m *mockISLProtocol) AddLink(link types.Link)             {}
func (m *mockISLProtocol) ConnectLink(link types.Link) error    { return nil }
func (m *mockISLProtocol) DisconnectLink(link types.Link) error { return nil }
func (m *mockISLProtocol) UpdateLinks() ([]types.Link, error)  { return nil, nil }
func (m *mockISLProtocol) Established() []types.Link           { return nil }
func (m *mockISLProtocol) Links() []types.Link                 { return nil }

type testNode struct {
	BaseNode
}

func (n *testNode) UpdatePosition(simTime time.Time)              {}
func (n *testNode) GetLinkNodeProtocol() types.LinkNodeProtocol   { return &mockISLProtocol{} }

// =============================================================================
// BaseNode & Distance Calculation Tests
// =============================================================================

func TestBaseNodeDistanceTo(t *testing.T) {
	node1 := &testNode{BaseNode: BaseNode{
		Name:     "Node1",
		Position: types.NewVector(0, 0, 0),
	}}
	node2 := &testNode{BaseNode: BaseNode{
		Name:     "Node2",
		Position: types.NewVector(3000, 4000, 0),
	}}
	node3 := &testNode{BaseNode: BaseNode{
		Name:     "Node3",
		Position: types.NewVector(1000, 2000, 2000),
	}}

	dist12 := node1.DistanceTo(node2)
	expected12 := 5000.0 // sqrt(3000^2 + 4000^2) = 5000
	if math.Abs(dist12-expected12) > 1e-9 {
		t.Errorf("DistanceTo(node2): expected %f, got %f", expected12, dist12)
	}

	dist13 := node1.DistanceTo(node3)
	expected13 := 3000.0 // sqrt(1000^2 + 2000^2 + 2000^2) = sqrt(9,000,000) = 3000
	if math.Abs(dist13-expected13) > 1e-9 {
		t.Errorf("DistanceTo(node3): expected %f, got %f", expected13, dist13)
	}

	// Distance is symmetric
	dist21 := node2.DistanceTo(node1)
	if math.Abs(dist12-dist21) > 1e-9 {
		t.Errorf("DistanceTo symmetry violated: %f vs %f", dist12, dist21)
	}
}

// =============================================================================
// LiveGroundStation Geodetic Coordinate Conversion & Earth Rotation Tests
// =============================================================================

func TestLiveGroundStationPositionAtEquatorAndPole(t *testing.T) {
	epoch := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Ground station at Equator (lat 0, lon 0)
	gsEquator := NewLiveGroundStation("GS_Equator", 0.0, 0.0, &mockGroundProtocol{}, epoch, &mockRouter{}, &mockComputing{})
	posEq := gsEquator.GetPosition()

	// WGS-84 semi-major axis is 6,378,137 m
	const a = 6378137.0
	const b = 6356752.314245

	if math.Abs(posEq.X-a) > 1.0 {
		t.Errorf("Equator GS X coordinate: expected ~%f, got %f", a, posEq.X)
	}
	if math.Abs(posEq.Y) > 1.0 {
		t.Errorf("Equator GS Y coordinate: expected ~0, got %f", posEq.Y)
	}
	if math.Abs(posEq.Z) > 1.0 {
		t.Errorf("Equator GS Z coordinate: expected ~0, got %f", posEq.Z)
	}

	// Ground station at North Pole (lat 90, lon 0)
	gsPole := NewLiveGroundStation("GS_Pole", 90.0, 0.0, &mockGroundProtocol{}, epoch, &mockRouter{}, &mockComputing{})
	posPole := gsPole.GetPosition()

	if math.Abs(posPole.X) > 1.0 {
		t.Errorf("North Pole GS X coordinate: expected ~0, got %f", posPole.X)
	}
	if math.Abs(posPole.Y) > 1.0 {
		t.Errorf("North Pole GS Y coordinate: expected ~0, got %f", posPole.Y)
	}
	if math.Abs(posPole.Z-b) > 1.0 {
		t.Errorf("North Pole GS Z coordinate: expected ~%f, got %f", b, posPole.Z)
	}
}

func TestLiveGroundStationEarthRotation(t *testing.T) {
	epoch := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	gs := NewLiveGroundStation("GS_Rot", 0.0, 0.0, &mockGroundProtocol{}, epoch, &mockRouter{}, &mockComputing{})

	initialPos := gs.GetPosition()
	initialRadius := initialPos.Magnitude()

	// Earth rotation rate: 7.2921150e-5 rad/s
	// Quarter rotation (pi/2 radians):
	const rotationSpeed = 7.2921150e-5
	tQuarter := (math.Pi / 2.0) / rotationSpeed

	simTime := epoch.Add(time.Duration(tQuarter * float64(time.Second)))
	gs.UpdatePosition(simTime)

	rotPos := gs.GetPosition()

	// After 90 deg rotation, X should be ~0 and Y should be ~initialRadius
	if math.Abs(rotPos.X) > 10.0 {
		t.Errorf("After 90 deg rotation, expected X ~0, got %f", rotPos.X)
	}
	if math.Abs(rotPos.Y-initialRadius) > 10.0 {
		t.Errorf("After 90 deg rotation, expected Y ~%f, got %f", initialRadius, rotPos.Y)
	}
	if math.Abs(rotPos.Z) > 1.0 {
		t.Errorf("After rotation, expected Z ~0, got %f", rotPos.Z)
	}

	// Magnitude should remain exactly constant
	if math.Abs(rotPos.Magnitude()-initialRadius) > 1e-4 {
		t.Errorf("Magnitude should be conserved under rotation: initial %f, rotated %f", initialRadius, rotPos.Magnitude())
	}
}

func TestLiveGroundStationFindNearestSatellite(t *testing.T) {
	epoch := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	gs := NewLiveGroundStation("GS", 0.0, 0.0, &mockGroundProtocol{}, epoch, &mockRouter{}, &mockComputing{})
	gsPos := gs.GetPosition()

	// Empty satellite list should return error
	_, err := gs.FindNearestSatellite(nil)
	if err == nil {
		t.Errorf("expected error for empty satellite list, got nil")
	}

	// Create 3 satellites with different distances
	satFar := &BaseNode{
		Name:     "SatFar",
		Position: types.NewVector(gsPos.X+2000000, gsPos.Y, gsPos.Z),
	}
	satClose := &BaseNode{
		Name:     "SatClose",
		Position: types.NewVector(gsPos.X+500000, gsPos.Y, gsPos.Z),
	}
	satMid := &BaseNode{
		Name:     "SatMid",
		Position: types.NewVector(gsPos.X+1000000, gsPos.Y, gsPos.Z),
	}

	sats := []types.Satellite{
		&mockSatWrapper{BaseNode: satFar},
		&mockSatWrapper{BaseNode: satClose},
		&mockSatWrapper{BaseNode: satMid},
	}

	nearest, err := gs.FindNearestSatellite(sats)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nearest.GetName() != "SatClose" {
		t.Errorf("expected nearest satellite to be SatClose, got %s", nearest.GetName())
	}
}

type mockSatWrapper struct {
	*BaseNode
}

func (m *mockSatWrapper) GetLinkNodeProtocol() types.LinkNodeProtocol { return &mockISLProtocol{} }
func (m *mockSatWrapper) GetISLProtocol() types.InterSatelliteLinkProtocol {
	return &mockISLProtocol{}
}
func (m *mockSatWrapper) UpdatePosition(simTime time.Time) {}
func (m *mockSatWrapper) SetCoordinatingGS(gs types.GroundStation) {}
func (m *mockSatWrapper) GetCoordinatingGS() types.GroundStation  { return nil }

// =============================================================================
// LiveSatellite Orbital Mechanics Calculations Tests
// =============================================================================

func TestNormalizeAngle(t *testing.T) {
	tests := []struct {
		input    float64
		expected float64
	}{
		{0.0, 0.0},
		{math.Pi, math.Pi},
		{2 * math.Pi, 0.0},
		{3 * math.Pi, math.Pi},
		{-math.Pi / 2.0, 3.0 * math.Pi / 2.0},
		{-2 * math.Pi, 0.0},
		{4 * math.Pi, 0.0},
	}

	for _, tt := range tests {
		res := normalizeAngle(tt.input)
		if math.Abs(res-tt.expected) > 1e-9 {
			t.Errorf("normalizeAngle(%f): expected %f, got %f", tt.input, tt.expected, res)
		}
	}
}

func TestSolveKeplersEquation(t *testing.T) {
	// Circular orbit (eccentricity = 0): E should equal M
	M1 := math.Pi / 3.0
	E1 := solveKeplersEquation(M1, 0.0)
	if math.Abs(E1-M1) > 1e-9 {
		t.Errorf("eccentricity 0: expected E = M (%f), got %f", M1, E1)
	}

	// Elliptical orbit with e = 0.1, 0.3, 0.5
	testCases := []struct {
		M   float64
		ecc float64
	}{
		{0.0, 0.1},
		{math.Pi / 6.0, 0.1},
		{math.Pi / 4.0, 0.25},
		{math.Pi / 2.0, 0.3},
		{math.Pi, 0.5},
	}

	for _, tc := range testCases {
		E := solveKeplersEquation(tc.M, tc.ecc)
		// Verify Kepler's equation holds: E - e*sin(E) == M
		lhs := E - tc.ecc*math.Sin(E)
		if math.Abs(lhs-tc.M) > 1e-8 {
			t.Errorf("Kepler equation violation for M=%f, ecc=%f: residual |%f - %f| = %e",
				tc.M, tc.ecc, lhs, tc.M, math.Abs(lhs-tc.M))
		}
	}
}

func TestComputeTrueAnomaly(t *testing.T) {
	// For circular orbit (ecc = 0), true anomaly nu == eccentric anomaly E
	E_circ := math.Pi / 3.0
	nu_circ := computeTrueAnomaly(E_circ, 0.0)
	if math.Abs(nu_circ-E_circ) > 1e-9 {
		t.Errorf("ecc 0: expected nu == E, got %f vs %f", nu_circ, E_circ)
	}

	// At perigee (E = 0), nu = 0 for any eccentricity
	nu_perigee := computeTrueAnomaly(0.0, 0.2)
	if math.Abs(nu_perigee) > 1e-9 {
		t.Errorf("at perigee (E=0): expected nu = 0, got %f", nu_perigee)
	}

	// At apogee (E = pi), nu = pi for any eccentricity
	nu_apogee := computeTrueAnomaly(math.Pi, 0.3)
	if math.Abs(nu_apogee-math.Pi) > 1e-9 {
		t.Errorf("at apogee (E=pi): expected nu = pi, got %f", nu_apogee)
	}
}

func TestApplyOrbitalTransformations(t *testing.T) {
	// Planar coordinates with 0 inclination, 0 RAAN, 0 ArgPerigee
	// Should map (x, y, 0) directly to (x, y, 0) in ECI
	x := 7000000.0
	y := 2000000.0
	z := 0.0

	res := applyOrbitalTransformations(x, y, z, 0.0, 0.0, 0.0)
	if math.Abs(res.X-x) > 1e-6 || math.Abs(res.Y-y) > 1e-6 || math.Abs(res.Z-z) > 1e-6 {
		t.Errorf("zero angle transformation failed: expected (%f, %f, %f), got %v", x, y, z, res)
	}

	// 90 degree inclination (polar orbit):
	// A vector along Y in orbital plane should map onto the Z axis in ECI
	resPolar := applyOrbitalTransformations(0.0, 7000000.0, 0.0, math.Pi/2.0, 0.0, 0.0)
	if math.Abs(resPolar.X) > 1e-6 || math.Abs(resPolar.Y) > 1e-6 || math.Abs(resPolar.Z-7000000.0) > 1e-6 {
		t.Errorf("polar orbit transformation failed: expected (0, 0, 7000000), got %v", resPolar)
	}
}

func TestKeplersThirdLawSemiMajorAxis(t *testing.T) {
	epoch := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Typical Starlink satellite: ~15.06 revolutions per day (period ~95.6 min, altitude ~550km)
	meanMotion := 15.06
	sat := NewLiveSatellite(
		"Starlink",
		53.0, 0.0, 0.0001, 0.0, 0.0,
		meanMotion,
		epoch, epoch,
		&mockISLProtocol{}, &mockRouter{}, &mockComputing{},
	)

	// Expected semi-major axis: a = (mu / n^2)^(1/3)
	// n = 15.06 * 2 * pi / 86400 rad/s
	nRadPerSec := meanMotion * 2.0 * math.Pi / 86400.0
	expectedA := math.Cbrt(earthMu / (nRadPerSec * nRadPerSec))

	if math.Abs(sat.semiMajorAxis-expectedA) > 1.0 {
		t.Errorf("Semi-major axis calculation: expected %f, got %f", expectedA, sat.semiMajorAxis)
	}

	// Verify altitude is in the expected LEO range (~540km - 560km):
	// altitude = a - earthEquatorialRad
	altitude := sat.semiMajorAxis - earthEquatorialRad
	if altitude < 500000.0 || altitude > 600000.0 {
		t.Errorf("Expected Starlink altitude in ~500-600km range, got %f m", altitude)
	}

	// Verify satellite position magnitude matches orbital distance ~a
	pos := sat.GetPosition()
	posMag := pos.Magnitude()
	if math.Abs(posMag-sat.semiMajorAxis) > sat.semiMajorAxis*0.01 {
		t.Errorf("Expected orbital radius ~%f, got position magnitude %f", sat.semiMajorAxis, posMag)
	}
}
