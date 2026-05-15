package deployment

import (
	"math"

	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

type FastestCoolingStrategy struct{}

func (s *FastestCoolingStrategy) Evaluate(
	source types.Node,
	target types.Satellite,
	task types.DeployableService,
	sunPlugin stateplugin.SunStatePlugin,
	thermalPlugin *simplugin.ThermalSimPlugin,
	batteryPlugin *simplugin.BatterySimPlugin,
) float64 {
	if thermalPlugin == nil {
		return 0.1
	}

	// 1. Alapadatok lekérése
	currentTemp, err := thermalPlugin.GetTemperatureCelsius(target)
	if err != nil {
		currentTemp = 20.0
	}

	// Try to fetch minTemp from the thermalPlugin or satellite type; fallback to hardcoded value if unavailable.
	minTemp := -10.0
	maxTemp := 50.0

	minTemp = thermalPlugin.GetThermalProperties(target).MinTemperature
	maxTemp = thermalPlugin.GetThermalProperties(target).MaxTemperature

	// 2. KÖTELEZŐ KIZÁRÁS: Ha már most túlforrósodott
	if currentTemp > maxTemp-5 {
		return -1.0
	}

	// 3. HŰLÉSI PONT (Heat Sink Score)
	// A Stefan-Boltzmann törvény alapján a melegebb műhold gyorsabban hűl (T^4)
	tempK := currentTemp + 273.15
	coolingPotential := math.Pow(tempK, 4) / 1e8

	// Ha árnyékban van, az nagyságrendekkel növeli a hűlés sebességét
	shadowBonus := 0.0
	if sunPlugin != nil && sunPlugin.GetSunlightExposure(target) <= 0.01 {
		shadowBonus = 500.0
	}

	// 4. FAGYVÉDELMI PONT (Warming Bonus) - A KÉRÉSÉRE
	// Ha a műhold közeledik a minimum hőmérséklethez, drasztikusan megnöveljük a pontszámát,
	// hogy kényszerítsük a feladat ráhelyezését (CPU fűtés).
	warmingBonus := 0.0
	threshold := minTemp + 20.0 // Ha a minimum felett 20 fokon belül van
	if currentTemp < threshold {
		// Minél hidegebb, annál sürgetőbb a fűtés (lineáris skálázás)
		warmingBonus = (threshold - currentTemp) * 100.0
	}

	// 5. VÉGSŐ PONT (U-görbe kombináció)
	// A cél: olyan műholdat találni, ami VAGY nagyon jól tud hűlni, VAGY nagyon kell fűteni.
	score := coolingPotential + shadowBonus + warmingBonus

	// Büntessük, ha már eleve sok feladat fut rajta (CPU fűtés ellene dolgozik a hűlésnek)
	cpuPenalty := 0.0
	if comp := target.GetComputing(); comp != nil {
		cpuPenalty = comp.GetCpuUtilization() * 5.0
	}

	return score - cpuPenalty
}
