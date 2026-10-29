package deployment

import (
	"log"
	"strings"
	"sync"

	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// OrchestrationStrategy defines the contract for scoring a satellite candidate.
// Returns a score in [0.0, 1.0] (or higher for specialized strategies) representing how well it fits the strategy.
// Returns a negative value (< 0.0) if the satellite strictly violates the strategy constraints.
type OrchestrationStrategy interface {
	// Evaluate calculates the fitness score for placing a task from source onto target.
	// Note: task may be nil during proactive environmental health-checks.
	Evaluate(
		source types.Node,
		target types.Satellite,
		task types.DeployableService,
		sunPlugin stateplugin.SunStatePlugin,
		thermalPlugin *simplugin.ThermalSimPlugin,
		batteryPlugin *simplugin.BatterySimPlugin,
	) float64
}

// StrategyFactory is a constructor function for creating an OrchestrationStrategy instance.
type StrategyFactory func() OrchestrationStrategy

var (
	strategyRegistryMu sync.RWMutex
	strategyRegistry   = map[string]StrategyFactory{
		"coldest":         func() OrchestrationStrategy { return &ColdestStrategy{} },
		"sunny":           func() OrchestrationStrategy { return &SunnyStrategy{} },
		"sunlight":        func() OrchestrationStrategy { return &SunnyStrategy{} },
		"dark":            func() OrchestrationStrategy { return &DarkStrategy{} },
		"anywhere":        func() OrchestrationStrategy { return &AnywhereStrategy{} },
		"fastest_cooling": func() OrchestrationStrategy { return &FastestCoolingStrategy{} },
	}
)

// RegisterStrategy registers a new orchestration strategy factory at runtime (Open/Closed Principle).
func RegisterStrategy(name string, factory StrategyFactory) {
	if name == "" || factory == nil {
		return
	}
	strategyRegistryMu.Lock()
	defer strategyRegistryMu.Unlock()
	strategyRegistry[strings.ToLower(strings.TrimSpace(name))] = factory
}

// GetStrategy resolves an OrchestrationStrategy instance by name from the registry.
func GetStrategy(name string) OrchestrationStrategy {
	key := strings.ToLower(strings.TrimSpace(name))
	strategyRegistryMu.RLock()
	factory, exists := strategyRegistry[key]
	strategyRegistryMu.RUnlock()

	if exists {
		return factory()
	}

	log.Printf("[WARN] Unknown orchestrator strategy '%s', falling back to 'anywhere'", name)
	return &AnywhereStrategy{}
}
