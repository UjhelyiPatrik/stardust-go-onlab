package deployment

import (
	"fmt"
	"log"
	"math/rand"
	"strings"

	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// OrchestrationStrategy defines the contract for scoring a satellite.
// Returns a score [0.0, 1.0] representing how well it fits the strategy.
// Returns -1.0 if it strictly violates the strategy.
type OrchestrationStrategy interface {
	Evaluate(sat types.Satellite, sunPlugin stateplugin.SunStatePlugin, thermalPlugin *simplugin.ThermalSimPlugin, batteryPlugin *simplugin.BatterySimPlugin) float64
}

// ColdestStrategy: Higher score for lower temperatures.
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

// SunnyStrategy: Strict sun requirement, higher score for better exposure.
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

// DarkStrategy: Strict dark requirement.
type DarkStrategy struct{}

func (s *DarkStrategy) Evaluate(sat types.Satellite, sunPlugin stateplugin.SunStatePlugin, thermalPlugin *simplugin.ThermalSimPlugin, batteryPlugin *simplugin.BatterySimPlugin) float64 {
	if sunPlugin == nil {
		return 1.0
	}
	exposure := sunPlugin.GetSunlightExposure(sat)
	if exposure > 0.1 {
		return -1.0 // Szigorú elutasítás, ha éri a nap
	}
	return 1.0 - exposure
}

type AnywhereStrategy struct{}

func (s *AnywhereStrategy) Evaluate(sat types.Satellite, sunPlugin stateplugin.SunStatePlugin, thermalPlugin *simplugin.ThermalSimPlugin, batteryPlugin *simplugin.BatterySimPlugin) float64 {
	return 1.0
}

func isValidTarget(sat types.Satellite, task types.DeployableService, thermalPlugin *simplugin.ThermalSimPlugin, batteryPlugin *simplugin.BatterySimPlugin) bool {
	if !sat.GetComputing().CanPlace(task) {
		return false
	}
	if thermalPlugin != nil && thermalPlugin.IsOverheating(sat) {
		return false
	}
	if batteryPlugin != nil && batteryPlugin.IsCritical(sat) {
		return false
	}
	return true
}

type TaskOrchestratorPlugin struct {
	strategyName  string
	Strategy      OrchestrationStrategy
	TaskPool      []types.DeployableService
	thermalPlugin *simplugin.ThermalSimPlugin
	batteryPlugin *simplugin.BatterySimPlugin
}

func NewTaskOrchestratorPlugin(strategyName string, plugins []types.SimulationPlugin) *TaskOrchestratorPlugin {
	orch := &TaskOrchestratorPlugin{
		strategyName: strategyName,
		TaskPool:     make([]types.DeployableService, 0),
	}

	switch strategyName {
	case "coldest":
		orch.Strategy = &ColdestStrategy{}
	case "sunny":
		orch.Strategy = &SunnyStrategy{}
	case "dark":
		orch.Strategy = &DarkStrategy{}
	case "anywhere":
		orch.Strategy = &AnywhereStrategy{}
	default:
		log.Printf("[WARN] Unknown orchestrator strategy '%s', falling back to 'anywhere'", strategyName)
		orch.Strategy = &AnywhereStrategy{}
	}

	for _, p := range plugins {
		if tp, ok := p.(*simplugin.ThermalSimPlugin); ok {
			orch.thermalPlugin = tp
		}
		if bp, ok := p.(*simplugin.BatterySimPlugin); ok {
			orch.batteryPlugin = bp
		}
	}

	return orch
}

func (p *TaskOrchestratorPlugin) Name() string {
	return "TaskOrchestratorPlugin"
}

func (p *TaskOrchestratorPlugin) PostSimulationStep(sim types.SimulationController) error {
	sats := sim.GetSatellites()
	var sunPlugin stateplugin.SunStatePlugin

	repo := sim.GetStatePluginRepository()
	if repo != nil {
		for _, sp := range repo.GetAllPlugins() {
			if envPlugin, ok := sp.(stateplugin.SunStatePlugin); ok {
				sunPlugin = envPlugin
				break
			}
		}
	}

	for _, sat := range sats {
		comp := sat.GetComputing()
		if comp == nil {
			continue
		}
		// After the main scheduling logic, we can simulate task completion with a small random chance (10%)
		for _, srv := range comp.GetServices() {
			if rand.Float64() < 0.10 {
				comp.RemoveDeploymentAsync(srv)
				log.Printf("[Workload] COMPLETED: %s on %s", srv.GetServiceName(), sat.GetName())
			}
		}
	}

	// For demonstration, we can also randomly generate new tasks at each step
	newTasksCount := rand.Intn(20) + 5
	for i := 0; i < newTasksCount; i++ {
		// Generating a unique task name using the current simulation time and an index
		taskName := fmt.Sprintf("Task-%d-%d", sim.GetSimulationTime().Unix(), i)
		// Randomly assign CPU load between 0.5 and 2.5 cores, and a fixed memory requirement of 128MB
		cpuLoad := 0.5 + (rand.Float64() * 2.0)
		task, _ := NewDeployableService(taskName, cpuLoad, 128.0)
		p.TaskPool = append(p.TaskPool, task)
	}

	// Phase 1: Eviction (if necessary)
	for _, sat := range sats {
		comp := sat.GetComputing()
		if comp == nil {
			continue
		}

		// ÚJ: Indokok dinamikus gyűjtése
		var evictionReasons []string

		if p.thermalPlugin != nil && p.thermalPlugin.IsOverheating(sat) {
			// Bónusz: Lekérjük a pontos hőmérsékletet, hogy lássuk, mennyire sült meg
			temp, _ := p.thermalPlugin.GetTemperatureCelsius(sat)
			evictionReasons = append(evictionReasons, fmt.Sprintf("Thermal Overheating (%.1f°C)", temp))
		}

		if p.batteryPlugin != nil && p.batteryPlugin.IsCritical(sat) {
			// Bónusz: Lekérjük a pontos töltöttséget
			soc, _ := p.batteryPlugin.GetSOC(sat)
			evictionReasons = append(evictionReasons, fmt.Sprintf("Battery Critical (%.1f%%)", soc*100))
		}

		// Ha van legalább egy indok, végrehajtjuk az eviction-t
		if len(evictionReasons) > 0 {
			reasonStr := strings.Join(evictionReasons, " & ")

			for _, srv := range comp.GetServices() {
				comp.RemoveDeploymentAsync(srv)
				p.TaskPool = append(p.TaskPool, srv)

				// ÚJ: Az indok kiírása a logba
				log.Printf("[Scheduler] EVICTED: %s from %s (Reason: %s)", srv.GetServiceName(), sat.GetName(), reasonStr)
			}
		}
	}

	// Log the current number of tasks in the pool
	log.Printf("[Scheduler] Current Task Pool Size: %d", len(p.TaskPool))

	// Phase 2: Scheduling with Load Balancing
	var unplaced []types.DeployableService

	for _, task := range p.TaskPool {
		var bestSat types.Satellite
		bestCombinedScore := -1.0

		for _, sat := range sats {
			if !isValidTarget(sat, task, p.thermalPlugin, p.batteryPlugin) {
				continue
			}

			// 1. Stratégiai pontszám beolvasása
			strategyScore := p.Strategy.Evaluate(sat, sunPlugin, p.thermalPlugin, p.batteryPlugin)
			if strategyScore < 0 {
				continue // A műhold sérti a stratégia szigorú szabályait
			}

			// 2. Erőforrás pontszám (Load Balancing): Minél több a szabad CPU, annál jobb
			comp := sat.GetComputing()
			// Feltételezzük, hogy egy Edge node-nak ~512 magja van.
			resourceScore := comp.CpuAvailable() / 512.0
			if resourceScore > 1.0 {
				resourceScore = 1.0
			}

			// 3. Kombinált pontszám (60% stratégia, 40% elosztás)
			combinedScore := (strategyScore * 0.6) + (resourceScore * 0.4)

			if combinedScore > bestCombinedScore {
				bestCombinedScore = combinedScore
				bestSat = sat
			}
		}

		if bestSat != nil {
			// A korábbi elnyelt hiba helyett lekezeljük az err-t
			placed, err := bestSat.GetComputing().TryPlaceDeploymentAsync(task)
			if err != nil {
				log.Printf("[Scheduler] ERROR: Failed to place task on %s: %v", bestSat.GetName(), err)
			}

			if placed {
				log.Printf("[Scheduler] PLACED: %s on %s (Score: %.2f)", task.GetServiceName(), bestSat.GetName(), bestCombinedScore)
			} else {
				unplaced = append(unplaced, task)
			}
		} else {
			unplaced = append(unplaced, task)
		}
	}

	p.TaskPool = unplaced
	return nil
}
