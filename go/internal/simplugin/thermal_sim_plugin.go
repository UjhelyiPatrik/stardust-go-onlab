package simplugin

import (
	"fmt"
	"sync"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/internal/computing"
	"github.com/polaris-slo-cloud/stardust-go/internal/node"
	"github.com/polaris-slo-cloud/stardust-go/pkg/helper"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.SimulationPlugin = (*ThermalSimPlugin)(nil)

// ThermalSimPlugin simulates thermal dynamics for satellites using multi-threaded execution.
type ThermalSimPlugin struct {
	mu               sync.RWMutex
	thermalStates    map[string]*types.SatellitePhysicalState
	orderedStates    []*types.SatellitePhysicalState
	thermalProps     map[string]types.ThermalProperties
	thermalEnvPlugin ThermalEnvironmentPlugin
	batteryPlugin    BatterySimPluginInterface
	timeStep         float64
	enableFeedback   bool
	lastSimTime      time.Time
}

type BatterySimPluginInterface interface {
	GetBatteryState(node types.Node) (*types.SatellitePhysicalState, error)
}

func NewThermalSimPlugin() *ThermalSimPlugin {
	return &ThermalSimPlugin{
		thermalStates:  make(map[string]*types.SatellitePhysicalState),
		thermalProps:   make(map[string]types.ThermalProperties),
		timeStep:       1.0,
		enableFeedback: true,
	}
}

func (p *ThermalSimPlugin) Name() string {
	return "ThermalSimPlugin"
}

func (p *ThermalSimPlugin) SetThermalEnvironmentPlugin(plugin ThermalEnvironmentPlugin) {
	p.thermalEnvPlugin = plugin
}

func (p *ThermalSimPlugin) SetBatteryPlugin(plugin BatterySimPluginInterface) {
	p.batteryPlugin = plugin
}

func (p *ThermalSimPlugin) SetThermalProperties(nodeType string, props types.ThermalProperties) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.thermalProps[nodeType] = props
}

func (p *ThermalSimPlugin) GetThermalProperties(n types.Node) types.ThermalProperties {
	if props, ok := p.thermalProps[n.GetName()]; ok {
		return props
	}
	if props, ok := p.thermalProps["default"]; ok {
		return props
	}
	return types.DefaultThermalProperties()
}

func (p *ThermalSimPlugin) SetTimeStep(dt float64) {
	p.timeStep = dt
}

func (p *ThermalSimPlugin) SetEnableFeedback(enable bool) {
	p.enableFeedback = enable
}

// PostSimulationStep updates thermal states for all satellites concurrently across all CPU cores.
func (p *ThermalSimPlugin) PostSimulationStep(simulation types.SimulationController) error {
	nodes := simulation.GetSatellites()
	if len(nodes) == 0 {
		return nil
	}
	simTime := simulation.GetSimulationTime()

	// Safely resolve Thermal Environment Plugin once before launching parallel workers
	if p.thermalEnvPlugin == nil {
		repo := simulation.GetStatePluginRepository()
		if repo != nil {
			for _, sp := range repo.GetAllPlugins() {
				if envPlugin, ok := sp.(ThermalEnvironmentPlugin); ok {
					p.thermalEnvPlugin = envPlugin
					break
				}
			}
		}
	}

	// Pre-allocate state pointers on the first simulation step
	if len(p.orderedStates) != len(nodes) {
		p.mu.Lock()
		p.orderedStates = make([]*types.SatellitePhysicalState, len(nodes))
		for i, n := range nodes {
			name := n.GetName()
			st, exists := p.thermalStates[name]
			if !exists {
				st = types.NewSatellitePhysicalState(name)
				st.Timestamp = simTime
				st.Temperature = 323.15 // Initialize to 50°C in Kelvin
				p.thermalStates[name] = st
			}
			p.orderedStates[i] = st
		}
		p.mu.Unlock()
		return nil
	}

	// Multi-threaded lock-free differential equation integration across all satellites
	indices := make([]int, len(nodes))
	for i := range nodes {
		indices[i] = i
	}

	helper.ParallelFor(indices, func(i int) {
		p.updateThermalStateForIndex(nodes[i], p.orderedStates[i], simTime)
	})

	return nil
}

func (p *ThermalSimPlugin) updateThermalStateForIndex(n types.Node, state *types.SatellitePhysicalState, simTime time.Time) {
	deltaT := simTime.Sub(state.Timestamp).Seconds()
	if deltaT <= 0 {
		return
	}

	thermalProps := p.getThermalProperties(n)
	thermalMass := thermalProps.ThermalMass
	if thermalMass <= 0 {
		thermalMass = 3500.0
	}

	// Fetch environmental heat ONCE per satellite per step (O(1) in precomputed mode)
	envHeat := p.resolveEnvironmentalHeat(n)
	heatGeneration := p.calculateInternalHeatGeneration(n)

	maxStepSize := p.timeStep
	if maxStepSize <= 0 {
		maxStepSize = 1.0
	}

	numSteps := int(deltaT / maxStepSize)
	remainder := deltaT - (float64(numSteps) * maxStepSize)

	currentTemp := state.Temperature

	for i := 0; i < numSteps; i++ {
		heatOutput := p.calculateHeatOutput(currentTemp, thermalProps)
		currentTemp += (maxStepSize / thermalMass) * (envHeat.TotalHeat + heatGeneration - heatOutput)
	}

	if remainder > 0 {
		heatOutput := p.calculateHeatOutput(currentTemp, thermalProps)
		currentTemp += (remainder / thermalMass) * (envHeat.TotalHeat + heatGeneration - heatOutput)
	}

	state.Temperature = currentTemp
	state.EnvironmentalHeat = envHeat
	state.Timestamp = simTime
}

