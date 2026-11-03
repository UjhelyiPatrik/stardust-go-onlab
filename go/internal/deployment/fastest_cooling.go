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

	// 1. Retrieve base metrics
	currentTemp, err := thermalPlugin.GetTemperatureCelsius(target)
	if err != nil {
		currentTemp = 20.0
	}

	// Try to fetch minTemp from the thermalPlugin or satellite type; fallback to hardcoded value if unavailable.
	minTemp := -10.0
	maxTemp := 50.0

	minTemp = thermalPlugin.GetThermalProperties(target).MinTemperature
	maxTemp = thermalPlugin.GetThermalProperties(target).MaxTemperature

	// 2. MANDATORY EXCLUSION: If already overheated
	if currentTemp > maxTemp-5 {
		return -1.0
	}

	// 3. COOLING SCORE (Heat Sink Score)
	// According to Stefan-Boltzmann law, a hotter satellite radiates heat faster (T^4)
	tempK := currentTemp + 273.15
	coolingPotential := math.Pow(tempK, 4) / 1e8

	// If in shadow, the cooling rate increases significantly
	shadowBonus := 0.0
	if sunPlugin != nil && sunPlugin.GetSunlightExposure(target) <= 0.01 {
		shadowBonus = 500.0
	}

	// 4. ANTI-FREEZING SCORE (Warming Bonus)
	// If the satellite approaches minimum temperature, drastically increase score
	// to prioritize task placement on it (CPU heating).
	warmingBonus := 0.0
	threshold := minTemp + 20.0 // Within 20 degrees above minimum
	if currentTemp < threshold {
		// The colder it is, the more urgent the heating (linear scaling)
		warmingBonus = (threshold - currentTemp) * 100.0
	}

	// 5. FINAL SCORE (U-curve combination)
	// Goal: find a satellite that either radiates heat very efficiently OR urgently requires heating.
	score := coolingPotential + shadowBonus + warmingBonus

	// Penalize if already running many tasks (CPU heating counteracts cooling)
	cpuPenalty := 0.0
	if comp := target.GetComputing(); comp != nil {
		cpuPenalty = comp.GetCpuUtilization() * 5.0
	}

	return score - cpuPenalty
}
