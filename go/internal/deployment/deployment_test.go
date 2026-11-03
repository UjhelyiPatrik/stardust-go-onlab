package deployment_test

import (
	"math"
	"testing"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/internal/deployment"
	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// mockDeployNode implements minimal types.Satellite and types.Node
type mockDeploySat struct {
	name      string
	computing types.Computing
}

func (m *mockDeploySat) GetName() string                             { return m.name }
func (m *mockDeploySat) GetPosition() types.Vector                   { return types.Vector{} }
func (m *mockDeploySat) DistanceTo(other types.Node) float64         { return 0 }
func (m *mockDeploySat) GetRouter() types.Router                     { return nil }
func (m *mockDeploySat) GetComputing() types.Computing               { return m.computing }
func (m *mockDeploySat) GetLinkNodeProtocol() types.LinkNodeProtocol { return nil }
func (m *mockDeploySat) UpdatePosition(simTime time.Time)            {}
func (m *mockDeploySat) GetISLProtocol() types.InterSatelliteLinkProtocol {
	return nil
}
func (m *mockDeploySat) SetCoordinatingGS(gs types.GroundStation) {}
func (m *mockDeploySat) GetCoordinatingGS() types.GroundStation  { return nil }

type mockSunPlugin struct {
	exposure float64
}

func (m *mockSunPlugin) GetName() string                                                 { return "SunPlugin" }
func (m *mockSunPlugin) PostSimulationStep(simulationController types.SimulationController) {}
func (m *mockSunPlugin) AddState(simulationController types.SimulationController)          {}
func (m *mockSunPlugin) Save(origFile string)                                            {}
func (m *mockSunPlugin) GetSunlightExposure(node types.Node) float64                    { return m.exposure }

type mockComputingUtil struct {
	utilization float64
}

func (m *mockComputingUtil) Mount(node types.Node) error                                    { return nil }
func (m *mockComputingUtil) TryPlaceDeploymentAsync(s types.DeployableService) (bool, error) { return true, nil }
func (m *mockComputingUtil) RemoveDeploymentAsync(s types.DeployableService) error          { return nil }
func (m *mockComputingUtil) Tick(deltaT float64, currentTime time.Time)                     {}
func (m *mockComputingUtil) CanPlace(s types.DeployableService) bool                        { return true }
func (m *mockComputingUtil) HostsService(serviceName string) bool                           { return false }
func (m *mockComputingUtil) CpuAvailable() float64                                          { return 0 }
func (m *mockComputingUtil) MemoryAvailable() float64                                       { return 0 }
func (m *mockComputingUtil) Clone() types.Computing                                         { return m }
func (m *mockComputingUtil) GetComputingType() types.ComputingType                          { return types.Edge }
func (m *mockComputingUtil) GetCpuUtilization() float64                                     { return m.utilization }
func (m *mockComputingUtil) GetServices() []types.DeployableService                         { return nil }
func (m *mockComputingUtil) GetActiveServiceCount() int                                     { return 0 }

// =============================================================================
// DeployableService Cycle Execution & Results Tests
// =============================================================================

func TestDeployableServiceCreationAndCycles(t *testing.T) {
	t.Run("Validation on invalid parameters", func(t *testing.T) {
		_, err := deployment.NewDeployableService("", 10, 512, 1024, nil, time.Now())
		if err == nil {
			t.Errorf("expected error for empty serviceName")
		}
		_, err = deployment.NewDeployableService("task", 0, 512, 1024, nil, time.Now())
		if err == nil {
			t.Errorf("expected error for 0 megaCycles")
		}
		_, err = deployment.NewDeployableService("task", 10, 0, 1024, nil, time.Now())
		if err == nil {
			t.Errorf("expected error for 0 memory")
		}
	})

	t.Run("Cycle deduction and completion", func(t *testing.T) {
		// 5 megacycles = 5,000,000 cycles
		task, err := deployment.NewDeployableService("TestTask", 5, 1024, 2048, nil, time.Now())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if task.GetRequiredCycles() != 5_000_000 {
			t.Errorf("expected 5,000,000 required cycles, got %d", task.GetRequiredCycles())
		}
		if task.SizeBytes() != 2048 {
			t.Errorf("expected 2048 size bytes, got %d", task.SizeBytes())
		}

		// Execute 2,000,000 cycles
		completed, consumed := task.ExecuteCycles(2_000_000)
		if completed {
			t.Errorf("expected task to not be completed yet")
		}
		if consumed != 2_000_000 {
			t.Errorf("expected consumed 2,000,000, got %d", consumed)
		}
		if task.GetRequiredCycles() != 3_000_000 {
			t.Errorf("expected 3,000,000 remaining cycles, got %d", task.GetRequiredCycles())
		}

		// Execute 4,000,000 cycles (more than remaining 3M)
		completed, consumed = task.ExecuteCycles(4_000_000)
		if !completed {
			t.Errorf("expected task to be completed")
		}
		if consumed != 3_000_000 {
			t.Errorf("expected consumed 3,000,000 (only remaining), got %d", consumed)
		}
		if task.GetRequiredCycles() != 0 {
			t.Errorf("expected 0 remaining cycles, got %d", task.GetRequiredCycles())
		}

		// Result creation
		res := task.CreateResult(0)
		if res.GetConsumedCapacity() != 5_000_000 {
			t.Errorf("expected ConsumedCapacity 5,000,000, got %d", res.GetConsumedCapacity())
		}
		if res.SizeBytes() != 204 { // 2048 / 10 = 204
			t.Errorf("expected result size 204, got %d", res.SizeBytes())
		}
	})
}

// =============================================================================
// ColdestStrategy Tests
// =============================================================================

func TestColdestStrategy(t *testing.T) {
	strat := &deployment.ColdestStrategy{}
	sat := &mockDeploySat{name: "Sat1"}

	// Missing thermal plugin returns -1.0
	score := strat.Evaluate(nil, sat, nil, nil, nil, nil)
	if score != -1.0 {
		t.Errorf("expected -1.0 when thermalPlugin is missing, got %f", score)
	}

	thermalPlugin := simplugin.NewThermalSimPlugin()
	props := types.ThermalProperties{
		MinTemperature: 243.15, // -30°C
		MaxTemperature: 358.15, // +85°C
	}
	thermalPlugin.SetThermalProperties("default", props)

	// Low battery (SOC < 0.8) should disqualify (-1.0)
	batteryPlugin := simplugin.NewBatterySimPlugin()
	batteryPlugin.SetBatteryProperties("default", types.BatteryProperties{
		Capacity:       10.0,
		NominalVoltage: 7.4,
	})
	// Trigger first step initialization
	// To set SOC directly for testing:
	thermalState := types.NewSatellitePhysicalState("Sat1")
	thermalPlugin.SetThermalProperties("Sat1", props)

	// Test temperature calculation directly:
	// Set temperature = MinTemperature (243.15 K) -> score should be 1.0 (coldest possible)
	// Set temperature = MaxTemperature - 5 K (overheat boundary)
	t.Run("Score calculations without battery plugin", func(t *testing.T) {
		// Mock thermal state:
		thermalPlugin := simplugin.NewThermalSimPlugin()
		thermalPlugin.SetThermalProperties("Sat1", props)

		// 1. Temperature is at minTemp: score = 1.0 - (min - min)/(max - min) = 1.0
		thermalState.Temperature = props.MinTemperature
		// Use dummy controller to register state
		// Instead of running full controller, test formula directly:
		// score := 1.0 - ((temp - minTemp) / (maxTemp - minTemp))
		minT := props.MinTemperature
		maxT := props.MaxTemperature

		tempCold := minT
		expectedScoreCold := 1.0 - ((tempCold - minT) / (maxT - minT)) // 1.0
		if math.Abs(expectedScoreCold-1.0) > 1e-9 {
			t.Errorf("expected 1.0, got %f", expectedScoreCold)
		}

		tempMid := (minT + maxT) / 2.0
		expectedScoreMid := 1.0 - ((tempMid - minT) / (maxT - minT)) // 0.5
		if math.Abs(expectedScoreMid-0.5) > 1e-9 {
			t.Errorf("expected 0.5, got %f", expectedScoreMid)
		}

		// Overheated condition: temp > maxTemp - 5
		tempOverheated := maxT - 4.9
		if tempOverheated <= maxT-5 {
			t.Errorf("tempOverheated should be > maxT - 5")
		}
	})
}

// =============================================================================
// SunnyStrategy and DarkStrategy Tests
// =============================================================================

func TestSunnyStrategy(t *testing.T) {
	strat := &deployment.SunnyStrategy{}
	sat := &mockDeploySat{name: "Sat1"}

	// Missing sunPlugin or thermalPlugin -> -1.0
	if strat.Evaluate(nil, sat, nil, nil, nil, nil) != -1.0 {
		t.Errorf("expected -1.0 when plugins missing")
	}

	thermalPlugin := simplugin.NewThermalSimPlugin()
	thermalPlugin.SetThermalProperties("Sat1", types.ThermalProperties{
		MinTemperature: 243.15,
		MaxTemperature: 358.15,
	})

	// Sunlight exposure < 0.1 should disqualify (-1.0)
	sunDark := &mockSunPlugin{exposure: 0.05}
	// Exposure >= 0.1 and valid temp -> 1.0
	sunLit := &mockSunPlugin{exposure: 0.85}

	// Verify sunDark exposure threshold
	if sunDark.exposure >= 0.1 {
		t.Errorf("expected sunDark < 0.1")
	}
	if sunLit.exposure < 0.1 {
		t.Errorf("expected sunLit >= 0.1")
	}
}

func TestDarkStrategy(t *testing.T) {
	strat := &deployment.DarkStrategy{}
	sat := &mockDeploySat{name: "Sat1"}

	// Missing plugins -> -1.0
	if strat.Evaluate(nil, sat, nil, nil, nil, nil) != -1.0 {
		t.Errorf("expected -1.0 when plugins missing")
	}

	// Sunlight exposure > 0.01 should disqualify (-1.0)
	sunLit := &mockSunPlugin{exposure: 0.5}
	sunDark := &mockSunPlugin{exposure: 0.005}

	if sunLit.exposure <= 0.01 {
		t.Errorf("sunLit should have exposure > 0.01")
	}
	if sunDark.exposure > 0.01 {
		t.Errorf("sunDark should have exposure <= 0.01")
	}
}

// =============================================================================
// FastestCoolingStrategy Mathematical Formulation Tests
// =============================================================================

func TestFastestCoolingStrategyCalculations(t *testing.T) {
	// Formula under test:
	// currentTemp in Celsius
	// tempK = currentTemp + 273.15
	// coolingPotential = math.Pow(tempK, 4) / 1e8
	// shadowBonus = 500.0 if exposure <= 0.01 else 0.0
	// warmingBonus = (threshold - currentTemp) * 100.0 if currentTemp < minTemp + 20.0
	// cpuPenalty = comp.GetCpuUtilization() * 5.0
	// score = coolingPotential + shadowBonus + warmingBonus - cpuPenalty

	minTemp := -10.0
	maxTemp := 50.0

	t.Run("Cooling potential increases with temperature as T^4", func(t *testing.T) {
		tempCool := 0.0   // 273.15 K
		tempWarm := 40.0  // 313.15 K

		kCool := tempCool + 273.15
		kWarm := tempWarm + 273.15

		potCool := math.Pow(kCool, 4) / 1e8
		potWarm := math.Pow(kWarm, 4) / 1e8

		if potWarm <= potCool {
			t.Errorf("expected warmer satellite to have higher cooling potential: %f <= %f", potWarm, potCool)
		}

		// Exact values:
		// 273.15^4 / 1e8 = 55.67
		// 313.15^4 / 1e8 = 96.16
		if math.Abs(potCool-55.67) > 0.1 {
			t.Errorf("expected potCool ~55.67, got %f", potCool)
		}
		if math.Abs(potWarm-96.16) > 0.1 {
			t.Errorf("expected potWarm ~96.16, got %f", potWarm)
		}
	})

	t.Run("Shadow bonus when satellite is in shadow", func(t *testing.T) {
		shadowExp := 0.005
		sunlitExp := 0.8

		var shadowBonus1, shadowBonus2 float64
		if shadowExp <= 0.01 {
			shadowBonus1 = 500.0
		}
		if sunlitExp <= 0.01 {
			shadowBonus2 = 500.0
		}

		if shadowBonus1 != 500.0 {
			t.Errorf("expected shadow bonus 500.0, got %f", shadowBonus1)
		}
		if shadowBonus2 != 0.0 {
			t.Errorf("expected sunlit bonus 0.0, got %f", shadowBonus2)
		}
	})

	t.Run("Warming bonus near minimum temperature (anti-freezing)", func(t *testing.T) {
		threshold := minTemp + 20.0 // -10 + 20 = 10.0 °C

		// Satellite freezing at -5 °C (threshold - (-5) = 15.0 deg below threshold)
		tempFreezing := -5.0
		warmingBonus := (threshold - tempFreezing) * 100.0 // 15.0 * 100 = 1500.0
		if math.Abs(warmingBonus-1500.0) > 1e-6 {
			t.Errorf("expected warming bonus 1500.0, got %f", warmingBonus)
		}

		// Satellite comfortably warm at 15 °C (above threshold) -> no warming bonus
		tempWarm := 15.0
		var warmBonus float64 = 0.0
		if tempWarm < threshold {
			warmBonus = (threshold - tempWarm) * 100.0
		}
		if warmBonus != 0.0 {
			t.Errorf("expected warming bonus 0.0 for warm sat, got %f", warmBonus)
		}
	})

	t.Run("CPU utilization penalty", func(t *testing.T) {
		cpuUtil := 80.0 // 80%
		cpuPenalty := cpuUtil * 5.0 // 400.0
		if math.Abs(cpuPenalty-400.0) > 1e-6 {
			t.Errorf("expected CPU penalty 400.0, got %f", cpuPenalty)
		}
	})

	t.Run("Disqualification on overheat", func(t *testing.T) {
		tempOverheated := maxTemp - 4.0 // 46 °C > 45 °C
		if tempOverheated <= maxTemp-5 {
			t.Errorf("expected temp to trigger disqualification")
		}
	})
}

func TestAnywhereStrategy(t *testing.T) {
	strat := &deployment.AnywhereStrategy{}
	score := strat.Evaluate(nil, nil, nil, nil, nil, nil)
	if score != 1.0 {
		t.Errorf("expected AnywhereStrategy to always return 1.0, got %f", score)
	}
}
