package stateplugin

import (
	"math"
	"testing"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

type stateMockNode struct {
	name     string
	position types.Vector
}

func (m *stateMockNode) GetName() string                          { return m.name }
func (m *stateMockNode) GetPosition() types.Vector                { return m.position }
func (m *stateMockNode) DistanceTo(other types.Node) float64      { return 0 }
func (m *stateMockNode) GetRouter() types.Router                  { return nil }
func (m *stateMockNode) GetComputing() types.Computing            { return nil }
func (m *stateMockNode) GetLinkNodeProtocol() types.LinkNodeProtocol { return nil }
func (m *stateMockNode) UpdatePosition(simTime time.Time)         {}

// =============================================================================
// Solar Vector & Orbit Eclipse Geometry Tests
// =============================================================================

func TestSunVectorGeneration(t *testing.T) {
	plugin := NewThermalEnvironmentStatePlugin()

	// Vernal equinox (ref epoch): sun should be approximately in +X direction in ECI
	vernalEquinox := time.Date(2025, 3, 20, 9, 1, 0, 0, time.UTC)
	plugin.updateSunVector(vernalEquinox)

	sunVec := plugin.sunVector

	// Sun vector must be a unit vector
	if math.Abs(sunVec.Magnitude()-1.0) > 1e-6 {
		t.Errorf("expected unit sun vector, got magnitude %f", sunVec.Magnitude())
	}

	// At vernal equinox, X component should be near 1.0, Y and Z near 0.0
	if sunVec.X < 0.99 {
		t.Errorf("expected sun vector X ~ 1.0 at vernal equinox, got %f", sunVec.X)
	}
	if math.Abs(sunVec.Y) > 0.1 || math.Abs(sunVec.Z) > 0.1 {
		t.Errorf("expected sun vector Y and Z near 0 at vernal equinox, got Y=%f, Z=%f", sunVec.Y, sunVec.Z)
	}

	// 3 months later (Summer Solstice, ~91 days later):
	// Sun has moved 90 degrees in ecliptic plane (X ~ 0, Y ~ cos(eps), Z ~ sin(eps))
	summerSolstice := vernalEquinox.Add(91 * 24 * time.Hour)
	plugin.updateSunVector(summerSolstice)
	sunSolstice := plugin.sunVector

	if math.Abs(sunSolstice.Magnitude()-1.0) > 1e-6 {
		t.Errorf("expected unit sun vector at solstice, got magnitude %f", sunSolstice.Magnitude())
	}
	// At summer solstice, Z component is positive (obliquity angle ~ 23.44 deg -> sin(23.44) ~ 0.398)
	if sunSolstice.Z < 0.35 || sunSolstice.Z > 0.45 {
		t.Errorf("expected sun solstice Z component ~ 0.398, got %f", sunSolstice.Z)
	}
}

func TestOrbitalConicalEclipseCalculation(t *testing.T) {
	plugin := NewThermalEnvironmentStatePlugin()
	// Set fixed sun vector along +X axis
	plugin.sunVector = types.Vector{X: 1, Y: 0, Z: 0}
	rLEO := 6928000.0 // 6,928 km orbital radius

	t.Run("Satellite on day side is in full sunlight", func(t *testing.T) {
		pos := types.Vector{X: rLEO, Y: 0, Z: 0}
		dist := rLEO
		distSq := dist * dist
		dot := pos.X * plugin.sunVector.X // +rLEO > 0
		normDot := dot / dist             // 1.0

		orbPos := plugin.calculateOrbitalPosition(pos, dist, distSq, dot, normDot)
		if orbPos.InEclipse {
			t.Errorf("expected satellite on sun side to not be in eclipse")
		}
		if orbPos.EclipseDepth != 0.0 {
			t.Errorf("expected EclipseDepth 0.0, got %f", orbPos.EclipseDepth)
		}

		exposure := plugin.calculateSunlightExposure(normDot, orbPos)
		if exposure != 1.0 {
			t.Errorf("expected sunlight exposure 1.0, got %f", exposure)
		}
	})

	t.Run("Satellite directly behind Earth along shadow axis is in total umbra", func(t *testing.T) {
		pos := types.Vector{X: -rLEO, Y: 0, Z: 0}
		dist := rLEO
		distSq := dist * dist
		dot := pos.X * plugin.sunVector.X // -rLEO < 0
		normDot := dot / dist             // -1.0

		orbPos := plugin.calculateOrbitalPosition(pos, dist, distSq, dot, normDot)
		if !orbPos.InEclipse {
			t.Errorf("expected satellite directly behind Earth to be in eclipse")
		}
		if orbPos.EclipseDepth != 1.0 {
			t.Errorf("expected EclipseDepth 1.0 in umbra, got %f", orbPos.EclipseDepth)
		}

		exposure := plugin.calculateSunlightExposure(normDot, orbPos)
		if exposure != 0.0 {
			t.Errorf("expected sunlight exposure 0.0 in umbra, got %f", exposure)
		}
	})

	t.Run("Satellite behind Earth but far outside shadow cylinder is illuminated", func(t *testing.T) {
		// Sat is at X = -rLEO, but Y = 20,000 km (far above Earth radius)
		pos := types.Vector{X: -rLEO, Y: 20000000.0, Z: 0}
		dist := pos.Magnitude()
		distSq := dist * dist
		dot := pos.X * plugin.sunVector.X
		normDot := dot / dist

		orbPos := plugin.calculateOrbitalPosition(pos, dist, distSq, dot, normDot)
		if orbPos.InEclipse {
			t.Errorf("expected satellite far outside shadow cylinder to not be in eclipse")
		}
		if orbPos.EclipseDepth != 0.0 {
			t.Errorf("expected EclipseDepth 0.0, got %f", orbPos.EclipseDepth)
		}
	})
}

// =============================================================================
// Environmental Heat Input Formulas Tests
// =============================================================================

func TestEnvironmentalHeatFormulas(t *testing.T) {
	plugin := NewThermalEnvironmentStatePlugin()

	thermalProps := types.ThermalProperties{
		Absorptivity: 0.92,
		Emissivity:   0.85,
		SurfaceArea:  0.14,
	}

	dist := 6928000.0 // LEO radius

	t.Run("Solar heat calculation", func(t *testing.T) {
		// Q_solar = Absorptivity * SolarConstant * SurfaceArea * exposure
		exposure := 0.8
		expected := thermalProps.Absorptivity * types.SolarConstant * thermalProps.SurfaceArea * exposure

		heat := plugin.calculateSolarHeat(thermalProps, exposure)
		if math.Abs(heat-expected) > 1e-6 {
			t.Errorf("calculateSolarHeat(): expected %f W, got %f W", expected, heat)
		}

		// Zero exposure gives 0 heat
		if plugin.calculateSolarHeat(thermalProps, 0.0) != 0.0 {
			t.Errorf("expected 0 heat for 0 exposure")
		}
	})

	t.Run("Earth albedo heat calculation", func(t *testing.T) {
		// Q_albedo = Absorptivity * SolarConstant * EarthAlbedoFactor * SurfaceArea * viewFactor * normDot
		// viewFactor = (earthRadius / dist)^2
		normDot := 0.75 // positive
		viewFactor := math.Pow(plugin.earthRadius/dist, 2)
		expected := thermalProps.Absorptivity * types.SolarConstant * types.EarthAlbedoFactor * thermalProps.SurfaceArea * viewFactor * normDot

		heat := plugin.calculateAlbedoHeat(dist, normDot, thermalProps)
		if math.Abs(heat-expected) > 1e-6 {
			t.Errorf("calculateAlbedoHeat(): expected %f W, got %f W", expected, heat)
		}

		// When normDot <= 0 (night side of Earth), albedo must be 0
		if plugin.calculateAlbedoHeat(dist, -0.5, thermalProps) != 0.0 {
			t.Errorf("expected 0 albedo heat on night side")
		}
	})

	t.Run("Earth infrared (IR) heat calculation", func(t *testing.T) {
		// Q_ir = Emissivity * StefanBoltzmannConstant * SurfaceArea * viewFactor * (EarthTemperature^4)
		viewFactor := math.Pow(plugin.earthRadius/dist, 2)
		expected := thermalProps.Emissivity * types.StefanBoltzmannConstant * thermalProps.SurfaceArea * viewFactor * math.Pow(types.EarthTemperature, 4)

		heat := plugin.calculateIRHeat(dist, thermalProps)
		if math.Abs(heat-expected) > 1e-6 {
			t.Errorf("calculateIRHeat(): expected %f W, got %f W", expected, heat)
		}

		// IR heat should decrease as distance increases (inverse square law with viewFactor)
		heatFar := plugin.calculateIRHeat(dist*2.0, thermalProps)
		// At 2x distance, viewFactor is 1/4 -> heat should be 1/4
		if math.Abs((heat/heatFar)-4.0) > 1e-4 {
			t.Errorf("expected IR heat to scale as 1/r^2: ratio %f != 4.0", heat/heatFar)
		}
	})
}
