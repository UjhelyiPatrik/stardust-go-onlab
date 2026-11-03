package deployment

import (
	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

type ColdestStrategy struct{}

func (s *ColdestStrategy) Evaluate(source types.Node, target types.Satellite, task types.DeployableService, sunPlugin stateplugin.SunStatePlugin, thermalPlugin *simplugin.ThermalSimPlugin, batteryPlugin *simplugin.BatterySimPlugin) float64 {

	if thermalPlugin == nil {
		//log that thermal plugin is missing, cannot evaluate
		print("Thermal plugin is missing for satellite %s", target.GetName())
		return -1.0
	}

	//check the battery plugin and if the satellite is already too low on battery, exclude it.
	if batteryPlugin != nil {
		soc, err := batteryPlugin.GetSOC(target)
		if err != nil {
			return -1.0
		}
		if soc < 0.8 {
			return -1.0
		}
	}

	temp, err := thermalPlugin.GetTemperature(target)
	if err != nil {
		//log that temperature could not be retrieved, cannot evaluate
		print("Could not retrieve temperature for satellite %s: %v", target.GetName(), err)
		return -1.0
	}

	// Default values
	minTemp := -10.0
	maxTemp := 50.0

	// Only overwrite if the read value is valid (non-zero, or supported by the type specification)
	props := thermalPlugin.GetThermalProperties(target)
	if props.MaxTemperature != 0.0 || props.MinTemperature != 0.0 {
		minTemp = props.MinTemperature
		maxTemp = props.MaxTemperature
	}

	// If the satellite is already too hot, exclude it.
	if temp > maxTemp-5 {
		return -1.0
	}

	// Normalization: between minTemp and 323K (50°C). Lower temperature = better score.
	score := 1.0 - ((temp - minTemp) / (maxTemp - minTemp))
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}
