package deployment

import (
	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

type SunnyStrategy struct{}

func (s *SunnyStrategy) Evaluate(sat types.Satellite, sunPlugin stateplugin.SunStatePlugin, thermalPlugin *simplugin.ThermalSimPlugin, batteryPlugin *simplugin.BatterySimPlugin) float64 {
	if sunPlugin == nil {
		return 1.0
	}
	exposure := sunPlugin.GetSunlightExposure(sat)
	if exposure < 0.1 {
		return -1.0 // Szigorú elutasítás, ha árnyékban van
	}
	return exposure // 0.1 - 1.0
}
