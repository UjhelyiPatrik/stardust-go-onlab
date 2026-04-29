package simplugin

import (
	"fmt"
	"math"
	"time"

	"github.com/keniack/stardustGo/internal/computing"
	"github.com/keniack/stardustGo/pkg/types"
)

var _ types.SimulationPlugin = (*ThermalSimPlugin)(nil)

// ThermalSimPlugin simulates thermal dynamics for satellites
type ThermalSimPlugin struct {
	// Thermal states: node name -> physical state
	thermalStates map[string]*types.SatellitePhysicalState

	// Thermal properties per node type
	thermalProps map[string]types.ThermalProperties

	// Reference to thermal environment plugin
	thermalEnvPlugin ThermalEnvironmentPlugin

	// Reference to battery plugin
	batteryPlugin BatterySimPluginInterface

	// Time step in seconds
	timeStep float64

	// Enable cyber-physical feedback
	enableFeedback bool
}

// BatterySimPluginInterface defines the interface for battery plugin access
type BatterySimPluginInterface interface {
	GetBatteryState(node types.Node) (*types.SatellitePhysicalState, error)
}

// NewThermalSimPlugin creates a new thermal simulation plugin
func NewThermalSimPlugin() *ThermalSimPlugin {
	return &ThermalSimPlugin{
		thermalStates:     make(map[string]*types.SatellitePhysicalState),
		thermalProps:      make(map[string]types.ThermalProperties),
		timeStep:          1.0, // Default 1 second time step
		enableFeedback:   true, // Enable cyber-physical feedback by default
	}
}

// Name returns the plugin name
func (p *ThermalSimPlugin) Name() string {
	return "ThermalSimPlugin"
}

// SetThermalEnvironmentPlugin sets the thermal environment plugin reference
func (p *ThermalSimPlugin) SetThermalEnvironmentPlugin(plugin ThermalEnvironmentPlugin) {
	p.thermalEnvPlugin = plugin
}

// SetBatteryPlugin sets the battery plugin reference for cyber-physical feedback
func (p *ThermalSimPlugin) SetBatteryPlugin(plugin BatterySimPluginInterface) {
	p.batteryPlugin = plugin
}

// SetThermalProperties sets the thermal properties for a node type
func (p *ThermalSimPlugin) SetThermalProperties(nodeType string, props types.ThermalProperties) {
	p.thermalProps[nodeType] = props
}

// SetTimeStep sets the simulation time step
func (p *ThermalSimPlugin) SetTimeStep(dt float64) {
	p.timeStep = dt
}

// SetEnableFeedback enables or disables cyber-physical feedback
func (p *ThermalSimPlugin) SetEnableFeedback(enable bool) {
	p.enableFeedback = enable
}

// PostSimulationStep updates thermal states for all satellites
func (p *ThermalSimPlugin) PostSimulationStep(simulation types.SimulationController) error {
	nodes := simulation.GetSatellites()
	simTime := simulation.GetSimulationTime()

	for _, node := range nodes {
		p.updateThermalState(node, simTime)
	}

	return nil
}

// updateThermalState updates the thermal state for a single satellite
func (p *ThermalSimPlugin) updateThermalState(node types.Node, simTime time.Time) {
	nodeName := node.GetName()

	// Get or create thermal state
	state, ok := p.thermalStates[nodeName]
	if !ok {
		state = types.NewSatellitePhysicalState(nodeName)
		p.thermalStates[nodeName] = state
	}

	// Get thermal properties
	thermalProps := p.getThermalProperties(node)

	// Get environmental heat input
	var envHeat types.EnvironmentalHeat
	if p.thermalEnvPlugin != nil {
		envHeat = p.thermalEnvPlugin.GetEnvironmentalHeat(node)
	}

	// Get internal heat generation (from power consumption)
	heatGeneration := p.calculateInternalHeatGeneration(node)

	// Calculate heat output (radiation)
	heatOutput := p.calculateHeatOutput(state.Temperature, thermalProps)

	// Apply thermal energy balance equation using Euler method
	// T_new = T_old + (Δt / (m * cp)) * (Q_in + Q_gen - Q_out)
	deltaT := p.timeStep
	thermalMass := thermalProps.ThermalMass

	// Avoid division by zero
	if thermalMass <= 0 {
		thermalMass = 3500.0 // Default 3U CubeSat value
	}

	// Calculate temperature change
	dT := (deltaT / thermalMass) * (envHeat.TotalHeat + heatGeneration - heatOutput)

	// Update temperature
	newTemp := state.Temperature + dT

	// Apply temperature limits
	if newTemp > thermalProps.MaxTemperature {
		newTemp = thermalProps.MaxTemperature
	}
	if newTemp < thermalProps.MinTemperature {
		newTemp = thermalProps.MinTemperature
	}

	// Update state
	state.Temperature = newTemp
	state.EnvironmentalHeat = envHeat
	state.Timestamp = simTime
}

