package deployment

import (
	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

type SunnyStrategy struct{}

func (s *SunnyStrategy) Evaluate(source types.Node, target types.Satellite, task types.DeployableService, sunPlugin stateplugin.SunStatePlugin, thermalPlugin *simplugin.ThermalSimPlugin, batteryPlugin *simplugin.BatterySimPlugin) float64 {
	if sunPlugin == nil {
		return -1.0
	}
	if thermalPlugin == nil {
		return -1.0
	}

	// Check temperature first to avoid unnecessary sunlight checks on already overheated satellites.
	temp, err := thermalPlugin.GetTemperature(target)
	if err != nil {
		return -1.0
	}
	maxTemp := 50.0
	maxTemp = thermalPlugin.GetThermalProperties(target).MaxTemperature

	// If the satellite is already too hot, exclude it.
	if temp > maxTemp-5 {
		return -1.0
	}

	exposure := sunPlugin.GetSunlightExposure(target)
	if exposure < 0.1 {
		return -1.0 // Exclude if not in direct sunlight
	}
	return 1.0 // Perfect score for being in direct sunlight and having a valid temperature
}
