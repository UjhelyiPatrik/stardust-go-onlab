package deployment

import (
	"log"

	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// OrchestrationStrategy defines the contract for scoring a satellite.
// Returns a score [0.0, 1.0] representing how well it fits the strategy.
// Returns -1.0 if it strictly violates the strategy.
type OrchestrationStrategy interface {
	// origin parameter stands for the ground station or node from which the satellite is being evaluated for deployment.
	Evaluate(sat types.Satellite, sunPlugin stateplugin.SunStatePlugin, thermalPlugin *simplugin.ThermalSimPlugin, batteryPlugin *simplugin.BatterySimPlugin) float64
}

// GetStrategy returns an OrchestrationStrategy based on the provided name.
func GetStrategy(name string) OrchestrationStrategy {
	switch name {
	case "coldest":
		return &ColdestStrategy{}
	case "sunny":
		return &SunnyStrategy{}
	case "dark":
		return &DarkStrategy{}
	case "anywhere":
		return &AnywhereStrategy{}
	default:
		log.Printf("[WARN] Unknown orchestrator strategy '%s', falling back to 'anywhere'", name)
		return &AnywhereStrategy{}
	}
}
