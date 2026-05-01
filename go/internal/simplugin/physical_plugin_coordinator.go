package simplugin

import (
	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// =============================================================================
// Physical Plugin Coordinator
// =============================================================================

// PhysicalPluginCoordinator coordinates all physical simulation plugins
type PhysicalPluginCoordinator struct {
	// Simulation plugins
	batteryPlugin *BatterySimPlugin
	thermalPlugin *ThermalSimPlugin

	// Configuration
	config *configs.PhysicalConfig
}

// NewPhysicalPluginCoordinator creates a new physical plugin coordinator
func NewPhysicalPluginCoordinator(config *configs.PhysicalConfig) *PhysicalPluginCoordinator {
	coordinator := &PhysicalPluginCoordinator{
		config: config,
	}

	// Initialize simulation plugins
	coordinator.initPlugins()

	return coordinator
}

// initPlugins initializes all physical plugins
func (c *PhysicalPluginCoordinator) initPlugins() {
	// Create simulation plugins
	c.batteryPlugin = NewBatterySimPlugin()
	c.thermalPlugin = NewThermalSimPlugin()

	// Connect Battery directly to Thermal for cyber-physical feedback.
	// (Note: ThermalEnvironmentStatePlugin is now dynamically resolved via Dependency Injection
	// in the PostSimulationStep of each plugin to ensure state sync)
	c.thermalPlugin.SetBatteryPlugin(c.batteryPlugin)

	// Configure properties for each satellite type
	c.configureProperties()
}

// configureProperties loads properties from config for all satellite types
func (c *PhysicalPluginCoordinator) configureProperties() {
	if c.config == nil {
		return
	}

	// Configure thermal environment plugin
	for satType, props := range c.config.Thermal {
		thermalProps := types.ThermalProperties{
			ThermalMass:    props.ThermalMass,
			SurfaceArea:    props.SurfaceArea,
			Absorptivity:   props.Absorptivity,
			Emissivity:     props.Emissivity,
			MaxTemperature: props.MaxTemperature,
			MinTemperature: props.MinTemperature,
		}
		c.thermalPlugin.SetThermalProperties(satType, thermalProps)
	}

	// Configure battery plugin
	for satType, props := range c.config.Battery {
		c.batteryPlugin.SetBatteryProperties(satType, types.BatteryProperties{
			Capacity:           props.Capacity,
			NominalVoltage:     props.NominalVoltage,
			CoulombEfficiency:  props.CoulombEfficiency,
			MaxDoD:             props.MaxDoD,
			CriticalSOC:        props.CriticalSOC,
			InternalResistance: props.InternalResistance,
			MaxVoltage:         props.MaxVoltage,
			MinVoltage:         props.MinVoltage,
		})
	}

	// Configure power properties
	for satType, props := range c.config.Power {
		powerProps := types.PowerProperties{
			SolarEfficiency:      props.SolarEfficiency,
			SolarPanelArea:       props.SolarPanelArea,
			MaxPowerGeneration:   props.MaxPowerGeneration,
			IdlePowerConsumption: props.IdlePowerConsumption,
		}
		c.batteryPlugin.SetPowerProperties(satType, powerProps)
	}

	// Set time step
	if c.config.Simulation.TimeStep > 0 {
		c.batteryPlugin.SetTimeStep(c.config.Simulation.TimeStep)
		c.thermalPlugin.SetTimeStep(c.config.Simulation.TimeStep)
	}

	// Set cyber-physical feedback
	c.thermalPlugin.SetEnableFeedback(c.config.Simulation.EnableCyberPhysicalFeedback)
}

// GetBatteryPlugin returns the battery simulation plugin
func (c *PhysicalPluginCoordinator) GetBatteryPlugin() *BatterySimPlugin {
	return c.batteryPlugin
}

// GetThermalPlugin returns the thermal simulation plugin
func (c *PhysicalPluginCoordinator) GetThermalPlugin() *ThermalSimPlugin {
	return c.thermalPlugin
}

// GetStatePlugins returns all state plugins for registration
// Now returns an empty slice to avoid duplicating the State Plugin explicitly loaded via CLI arguments
func (c *PhysicalPluginCoordinator) GetStatePlugins() []types.StatePlugin {
	return []types.StatePlugin{}
}

// GetSimulationPlugins returns all simulation plugins for registration
func (c *PhysicalPluginCoordinator) GetSimulationPlugins() []types.SimulationPlugin {
	return []types.SimulationPlugin{c.batteryPlugin, c.thermalPlugin}
}

// =============================================================================
// Helper Functions for External Access
// =============================================================================

// GetSOC returns the state of charge for a satellite
func (c *PhysicalPluginCoordinator) GetSOC(node types.Node) (float64, error) {
	return c.batteryPlugin.GetSOC(node)
}

// GetTemperature returns the temperature for a satellite in Kelvin
func (c *PhysicalPluginCoordinator) GetTemperature(node types.Node) (float64, error) {
	return c.thermalPlugin.GetTemperature(node)
}

// GetTemperatureCelsius returns the temperature for a satellite in Celsius
func (c *PhysicalPluginCoordinator) GetTemperatureCelsius(node types.Node) (float64, error) {
	return c.thermalPlugin.GetTemperatureCelsius(node)
}

// IsBatteryCritical returns true if the battery is at critical level
func (c *PhysicalPluginCoordinator) IsBatteryCritical(node types.Node) bool {
	return c.batteryPlugin.IsCritical(node)
}

// IsOverheating returns true if the satellite is overheating
func (c *PhysicalPluginCoordinator) IsOverheating(node types.Node) bool {
	return c.thermalPlugin.IsOverheating(node)
}

// GetEffectiveCapacity returns the temperature-adjusted battery capacity
func (c *PhysicalPluginCoordinator) GetEffectiveCapacity(node types.Node) float64 {
	return c.thermalPlugin.GetEffectiveCapacity(node)
}

// GetAllBatteryStates returns all battery states
func (c *PhysicalPluginCoordinator) GetAllBatteryStates() map[string]*types.SatellitePhysicalState {
	return c.batteryPlugin.GetAllStates()
}

// GetAllThermalStates returns all thermal states
func (c *PhysicalPluginCoordinator) GetAllThermalStates() map[string]*types.SatellitePhysicalState {
	return c.thermalPlugin.GetAllStates()
}

// Reset resets all plugin states
func (c *PhysicalPluginCoordinator) Reset() {
	c.batteryPlugin.Reset()
	c.thermalPlugin.Reset()
}
