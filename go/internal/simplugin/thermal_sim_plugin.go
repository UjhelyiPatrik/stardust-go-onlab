package simplugin

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/internal/computing"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.SimulationPlugin = (*ThermalSimPlugin)(nil)

// ThermalSimPlugin simulates thermal dynamics for satellites
type ThermalSimPlugin struct {
	thermalStates    map[string]*types.SatellitePhysicalState
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
	p.thermalProps[nodeType] = props
}

func (p *ThermalSimPlugin) GetThermalProperties(node types.Node) types.ThermalProperties {
	if props, ok := p.thermalProps[node.GetName()]; ok {
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

// PostSimulationStep updates thermal states for all satellites
func (p *ThermalSimPlugin) PostSimulationStep(simulation types.SimulationController) error {
	nodes := simulation.GetSatellites()
	simTime := simulation.GetSimulationTime()

	for _, node := range nodes {
		p.updateThermalState(node, simTime, simulation)
	}

	// Calculate and print temperature statistics
	if len(p.thermalStates) > 0 {
		temperatures := make([]float64, 0, len(p.thermalStates))

		var minTemp, maxTemp float64
		var minNodeName, maxNodeName string
		sumTemp := 0.0
		first := true

		for name, state := range p.thermalStates {
			tempC := state.Temperature - 273.15 // Convert to Celsius
			temperatures = append(temperatures, tempC)

			// Inicializing min/max values with the first entry
			if first {
				minTemp = tempC
				maxTemp = tempC
				minNodeName = name
				maxNodeName = name
				first = false
			} else {
				// finding minimum and maximum temperatures with node names
				if tempC < minTemp {
					minTemp = tempC
					minNodeName = name
				}
				if tempC > maxTemp {
					maxTemp = tempC
					maxNodeName = name
				}
			}
			sumTemp += tempC
		}

		avgTemp := sumTemp / float64(len(temperatures))

		// Calculate median
		sort.Float64s(temperatures)
		var medianTemp float64
		n := len(temperatures)
		if n%2 == 0 {
			medianTemp = (temperatures[n/2-1] + temperatures[n/2]) / 2.0
		} else {
			medianTemp = temperatures[n/2]
		}

		// Print to console with node names
		fmt.Printf("\n=== Thermal Statistics (Simulation Time: %v) ===\n", simTime)
		fmt.Printf("Maximum Temperature: %.2f°C (%s)\n", maxTemp, maxNodeName)
		fmt.Printf("Minimum Temperature: %.2f°C (%s)\n", minTemp, minNodeName)
		fmt.Printf("Average Temperature: %.2f°C\n", avgTemp)
		fmt.Printf("Median Temperature: %.2f°C\n", medianTemp)
		fmt.Println("==================================================\n ")
	}

	return nil
}

func (p *ThermalSimPlugin) updateThermalState(node types.Node, simTime time.Time, simulation types.SimulationController) {
	nodeName := node.GetName()

	state, ok := p.thermalStates[nodeName]
	if !ok {
		state = types.NewSatellitePhysicalState(nodeName)
		state.Timestamp = simTime
		state.Temperature = 323.15 // Initialize to 50°C in Kelvin
		p.thermalStates[nodeName] = state
		return
	}

	deltaT := simTime.Sub(state.Timestamp).Seconds()
	if deltaT <= 0 {
		return
	}

	// Safely Resolve Thermal Environment Plugin from repository at runtime
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

	thermalProps := p.getThermalProperties(node)
	thermalMass := thermalProps.ThermalMass
	if thermalMass <= 0 {
		thermalMass = 3500.0
	}

	maxStepSize := p.timeStep
	if maxStepSize <= 0 {
		maxStepSize = 1.0
	}

	numSteps := int(deltaT / maxStepSize)
	remainder := deltaT - (float64(numSteps) * maxStepSize)

	currentTemp := state.Temperature

	for i := 0; i < numSteps; i++ {
		currentTemp = p.integrateOneStep(node, currentTemp, maxStepSize, thermalMass, thermalProps)
	}

	if remainder > 0 {
		currentTemp = p.integrateOneStep(node, currentTemp, remainder, thermalMass, thermalProps)
	}

	var envHeat types.EnvironmentalHeat
	if p.thermalEnvPlugin != nil {
		envHeat = p.thermalEnvPlugin.GetEnvironmentalHeat(node)
	}

	state.Temperature = currentTemp
	state.EnvironmentalHeat = envHeat
	state.Timestamp = simTime
}

func (p *ThermalSimPlugin) integrateOneStep(node types.Node, currentTemp float64, dt float64, thermalMass float64, thermalProps types.ThermalProperties) float64 {
	var envHeat types.EnvironmentalHeat
	if p.thermalEnvPlugin != nil {
		envHeat = p.thermalEnvPlugin.GetEnvironmentalHeat(node)
	}

	heatGeneration := p.calculateInternalHeatGeneration(node)
	heatOutput := p.calculateHeatOutput(currentTemp, thermalProps)
	dT := (dt / thermalMass) * (envHeat.TotalHeat + heatGeneration - heatOutput)

	return currentTemp + dT
}

func (p *ThermalSimPlugin) calculateInternalHeatGeneration(node types.Node) float64 {
	if p.batteryPlugin == nil {
		compNode := node.GetComputing()
		if compNode == nil {
			return 0
		}
		if comp, ok := compNode.(*computing.Computing); ok {
			return comp.CpuUsage * 10.0
		}
		return 0
	}

	state, err := p.batteryPlugin.GetBatteryState(node)
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

	return emissivity * sigma * surfaceArea * math.Pow(temperature, 4)
}

func (p *ThermalSimPlugin) getThermalProperties(node types.Node) types.ThermalProperties {
	if props, ok := p.thermalProps[node.GetName()]; ok {
		return props
	}
	if props, ok := p.thermalProps["default"]; ok {
		return props
	}
	return types.DefaultThermalProperties()
}

func (p *ThermalSimPlugin) GetThermalState(node types.Node) (*types.SatellitePhysicalState, error) {
	state, ok := p.thermalStates[node.GetName()]
	if !ok {
		return nil, fmt.Errorf("thermal state not found for node %s", node.GetName())
	}
	return state, nil
}

func (p *ThermalSimPlugin) GetTemperature(node types.Node) (float64, error) {
	state, err := p.GetThermalState(node)
	if err != nil {
		return 0, err
	}
	return state.Temperature, nil
}

func (p *ThermalSimPlugin) GetTemperatureCelsius(node types.Node) (float64, error) {
	tempK, err := p.GetTemperature(node)
	if err != nil {
		return 0, err
	}
	return tempK - 273.15, nil
}

func (p *ThermalSimPlugin) IsOverheating(node types.Node) bool {
	state, err := p.GetThermalState(node)
	if err != nil {
		return false
	}
	thermalProps := p.getThermalProperties(node)

	// Consider overheating if the temperature is within 10 degrees of the maximum threshold
	return state.Temperature > (thermalProps.MaxTemperature - 10.0)
}

func (p *ThermalSimPlugin) IsHypothermia(node types.Node) bool {
	state, err := p.GetThermalState(node)
	if err != nil {
		return false
	}
	thermalProps := p.getThermalProperties(node)
	return state.Temperature < thermalProps.MinTemperature*1.2
}

func (p *ThermalSimPlugin) GetAllStates() map[string]*types.SatellitePhysicalState {
	return p.thermalStates
}

func (p *ThermalSimPlugin) Reset() {
	p.thermalStates = make(map[string]*types.SatellitePhysicalState)
}

func (p *ThermalSimPlugin) GetEffectiveCapacity(node types.Node) float64 {
	if !p.enableFeedback {
		return 1.0
	}

	state, err := p.GetThermalState(node)
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

func (p *ThermalSimPlugin) GetInternalResistance(node types.Node) float64 {
	if !p.enableFeedback {
		return 0.1
	}

	state, err := p.GetThermalState(node)
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
