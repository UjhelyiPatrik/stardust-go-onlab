package simplugin

import (
	"fmt"

	"github.com/keniack/stardustGo/configs"
	"github.com/keniack/stardustGo/pkg/types"
)

type SimPluginBuilder struct {
}

// NewPluginBuilder creates a new instance of PluginBuilder
func NewPluginBuilder() *SimPluginBuilder {
	return &SimPluginBuilder{}
}

// BuildPlugins constructs plugin instances based on provided names
func (pb *SimPluginBuilder) BuildPlugins(pluginNames []string) ([]types.SimulationPlugin, error) {
	var plugins []types.SimulationPlugin
	for _, name := range pluginNames {
		switch name {
		case "DummyPlugin":
			plugins = append(plugins, &DummyPlugin{})
		case "BatterySimPlugin":
			plugins = append(plugins, NewBatterySimPlugin())
		case "ThermalSimPlugin":
			plugins = append(plugins, NewThermalSimPlugin())
		case "PhysicalPluginCoordinator":
			// PhysicalPluginCoordinator includes both Battery and Thermal plugins
			// Load physical config
			physicalConfig, err := configs.LoadPhysicalConfig("./resources/configs/physicalConfig.yaml")
			if err != nil {
				return nil, fmt.Errorf("failed to load physical config: %w", err)
			}
			coordinator := NewPhysicalPluginCoordinator(physicalConfig)
			// Add both simulation plugins
			plugins = append(plugins, coordinator.GetSimulationPlugins()...)
		default:
			return nil, fmt.Errorf("unknown plugin: %s", name)
		}
	}
	return plugins, nil
}
