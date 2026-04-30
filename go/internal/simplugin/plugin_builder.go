package simplugin

import (
	"fmt"

	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
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
			configPath := "./resources/configs/physicalConfig.yaml"
			
			physicalConfig, err := configs.LoadPhysicalConfig(configPath)
			if err!= nil {
				return nil, fmt.Errorf("failed to load physical config at %s: %w", configPath, err)
			}
			coordinator := NewPhysicalPluginCoordinator(physicalConfig)
			plugins = append(plugins, coordinator.GetSimulationPlugins()...)
		default:
			return nil, fmt.Errorf("unknown plugin: %s", name)
		}
	}
	return plugins, nil
}
