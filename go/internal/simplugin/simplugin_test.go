package simplugin

import (
	"math"
	"testing"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/internal/computing"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// mockSimNode implements types.Satellite and types.Node
type mockSimNode struct {
	name      string
	computing types.Computing
	proto     types.LinkNodeProtocol
}

func (m *mockSimNode) GetName() string                          { return m.name }
func (m *mockSimNode) GetPosition() types.Vector                { return types.Vector{} }
func (m *mockSimNode) DistanceTo(other types.Node) float64      { return 0 }
func (m *mockSimNode) GetRouter() types.Router                  { return nil }
func (m *mockSimNode) GetComputing() types.Computing            { return m.computing }
func (m *mockSimNode) GetLinkNodeProtocol() types.LinkNodeProtocol {
	if m.proto != nil {
		return m.proto
	}
	return &mockNodeLinkProto{}
}
func (m *mockSimNode) UpdatePosition(simTime time.Time) {}
func (m *mockSimNode) GetISLProtocol() types.InterSatelliteLinkProtocol {
	return nil
}
func (m *mockSimNode) SetCoordinatingGS(gs types.GroundStation) {}
func (m *mockSimNode) GetCoordinatingGS() types.GroundStation  { return nil }

type mockNodeLinkProto struct {
	links []types.Link
}

func (m *mockNodeLinkProto) Mount(node types.Node)                  {}
func (m *mockNodeLinkProto) AddLink(link types.Link)                {}
func (m *mockNodeLinkProto) ConnectLink(link types.Link) error       { return nil }
func (m *mockNodeLinkProto) DisconnectLink(link types.Link) error    { return nil }
func (m *mockNodeLinkProto) UpdateLinks() ([]types.Link, error)     { return nil, nil }
func (m *mockNodeLinkProto) Established() []types.Link              { return m.links }
func (m *mockNodeLinkProto) Links() []types.Link                    { return m.links }

type mockLinkWithTraffic struct {
	traffic uint64
}

func (m *mockLinkWithTraffic) Distance() float64                   { return 0 }
func (m *mockLinkWithTraffic) Latency() float64                    { return 0 }
func (m *mockLinkWithTraffic) Bandwidth() float64                  { return 0 }
func (m *mockLinkWithTraffic) IsReachable() bool                   { return true }
func (m *mockLinkWithTraffic) GetOther(self types.Node) types.Node { return nil }
func (m *mockLinkWithTraffic) Involves(node types.Node) bool       { return false }
func (m *mockLinkWithTraffic) Nodes() (types.Node, types.Node)     { return nil, nil }
func (m *mockLinkWithTraffic) AddTraffic(bytes uint64)             {}
func (m *mockLinkWithTraffic) GetTraffic() uint64                  { return m.traffic }
func (m *mockLinkWithTraffic) ResetTraffic()                       { m.traffic = 0 }

type mockThermalEnv struct {
	envHeat types.EnvironmentalHeat
}

func (m *mockThermalEnv) GetEnvironmentalHeat(node types.Node) types.EnvironmentalHeat {
	return m.envHeat
}

// =============================================================================
// BatterySimPlugin Calculation Tests
// =============================================================================

func TestBatteryPowerConsumptionCalculation(t *testing.T) {
	plugin := NewBatterySimPlugin()

	powerProps := types.PowerProperties{
		IdlePowerConsumption: 2.0,    // 2 W
		IslEnergyPerByte:     1.0e-7, // 100 nJ/byte
	}

	t.Run("Idle satellite power consumption", func(t *testing.T) {
		sat := &mockSimNode{name: "IdleSat", computing: nil}
		power := plugin.calculatePowerConsumption(sat, powerProps, 1.0)
		if math.Abs(power-powerProps.IdlePowerConsumption) > 1e-9 {
			t.Errorf("expected idle power %f, got %f", powerProps.IdlePowerConsumption, power)
		}
	})

	t.Run("Power consumption with CPU and Memory usage", func(t *testing.T) {
		comp := computing.NewComputing(4.0, 16000.0, types.Edge)
		comp.CpuUsage = 1.5       // 1.5 cores active
		comp.MemoryUsage = 4000.0 // 4000 MB reserved

		sat := &mockSimNode{name: "ActiveSat", computing: comp}

		// Expected formula:
		// basePower = 2.0
		// cpuPower = 1.5 * 10.0 = 15.0 W
		// memoryPower = 4000.0 * 0.1 = 400.0 W
		// total = 2.0 + 15.0 + 400.0 = 417.0 W
		expected := 2.0 + (1.5 * 10.0) + (4000.0 * 0.1)
		power := plugin.calculatePowerConsumption(sat, powerProps, 1.0)
		if math.Abs(power-expected) > 1e-9 {
			t.Errorf("expected power %f W, got %f W", expected, power)
		}
	})

	t.Run("Power consumption with network traffic", func(t *testing.T) {
		deltaT := 2.0 // 2 seconds
		trafficBytes := uint64(10_000_000) // 10 MB transferred
		link := &mockLinkWithTraffic{traffic: trafficBytes}
		proto := &mockNodeLinkProto{links: []types.Link{link}}

		comp := computing.NewComputing(1.0, 1000.0, types.Edge)
		sat := &mockSimNode{name: "NetSat", computing: comp, proto: proto}

		// networkPower = (traffic * IslEnergyPerByte) / deltaT
		// = (10,000,000 * 1.0e-7) / 2.0 = 1.0 / 2.0 = 0.5 W
		expectedNetPower := (float64(trafficBytes) * powerProps.IslEnergyPerByte) / deltaT // 0.5 W
		expectedTotal := powerProps.IdlePowerConsumption + expectedNetPower               // 2.5 W

		power := plugin.calculatePowerConsumption(sat, powerProps, deltaT)
		if math.Abs(power-expectedTotal) > 1e-9 {
			t.Errorf("expected power with network %f W, got %f W", expectedTotal, power)
		}
	})
}

func TestBatteryPowerGenerationCalculation(t *testing.T) {
	plugin := NewBatterySimPlugin()

	powerProps := types.PowerProperties{
		MaxPowerGeneration: 40.0, // 40 W
		SolarEfficiency:    0.28, // 28%
	}

	t.Run("Zero generation in eclipse", func(t *testing.T) {
		env := &mockThermalEnv{
			envHeat: types.EnvironmentalHeat{
				InEclipse:        true,
				SunlightExposure: 0.0,
			},
		}
		plugin.SetThermalPlugin(env)

		sat := &mockSimNode{name: "EclipseSat"}
		gen := plugin.calculatePowerGeneration(sat, powerProps)
		if gen != 0.0 {
			t.Errorf("expected 0 generation in eclipse, got %f", gen)
		}
	})

	t.Run("Generation in partial sunlight", func(t *testing.T) {
		exposure := 0.75
		env := &mockThermalEnv{
			envHeat: types.EnvironmentalHeat{
				InEclipse:        false,
				SunlightExposure: exposure,
			},
		}
		plugin.SetThermalPlugin(env)

		sat := &mockSimNode{name: "SunSat"}
		// Expected: MaxPowerGeneration * exposure * SolarEfficiency
		// = 40.0 * 0.75 * 0.28 = 8.4 W
		expected := powerProps.MaxPowerGeneration * exposure * powerProps.SolarEfficiency
		gen := plugin.calculatePowerGeneration(sat, powerProps)
		if math.Abs(gen-expected) > 1e-9 {
			t.Errorf("expected generation %f W, got %f W", expected, gen)
		}
	})

	t.Run("Precomputed solar power override", func(t *testing.T) {
		precompPower := 25.5
		env := &mockThermalEnv{
			envHeat: types.EnvironmentalHeat{
				InEclipse:            false,
				SolarPowerGeneration: precompPower,
			},
		}
		plugin.SetThermalPlugin(env)

		sat := &mockSimNode{name: "PrecompSat"}
		gen := plugin.calculatePowerGeneration(sat, powerProps)
		if math.Abs(gen-precompPower) > 1e-9 {
			t.Errorf("expected precomputed generation %f W, got %f W", precompPower, gen)
		}
	})
}

func TestBatteryCoulombCountingSoCUpdate(t *testing.T) {
	plugin := NewBatterySimPlugin()

	bProps := types.BatteryProperties{
		Capacity:          10.0, // 10 Ah
		NominalVoltage:    7.4,  // 7.4 V
		CoulombEfficiency: 0.95, // 95%
	}
	pProps := types.PowerProperties{
		IdlePowerConsumption: 2.0,  // 2 W
		MaxPowerGeneration:   40.0, // 40 W
		SolarEfficiency:      0.28, // 28%
	}
	plugin.SetBatteryProperties("default", bProps)
	plugin.SetPowerProperties("default", pProps)

	now := time.Now()
	sat := &mockSimNode{name: "Sat1"}

	t.Run("Charging in sunlight: SoC increases correctly", func(t *testing.T) {
		// Exposure = 1.0 -> gen = 40 * 1 * 0.28 = 11.2 W
		// Idle consumption = 2.0 W
		// Net power = 11.2 - 2.0 = 9.2 W
		// Net current = 9.2 / 7.4 = 1.24324 A
		// deltaT = 3600 s (1 hour)
		// capacityAs = 10 * 3600 = 36000 As
		// deltaSOC = (0.95 * 1.24324 * 3600) / 36000 = (0.95 * 1.24324) / 10 = 0.118108
		env := &mockThermalEnv{
			envHeat: types.EnvironmentalHeat{
				InEclipse:        false,
				SunlightExposure: 1.0,
			},
		}
		plugin.SetThermalPlugin(env)

		state := types.NewSatellitePhysicalState("Sat1")
		state.Timestamp = now
		state.SOC = 0.50 // Start at 50%

		nextTime := now.Add(3600 * time.Second)
		plugin.updateBatteryStateForIndex(sat, state, nextTime)

		expectedNetPower := 11.2 - 2.0
		expectedNetCurrent := expectedNetPower / bProps.NominalVoltage
		expectedDeltaSOC := (bProps.CoulombEfficiency * expectedNetCurrent * 3600.0) / (bProps.Capacity * 3600.0)
		expectedSOC := 0.50 + expectedDeltaSOC

		if math.Abs(state.PowerGeneration-11.2) > 1e-4 {
			t.Errorf("expected PowerGen 11.2, got %f", state.PowerGeneration)
		}
		if math.Abs(state.PowerConsumption-2.0) > 1e-4 {
			t.Errorf("expected PowerCons 2.0, got %f", state.PowerConsumption)
		}
		if math.Abs(state.NetCurrent-expectedNetCurrent) > 1e-4 {
			t.Errorf("expected NetCurrent %f, got %f", expectedNetCurrent, state.NetCurrent)
		}
		if math.Abs(state.SOC-expectedSOC) > 1e-4 {
			t.Errorf("expected SOC %f, got %f", expectedSOC, state.SOC)
		}
	})

	t.Run("Discharging in eclipse: SoC decreases and clamps at 0", func(t *testing.T) {
		env := &mockThermalEnv{
			envHeat: types.EnvironmentalHeat{
				InEclipse: true,
			},
		}
		plugin.SetThermalPlugin(env)

		state := types.NewSatellitePhysicalState("Sat1")
		state.Timestamp = now
		state.SOC = 0.01 // Start at 1%

		// Discharge for 10 hours -> would deplete completely
		nextTime := now.Add(10 * 3600 * time.Second)
		plugin.updateBatteryStateForIndex(sat, state, nextTime)

		if state.SOC < 0.0 || state.SOC > 1.0 {
			t.Errorf("SOC should be clamped between 0 and 1, got %f", state.SOC)
		}
		if state.SOC != 0.0 {
			t.Errorf("expected depleted battery to clamp to 0.0, got %f", state.SOC)
		}
	})

	t.Run("Overcharging clamps SoC at 1.0", func(t *testing.T) {
		env := &mockThermalEnv{
			envHeat: types.EnvironmentalHeat{
				InEclipse:        false,
				SunlightExposure: 1.0,
			},
		}
		plugin.SetThermalPlugin(env)

		state := types.NewSatellitePhysicalState("Sat1")
		state.Timestamp = now
		state.SOC = 0.99 // Start at 99%

		// Charge for 10 hours -> clamps to 1.0
		nextTime := now.Add(10 * 3600 * time.Second)
		plugin.updateBatteryStateForIndex(sat, state, nextTime)

		if state.SOC != 1.0 {
			t.Errorf("expected fully charged battery to clamp to 1.0, got %f", state.SOC)
		}
	})
}

// =============================================================================
// ThermalSimPlugin Stefan-Boltzmann Radiation & Differential Equation Tests
// =============================================================================

func TestThermalRadiationCalculation(t *testing.T) {
	plugin := NewThermalSimPlugin()

	thermalProps := types.ThermalProperties{
		Emissivity:  0.85,
		SurfaceArea: 0.14, // m²
	}

	// Stefan-Boltzmann radiation: Q_out = emissivity * sigma * surfaceArea * (T^4)
	// For T = 300 K:
	// sigma = 5.670374419e-8
	// T^4 = 8.1e9
	// Q_out = 0.85 * 5.670374419e-8 * 0.14 * 8.1e9 = 54.6666 W
	temp := 300.0
	expectedQout := thermalProps.Emissivity * types.StefanBoltzmannConstant * thermalProps.SurfaceArea * math.Pow(temp, 4)

	qOut := plugin.calculateHeatOutput(temp, thermalProps)
	if math.Abs(qOut-expectedQout) > 1e-6 {
		t.Errorf("calculateHeatOutput(%f): expected %f W, got %f W", temp, expectedQout, qOut)
	}

	// Double check T^4 proportionality: at 2T, heat output should be 16x
	qOutDouble := plugin.calculateHeatOutput(temp*2.0, thermalProps)
	if math.Abs((qOutDouble/qOut)-16.0) > 1e-4 {
		t.Errorf("expected radiation to scale by 16x at 2T, got ratio %f", qOutDouble/qOut)
	}
}

func TestThermalDifferentialEquationIntegration(t *testing.T) {
	plugin := NewThermalSimPlugin()
	plugin.SetTimeStep(1.0) // 1 second sub-step

	tProps := types.ThermalProperties{
		ThermalMass: 3500.0, // J/K
		Emissivity:  0.85,
		SurfaceArea: 0.14,
	}
	plugin.SetThermalProperties("default", tProps)

	now := time.Now()
	sat := &mockSimNode{name: "SatThermal"}

	t.Run("Thermal equilibrium condition", func(t *testing.T) {
		// Set temperature to 300 K
		initTemp := 300.0
		qRad := plugin.calculateHeatOutput(initTemp, tProps)

		// Environmental heat exactly equals radiated heat (Q_in = Q_out)
		env := &mockThermalEnv{
			envHeat: types.EnvironmentalHeat{
				TotalHeat: qRad,
			},
		}
		plugin.SetThermalEnvironmentPlugin(env)

		state := types.NewSatellitePhysicalState("SatThermal")
		state.Timestamp = now
		state.Temperature = initTemp

		// Advance 10 seconds
		plugin.updateThermalStateForIndex(sat, state, now.Add(10*time.Second))

		// In equilibrium, temperature should remain virtually unchanged
		if math.Abs(state.Temperature-initTemp) > 1e-4 {
			t.Errorf("thermal equilibrium violated: expected %f K, got %f K", initTemp, state.Temperature)
		}
	})

	t.Run("Heating when Q_in > Q_out", func(t *testing.T) {
		initTemp := 300.0
		qRad := plugin.calculateHeatOutput(initTemp, tProps)

		// Surplus heat of 350 W
		surplusHeat := 350.0
		env := &mockThermalEnv{
			envHeat: types.EnvironmentalHeat{
				TotalHeat: qRad + surplusHeat,
			},
		}
		plugin.SetThermalEnvironmentPlugin(env)

		state := types.NewSatellitePhysicalState("SatThermal")
		state.Timestamp = now
		state.Temperature = initTemp

		// Advance 10 seconds with sub-step 1.0s
		// dT ~ (surplusHeat / thermalMass) * deltaT
		// = (350 / 3500) * 10 = 1.0 K
		plugin.updateThermalStateForIndex(sat, state, now.Add(10*time.Second))

		if state.Temperature <= initTemp {
			t.Errorf("expected temperature to rise with surplus heat, but got %f K (initial %f)", state.Temperature, initTemp)
		}

		deltaTemp := state.Temperature - initTemp
		// Close to 1.0 K (slightly less due to increased radiation as temperature rises)
		if deltaTemp < 0.95 || deltaTemp > 1.05 {
			t.Errorf("expected deltaTemp ~1.0 K, got %f K", deltaTemp)
		}
	})

	t.Run("Cooling in eclipse (radiation into cold space)", func(t *testing.T) {
		initTemp := 323.15 // 50°C in Kelvin
		// Total environmental heat = 0
		env := &mockThermalEnv{
			envHeat: types.EnvironmentalHeat{
				TotalHeat: 0.0,
			},
		}
		plugin.SetThermalEnvironmentPlugin(env)

		state := types.NewSatellitePhysicalState("SatThermal")
		state.Timestamp = now
		state.Temperature = initTemp

		// Radiating for 60 seconds
		plugin.updateThermalStateForIndex(sat, state, now.Add(60*time.Second))

		if state.Temperature >= initTemp {
			t.Errorf("expected satellite to cool down in shadow, got %f K", state.Temperature)
		}
	})
}