func (p *ThermalSimPlugin) resolveEnvironmentalHeat(n types.Node) types.EnvironmentalHeat {
	if pSat, ok := n.(*node.PrecomputedSatellite); ok {
		if st, hasState := pSat.GetCurrentPrecomputedState(); hasState {
			return types.EnvironmentalHeat{
				SolarHeat:            st.SolarHeat,
				AlbedoHeat:           st.AlbedoHeat,
				IRHeat:               st.IRHeat,
				TotalHeat:            st.TotalEnvironmentalHeat,
				SunlightExposure:     st.SunlightExposure,
				BetaAngle:            st.BetaAngle,
				InEclipse:            st.InEclipse,
				EclipseDepth:         st.EclipseDepth,
				SolarPowerGeneration: st.SolarPowerGeneration,
			}
		}
	}
	if p.thermalEnvPlugin != nil {
		return p.thermalEnvPlugin.GetEnvironmentalHeat(n)
	}
	return types.EnvironmentalHeat{}
}

func (p *ThermalSimPlugin) calculateInternalHeatGeneration(n types.Node) float64 {
	if p.batteryPlugin == nil {
		compNode := n.GetComputing()
		if compNode == nil {
			return 0
		}
		if comp, ok := compNode.(*computing.Computing); ok {
			return comp.CpuUsage * 10.0
		}
		return 0
	}

	state, err := p.batteryPlugin.GetBatteryState(n)
	if err != nil {
		return 0
	}

	return state.PowerConsumption
}

func (p *ThermalSimPlugin) calculateHeatOutput(temperature float64, thermalProps types.ThermalProperties) float64 {
	emissivity := thermalProps.Emissivity
	surfaceArea := thermalProps.SurfaceArea
	sigma := types.StefanBoltzmannConstant

	if temperature <= 0 {
		temperature = 1.0
	}

	t2 := temperature * temperature
	return emissivity * sigma * surfaceArea * (t2 * t2)
}

func (p *ThermalSimPlugin) getThermalProperties(n types.Node) types.ThermalProperties {
	if props, ok := p.thermalProps[n.GetName()]; ok {
		return props
	}
	if props, ok := p.thermalProps["default"]; ok {
		return props
	}
	return types.DefaultThermalProperties()
}

func (p *ThermalSimPlugin) GetThermalState(n types.Node) (*types.SatellitePhysicalState, error) {
	if pSat, ok := n.(*node.PrecomputedSatellite); ok {
		if pSat.SatIndex >= 0 && pSat.SatIndex < len(p.orderedStates) {
			return p.orderedStates[pSat.SatIndex], nil
		}
	}
	p.mu.RLock()
	state, ok := p.thermalStates[n.GetName()]
	p.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("thermal state not found for node %s", n.GetName())
	}
	return state, nil
}

func (p *ThermalSimPlugin) GetTemperature(n types.Node) (float64, error) {
	state, err := p.GetThermalState(n)
	if err != nil {
		return 0, err
	}
	return state.Temperature, nil
}

func (p *ThermalSimPlugin) GetTemperatureCelsius(n types.Node) (float64, error) {
	tempK, err := p.GetTemperature(n)
	if err != nil {
		return 0, err
	}
	return tempK - 273.15, nil
}

func (p *ThermalSimPlugin) IsOverheating(n types.Node) bool {
	state, err := p.GetThermalState(n)
	if err != nil {
		return false
	}
	thermalProps := p.getThermalProperties(n)
	return state.Temperature > (thermalProps.MaxTemperature - 10.0)
}

func (p *ThermalSimPlugin) IsHypothermia(n types.Node) bool {
	state, err := p.GetThermalState(n)
	if err != nil {
		return false
	}
	thermalProps := p.getThermalProperties(n)
	return state.Temperature < thermalProps.MinTemperature*1.2
}

func (p *ThermalSimPlugin) GetAllStates() map[string]*types.SatellitePhysicalState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.thermalStates
}

func (p *ThermalSimPlugin) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.thermalStates = make(map[string]*types.SatellitePhysicalState)
	p.orderedStates = nil
}

func (p *ThermalSimPlugin) GetEffectiveCapacity(n types.Node) float64 {
	if !p.enableFeedback {
		return 1.0
	}

	state, err := p.GetThermalState(n)
	if err != nil {
		return 1.0
	}

	temp := state.Temperature
	efficiency := 1.0
	if temp < 283.15 {
		efficiency = 0.5 + 0.5*(temp-263.15)/20.0
	} else if temp > 313.15 {
		efficiency = 1.0 - 0.001*(temp-313.15)
	}

	if efficiency < 0.1 {
		efficiency = 0.1
	}
	if efficiency > 1.0 {
		efficiency = 1.0
	}

	return efficiency
}

func (p *ThermalSimPlugin) GetInternalResistance(n types.Node) float64 {
	if !p.enableFeedback {
		return 0.1
	}

	state, err := p.GetThermalState(n)
	if err != nil {
		return 0.1
	}

	baseResistance := 0.1
	temp := state.Temperature

	if temp < 273.15 {
		return baseResistance * 2.0
	} else if temp < 293.15 {
		return baseResistance * 1.5
	}

	return baseResistance
}
