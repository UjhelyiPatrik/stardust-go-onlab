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

	temp, err := thermalPlugin.GetTemperature(target)
	if err != nil {
		//log that temperature could not be retrieved, cannot evaluate
		print("Could not retrieve temperature for satellite %s: %v", target.GetName(), err)
		return -1.0
	}

	// Alapértelmezett értékek
	minTemp := -10.0
	maxTemp := 50.0

	// Csak akkor írjuk felül, ha a beolvasott érték értelmezhető (nem 0.0, vagy a típus specifikáció támogatja)
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
