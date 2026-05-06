package deployment

import (
	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

type ColdestStrategy struct{}

func (s *ColdestStrategy) Evaluate(sat types.Satellite, sunPlugin stateplugin.SunStatePlugin, thermalPlugin *simplugin.ThermalSimPlugin, batteryPlugin *simplugin.BatterySimPlugin) float64 {
	if thermalPlugin == nil {
		return 1.0
	}
	temp, err := thermalPlugin.GetTemperature(sat)
	if err != nil {
		return 0.5
	}
	// Normalizálás: 250K (-23°C) és 323K (50°C) között. Alacsonyabb hő = jobb pontszám.
	score := 1.0 - ((temp - 250.0) / (323.0 - 250.0))
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}