// calculateInternalHeatGeneration calculates internal heat from power consumption
func (p *ThermalSimPlugin) calculateInternalHeatGeneration(node types.Node) float64 {
	// Internal heat is essentially the power consumption
	// In space, all electrical power eventually becomes heat
	if p.batteryPlugin == nil {
		// Fallback: estimate from computing resources
		computing := node.GetComputing()
		if computing == nil {
			return 0
		}
		// Use type assertion to access the concrete Computing struct
		if comp, ok := computing.(*computing.Computing); ok {
			return comp.CpuUsage * 10.0 // Simplified estimate
		}
		return 0
	}

	// Get actual power consumption from battery plugin
	state, err := p.batteryPlugin.GetBatteryState(node)
	if err != nil {
		return 0
	}

	// Power consumption equals heat generation (100% efficiency assumption)
	return state.PowerConsumption
}

// calculateHeatOutput calculates heat radiation using Stefan-Boltzmann law
func (p *ThermalSimPlugin) calculateHeatOutput(temperature float64, thermalProps types.ThermalProperties) float64 {
	// Q_out = ε * σ * A * T^4
	emissivity := thermalProps.Emissivity
	surfaceArea := thermalProps.SurfaceArea
	sigma := types.StefanBoltzmannConstant

	// Avoid negative or zero temperature
	if temperature <= 0 {
		temperature = 1.0 // Avoid math error
	}

	return emissivity * sigma * surfaceArea * math.Pow(temperature, 4)
}

// getThermalProperties returns thermal properties for a node
func (p *ThermalSimPlugin) getThermalProperties(node types.Node) types.ThermalProperties {
	if props, ok := p.thermalProps[node.GetName()]; ok {
		return props
	}
	if props, ok := p.thermalProps["default"]; ok {
		return props
	}
	return types.DefaultThermalProperties()
}

// GetThermalState returns the thermal state for a node
func (p *ThermalSimPlugin) GetThermalState(node types.Node) (*types.SatellitePhysicalState, error) {
	state, ok := p.thermalStates[node.GetName()]
	if !ok {
		return nil, fmt.Errorf("thermal state not found for node %s", node.GetName())
	}
	return state, nil
}

// GetTemperature returns the current temperature for a node in Kelvin
func (p *ThermalSimPlugin) GetTemperature(node types.Node) (float64, error) {
	state, err := p.GetThermalState(node)
	if err != nil {
		return 0, err
	}
	return state.Temperature, nil
}

// GetTemperatureCelsius returns the current temperature for a node in Celsius
func (p *ThermalSimPlugin) GetTemperatureCelsius(node types.Node) (float64, error) {
	tempK, err := p.GetTemperature(node)
	if err != nil {
		return 0, err
	}
	return tempK - 273.15, nil
}

// IsOverheating returns true if the satellite is overheating
func (p *ThermalSimPlugin) IsOverheating(node types.Node) bool {
	state, err := p.GetThermalState(node)
	if err != nil {
		return false
	}
	thermalProps := p.getThermalProperties(node)
	return state.Temperature > thermalProps.MaxTemperature*0.9 // Warning at 90% of max
}

// IsHypothermia returns true if the satellite is too cold
func (p *ThermalSimPlugin) IsHypothermia(node types.Node) bool {
	state, err := p.GetThermalState(node)
	if err != nil {
		return false
	}
	thermalProps := p.getThermalProperties(node)
	return state.Temperature < thermalProps.MinTemperature*1.2 // Warning at 120% of min
}

// GetAllStates returns all thermal states
func (p *ThermalSimPlugin) GetAllStates() map[string]*types.SatellitePhysicalState {
	return p.thermalStates
}

// Reset resets all thermal states
func (p *ThermalSimPlugin) Reset() {
	p.thermalStates = make(map[string]*types.SatellitePhysicalState)
}

// =============================================================================
// Cyber-Physical Feedback Functions
// =============================================================================

// GetEffectiveCapacity returns the temperature-adjusted battery capacity
// This implements the cyber-physical feedback loop
func (p *ThermalSimPlugin) GetEffectiveCapacity(node types.Node) float64 {
	if !p.enableFeedback {
		return 1.0
	}

	state, err := p.GetThermalState(node)
	if err != nil {
		return 1.0
	}

	// Temperature effect on battery capacity
	// Cold temperatures reduce effective capacity
	temp := state.Temperature
	optimalTemp := 293.15 // 20°C

	// Simple model: capacity drops below 10°C and above 40°C
	efficiency := 1.0
	if temp < 283.15 { // Below 10°C
		efficiency = 0.5 + 0.5*(temp-263.15)/20.0
	} else if temp > 313.15 { // Above 40°C
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

// GetInternalResistance returns temperature-adjusted internal resistance
// Cold temperatures increase internal resistance
func (p *ThermalSimPlugin) GetInternalResistance(node types.Node) float64 {
	if !p.enableFeedback {
		return 0.1 // Default 100 mOhm
	}

	state, err := p.GetThermalState(node)
	if err != nil {
		return 0.1
	}

	baseResistance := 0.1 // 100 mOhm
	temp := state.Temperature

	// Resistance increases at low temperatures
	if temp < 273.15 { // Below 0°C
		return baseResistance * 2.0
	} else if temp < 293.15 { // 0-20°C
		return baseResistance * 1.5
	}

	return baseResistance
}