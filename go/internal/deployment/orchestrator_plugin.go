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
		Strategy:     GetStrategy(strategyName), // Factory hívás
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

	// --- A. Feladatok befejezésének szimulálása ---
	for _, sat := range sats {
		comp := sat.GetComputing()
		if comp == nil {
			continue
		}
		for _, srv := range comp.GetServices() {
			if rand.Float64() < 0.10 {
				comp.RemoveDeploymentAsync(srv)
				log.Printf("[Workload] COMPLETED: %s on %s", srv.GetServiceName(), sat.GetName())
			}
		}
	}

	// --- B. Új feladatok beérkezésének szimulálása ---
	newTasksCount := rand.Intn(20) + 5
	for i := 0; i < newTasksCount; i++ {
		taskName := fmt.Sprintf("Task-%d-%d", sim.GetSimulationTime().Unix(), i)
		cpuLoad := 0.5 + (rand.Float64() * 2.0)
		task, _ := NewDeployableService(taskName, cpuLoad, 128.0, 20971520)
		p.TaskPool = append(p.TaskPool, task)
	}

	// --- C. Eviction (Kilakoltatás, indoklással) ---
	for _, sat := range sats {
		comp := sat.GetComputing()
		if comp == nil {
			continue
		}

		var evictionReasons []string

		if p.thermalPlugin != nil && p.thermalPlugin.IsOverheating(sat) {
			temp, _ := p.thermalPlugin.GetTemperatureCelsius(sat)
			evictionReasons = append(evictionReasons, fmt.Sprintf("Thermal Overheating (%.1f°C)", temp))
		}

		if p.batteryPlugin != nil && p.batteryPlugin.IsCritical(sat) {
			soc, _ := p.batteryPlugin.GetSOC(sat)
			evictionReasons = append(evictionReasons, fmt.Sprintf("Battery Critical (%.1f%%)", soc*100))
		}

		if len(evictionReasons) > 0 {
			reasonStr := strings.Join(evictionReasons, " & ")
			for _, srv := range comp.GetServices() {
				comp.RemoveDeploymentAsync(srv)
				p.TaskPool = append(p.TaskPool, srv)
				log.Printf("[Scheduler] EVICTED: %s from %s (Reason: %s)", srv.GetServiceName(), sat.GetName(), reasonStr)
			}
		}
	}

	log.Printf("[Scheduler] Current Task Pool Size: %d", len(p.TaskPool))

	// --- D. Feladatok Kiosztása (Load Balancing + Stratégia) ---
	var unplaced []types.DeployableService

	for _, task := range p.TaskPool {
		var bestSat types.Satellite
		bestCombinedScore := -1.0

		for _, sat := range sats {
			if !isValidTarget(sat, task, p.thermalPlugin, p.batteryPlugin) {
				continue
			}

			// A stratégiai logika delegálása az adott fájl/algoritmus felé
			strategyScore := p.Strategy.Evaluate(nil, sat, task, sunPlugin, p.thermalPlugin, p.batteryPlugin)
			if strategyScore < 0 {
				continue
			}

			comp := sat.GetComputing()
			resourceScore := comp.CpuAvailable() / 512.0
			if resourceScore > 1.0 {
				resourceScore = 1.0
			}

			combinedScore := (strategyScore * 0.6) + (resourceScore * 0.4)

			if combinedScore > bestCombinedScore {
				bestCombinedScore = combinedScore
				bestSat = sat
			}
		}

		if bestSat != nil {
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
