package simplugin

import (
	"fmt"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/internal/computing"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.SimulationPlugin = (*BatterySimPlugin)(nil)

// BatterySimPlugin simulates battery dynamics for satellites
type BatterySimPlugin struct {
	// Battery states: node name -> physical state
	batteryStates map[string]*types.SatellitePhysicalState

	// Battery properties per node type
	batteryProps map[string]types.BatteryProperties

	// Power properties per node type
	powerProps map[string]types.PowerProperties

	// Reference to thermal environment plugin
	thermalPlugin ThermalEnvironmentPlugin

	// Time step in seconds
	timeStep float64
}

// ThermalEnvironmentPlugin interface (must be implemented by thermal plugin)
type ThermalEnvironmentPlugin interface {
	GetEnvironmentalHeat(node types.Node) types.EnvironmentalHeat
}

// NewBatterySimPlugin creates a new battery simulation plugin
func NewBatterySimPlugin() *BatterySimPlugin {
	return &BatterySimPlugin{
		batteryStates: make(map[string]*types.SatellitePhysicalState),
		batteryProps:  make(map[string]types.BatteryProperties),
		powerProps:    make(map[string]types.PowerProperties),
		timeStep:      1.0, // Default 1 second time step
	}
}

// Name returns the plugin name
func (p *BatterySimPlugin) Name() string {
	return "BatterySimPlugin"
}

// SetThermalPlugin sets the thermal environment plugin reference
func (p *BatterySimPlugin) SetThermalPlugin(plugin ThermalEnvironmentPlugin) {
	p.thermalPlugin = plugin
}

// SetBatteryProperties sets the battery properties for a node type
func (p *BatterySimPlugin) SetBatteryProperties(nodeType string, props types.BatteryProperties) {
	p.batteryProps[nodeType] = props
}

// SetPowerProperties sets the power properties for a node type
func (p *BatterySimPlugin) SetPowerProperties(nodeType string, props types.PowerProperties) {
	p.powerProps[nodeType] = props
}

// SetTimeStep sets the simulation time step
func (p *BatterySimPlugin) SetTimeStep(dt float64) {
	p.timeStep = dt
}

// PostSimulationStep updates battery states for all satellites
func (p *BatterySimPlugin) PostSimulationStep(simulation types.SimulationController) error {
	nodes := simulation.GetSatellites()
	simTime := simulation.GetSimulationTime()

	for _, node := range nodes {
		p.updateBatteryState(node, simTime)
	}

	return nil
}

// updateBatteryState updates the battery state for a single satellite
func (p *BatterySimPlugin) updateBatteryState(node types.Node, simTime time.Time) {
	nodeName := node.GetName()

	// Get or create battery state
	state, ok := p.batteryStates[nodeName]
	if !ok {
		state = types.NewSatellitePhysicalState(nodeName)
		p.batteryStates[nodeName] = state
	}

	// Get properties
	batteryProps := p.getBatteryProperties(node)
	powerProps := p.getPowerProperties(node)

	// Calculate power consumption from computing
	powerConsumption := p.calculatePowerConsumption(node, batteryProps)

	// Calculate power generation from solar panels
	powerGeneration := p.calculatePowerGeneration(node, powerProps)

	// Calculate net current
	netPower := powerGeneration - powerConsumption
	netCurrent := netPower / batteryProps.NominalVoltage

	// Update state
	state.PowerConsumption = powerConsumption
	state.PowerGeneration = powerGeneration
	state.NetCurrent = netCurrent
	state.Timestamp = simTime

	// Update SOC using Coulomb Counting
	// SOC(t+1) = SOC(t) + (η * I * Δt) / (3600 * C_total)
	capacityAh := batteryProps.Capacity
	capacityAs := capacityAh * 3600 // Convert to Ampere-seconds
	efficiency := batteryProps.CoulombEfficiency

	// Apply Coulomb efficiency
	deltaSOC := (efficiency * netCurrent * p.timeStep) / capacityAs
	newSOC := state.SOC + deltaSOC

	// Clamp SOC to valid range
	if newSOC > 1.0 {
		newSOC = 1.0
	} else if newSOC < 0 {
		newSOC = 0
	}

	state.SOC = newSOC
}

// calculatePowerConsumption calculates the power consumption of a satellite
func (p *BatterySimPlugin) calculatePowerConsumption(node types.Node, batteryProps types.BatteryProperties) float64 {
	// Get power properties for idle consumption
	powerProps := p.getPowerProperties(node)

	// Get computing resource usage
	compNode := node.GetComputing()
	if compNode == nil {
		return powerProps.IdlePowerConsumption
	}

	// Estimate usage as difference from total (simplified model)
	// In a full implementation, we'd track actual usage
	cpuUsage := 0.0
	memoryUsage := 0.0
	if comp, ok := compNode.(*computing.Computing); ok {
		cpuUsage = comp.CpuUsage
		memoryUsage = comp.MemoryUsage
	}

	// Power model: base + CPU scaling + memory scaling
	basePower := powerProps.IdlePowerConsumption
	cpuPower := cpuUsage * 10.0  // 10W per unit of CPU usage
	memoryPower := memoryUsage * 0.1 // 0.1W per unit of memory

	totalPower := basePower + cpuPower + memoryPower

	return totalPower
}

// calculatePowerGeneration calculates the solar power generation
func (p *BatterySimPlugin) calculatePowerGeneration(node types.Node, powerProps types.PowerProperties) float64 {
	// Get environmental heat from thermal plugin
	if p.thermalPlugin == nil {
		return 0
	}

	envHeat := p.thermalPlugin.GetEnvironmentalHeat(node)

	// If in eclipse, no power generation
	if envHeat.InEclipse {
		return 0
	}

	// Power generation based on sunlight exposure
	// P = P_max * exposure * efficiency
	exposure := envHeat.SunlightExposure
	if exposure > 1.0 {
		exposure = 1.0
	}
	if exposure < 0 {
		exposure = 0
	}

	return powerProps.MaxPowerGeneration * exposure * powerProps.SolarEfficiency
}

// getBatteryProperties returns battery properties for a node
func (p *BatterySimPlugin) getBatteryProperties(node types.Node) types.BatteryProperties {
	if props, ok := p.batteryProps[node.GetName()]; ok {
		return props
	}
	if props, ok := p.batteryProps["default"]; ok {
		return props
	}
	return types.DefaultBatteryProperties()
}

// getPowerProperties returns power properties for a node
func (p *BatterySimPlugin) getPowerProperties(node types.Node) types.PowerProperties {
	if props, ok := p.powerProps[node.GetName()]; ok {
		return props
	}
	if props, ok := p.powerProps["default"]; ok {
		return props
	}
	return types.DefaultPowerProperties()
}

// GetBatteryState returns the battery state for a node
func (p *BatterySimPlugin) GetBatteryState(node types.Node) (*types.SatellitePhysicalState, error) {
	state, ok := p.batteryStates[node.GetName()]
	if !ok {
		return nil, fmt.Errorf("battery state not found for node %s", node.GetName())
	}
	return state, nil
}

// GetSOC returns the current SOC for a node
func (p *BatterySimPlugin) GetSOC(node types.Node) (float64, error) {
	state, err := p.GetBatteryState(node)
	if err != nil {
		return 0, err
	}
	return state.SOC, nil
}

// IsCritical returns true if the battery is at critical level
func (p *BatterySimPlugin) IsCritical(node types.Node) bool {
	state, err := p.GetBatteryState(node)
	if err != nil {
		return false
	}
	batteryProps := p.getBatteryProperties(node)
	return state.SOC < batteryProps.CriticalSOC
}

// GetAllStates returns all battery states
func (p *BatterySimPlugin) GetAllStates() map[string]*types.SatellitePhysicalState {
	return p.batteryStates
}

// Reset resets all battery states
func (p *BatterySimPlugin) Reset() {
	p.batteryStates = make(map[string]*types.SatellitePhysicalState)
}