package deployment

import (
	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

type DarkStrategy struct{}

func (s *DarkStrategy) Evaluate(source types.Node, target types.Satellite, task types.DeployableService, sunPlugin stateplugin.SunStatePlugin, thermalPlugin *simplugin.ThermalSimPlugin, batteryPlugin *simplugin.BatterySimPlugin) float64 {
	if sunPlugin == nil {
		return 1.0
	}
	exposure := sunPlugin.GetSunlightExposure(target)
	if exposure > 0.1 {
		return -1.0 // Szigorú elutasítás, ha éri a nap
	}
	return 1.0 - exposure
}
