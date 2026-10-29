package simplugin

import (
	"fmt"
	"sync"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/internal/computing"
	"github.com/polaris-slo-cloud/stardust-go/internal/node"
	"github.com/polaris-slo-cloud/stardust-go/pkg/helper"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.SimulationPlugin = (*BatterySimPlugin)(nil)

// BatterySimPlugin simulates battery dynamics for satellites using multi-threaded execution.
type BatterySimPlugin struct {
	mu sync.RWMutex

	// Battery states: node name -> physical state
	batteryStates map[string]*types.SatellitePhysicalState

	// Flat slice of state pointers matching satellite slice order for lock-free ParallelFor updates
	orderedStates []*types.SatellitePhysicalState

	// Battery properties per node type
	batteryProps map[string]types.BatteryProperties

	// Power properties per node type
	powerProps map[string]types.PowerProperties

	// Reference to thermal environment plugin
	thermalPlugin ThermalEnvironmentPlugin

	// Time step in seconds (used as a fallback or sub-stepping constraint)
	timeStep float64
}

// ThermalEnvironmentPlugin interface (must be implemented by thermal plugin)
type ThermalEnvironmentPlugin interface {
	GetEnvironmentalHeat(node types.Node) types.EnvironmentalHeat
}

// NewBatterySimPlugin creates a new battery simulation plugin
func NewBatterySimPlugin() *BatterySimPlugin {
	return &BatterySimPlugin{
		batteryStates: make(map[string]*types.SatellitePhysicalState),
		batteryProps:  make(map[string]types.BatteryProperties),
		powerProps:    make(map[string]types.PowerProperties),
		timeStep:      1.0, // Default 1 second time step
	}
}

// Name returns the plugin name
func (p *BatterySimPlugin) Name() string {
	return "BatterySimPlugin"
}

// SetThermalPlugin sets the thermal environment plugin reference
func (p *BatterySimPlugin) SetThermalPlugin(plugin ThermalEnvironmentPlugin) {
	p.thermalPlugin = plugin
}

// SetBatteryProperties sets the battery properties for a node type
func (p *BatterySimPlugin) SetBatteryProperties(nodeType string, props types.BatteryProperties) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.batteryProps[nodeType] = props
}

// SetPowerProperties sets the power properties for a node type
func (p *BatterySimPlugin) SetPowerProperties(nodeType string, props types.PowerProperties) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.powerProps[nodeType] = props
}

// SetTimeStep sets the simulation time step
func (p *BatterySimPlugin) SetTimeStep(dt float64) {
	p.timeStep = dt
}

// PostSimulationStep updates battery states for all satellites in parallel across all CPU cores.
func (p *BatterySimPlugin) PostSimulationStep(simulation types.SimulationController) error {
	nodes := simulation.GetSatellites()
	if len(nodes) == 0 {
		return nil
	}
	simTime := simulation.GetSimulationTime()

	// Resolve Thermal Environment Plugin once before launching parallel workers
	if p.thermalPlugin == nil {
		repo := simulation.GetStatePluginRepository()
		if repo != nil {
			for _, sp := range repo.GetAllPlugins() {
				if envPlugin, ok := sp.(ThermalEnvironmentPlugin); ok {
					p.thermalPlugin = envPlugin
					break
				}
			}
		}
	}

	// Ensure all satellite state structs are pre-allocated in the map and flat slice (only runs on first step)
	if len(p.orderedStates) != len(nodes) {
		p.mu.Lock()
		p.orderedStates = make([]*types.SatellitePhysicalState, len(nodes))
		for i, n := range nodes {
			name := n.GetName()
			st, exists := p.batteryStates[name]
			if !exists {
				st = types.NewSatellitePhysicalState(name)
				st.Timestamp = simTime
				st.SOC = 1.0 // Start fully charged
				powerProps := p.getPowerProperties(n)
				st.PowerConsumption = p.calculatePowerConsumption(n, powerProps, 0)
				st.PowerGeneration = p.calculatePowerGeneration(n, powerProps)
				p.batteryStates[name] = st
			}
			p.orderedStates[i] = st
		}
		p.mu.Unlock()
		return nil
	}

	// Multi-threaded lock-free update across all satellites
	indices := make([]int, len(nodes))
	for i := range nodes {
		indices[i] = i
	}

	helper.ParallelFor(indices, func(i int) {
		p.updateBatteryStateForIndex(nodes[i], p.orderedStates[i], simTime)
	})

	// Reset traffic on all active links concurrently
	helper.ParallelFor(nodes, func(sat types.Satellite) {
		for _, link := range sat.GetLinkNodeProtocol().Established() {
			link.ResetTraffic()
		}
	})

	return nil
}

// updateBatteryStateForIndex updates the battery state for a single satellite without map locks.
func (p *BatterySimPlugin) updateBatteryStateForIndex(n types.Node, state *types.SatellitePhysicalState, simTime time.Time) {
	deltaT := simTime.Sub(state.Timestamp).Seconds()
	if deltaT <= 0 {
		return
	}

	batteryProps := p.getBatteryProperties(n)
	powerProps := p.getPowerProperties(n)

	// Calculate dynamic power consumption from CPU, Memory, and ISL network traffic
	powerConsumption := p.calculatePowerConsumption(n, powerProps, deltaT)

	// Retrieve precomputed or live solar power generation
	powerGeneration := p.calculatePowerGeneration(n, powerProps)

	// Calculate net current
	netPower := powerGeneration - powerConsumption
	netCurrent := netPower / batteryProps.NominalVoltage

	state.PowerConsumption = powerConsumption
	state.PowerGeneration = powerGeneration
	state.NetCurrent = netCurrent
	state.Timestamp = simTime

	// Update SOC using Coulomb Counting with dynamic deltaT
	capacityAs := batteryProps.Capacity * 3600.0
	efficiency := batteryProps.CoulombEfficiency

	deltaSOC := (efficiency * netCurrent * deltaT) / capacityAs
	newSOC := state.SOC + deltaSOC

	if newSOC > 1.0 {
		newSOC = 1.0
	} else if newSOC < 0 {
		newSOC = 0
	}

	state.SOC = newSOC
}

// calculatePowerConsumption calculates the power consumption of a satellite
func (p *BatterySimPlugin) calculatePowerConsumption(n types.Node, powerProps types.PowerProperties, deltaT float64) float64 {
	compNode := n.GetComputing()
	if compNode == nil {
		return powerProps.IdlePowerConsumption
	}

	cpuUsage := 0.0
	memoryUsage := 0.0

	if comp, ok := compNode.(*computing.Computing); ok {
		cpuUsage = comp.CpuUsage
		memoryUsage = comp.MemoryUsage
	}

	basePower := powerProps.IdlePowerConsumption
	cpuPower := cpuUsage * 10.0      // 10W per unit of CPU usage
	memoryPower := memoryUsage * 0.1 // 0.1W per unit of memory

	networkPower := 0.0
	if deltaT > 0 && powerProps.IslEnergyPerByte > 0 {
		for _, link := range n.GetLinkNodeProtocol().Established() {
			traffic := float64(link.GetTraffic())
			if traffic > 0 {
				networkPower += (traffic * powerProps.IslEnergyPerByte) / deltaT
			}
		}
	}

	return basePower + cpuPower + memoryPower + networkPower
}

// calculatePowerGeneration retrieves the precomputed solar power generation in O(1), or calculates it if running live.
func (p *BatterySimPlugin) calculatePowerGeneration(n types.Node, powerProps types.PowerProperties) float64 {
	// 1. O(1) Fast Path: Read directly from PrecomputedSatellite state
	if pSat, ok := n.(*node.PrecomputedSatellite); ok {
		if st, hasState := pSat.GetCurrentPrecomputedState(); hasState {
			if st.InEclipse {
				return 0
			}
			if st.SolarPowerGeneration > 0 {
				return st.SolarPowerGeneration
			}
			return powerProps.MaxPowerGeneration * st.SunlightExposure * powerProps.SolarEfficiency
		}
	}

	// 2. Live Mode Path: Read from ThermalEnvironmentStatePlugin
	if p.thermalPlugin == nil {
		return 0
	}

	envHeat := p.thermalPlugin.GetEnvironmentalHeat(n)
	if envHeat.InEclipse {
		return 0
	}
	if envHeat.SolarPowerGeneration > 0 {
		return envHeat.SolarPowerGeneration
	}

	exposure := envHeat.SunlightExposure
	if exposure > 1.0 {
		exposure = 1.0
	}
	if exposure < 0 {
		exposure = 0
	}

	return powerProps.MaxPowerGeneration * exposure * powerProps.SolarEfficiency
}

func (p *BatterySimPlugin) getBatteryProperties(n types.Node) types.BatteryProperties {
	if props, ok := p.batteryProps[n.GetName()]; ok {
		return props
	}
	if props, ok := p.batteryProps["default"]; ok {
		return props
	}
	return types.DefaultBatteryProperties()
}

func (p *BatterySimPlugin) getPowerProperties(n types.Node) types.PowerProperties {
	if props, ok := p.powerProps[n.GetName()]; ok {
		return props
	}
	if props, ok := p.powerProps["default"]; ok {
		return props
	}
	return types.DefaultPowerProperties()
}

func (p *BatterySimPlugin) GetBatteryState(n types.Node) (*types.SatellitePhysicalState, error) {
	if pSat, ok := n.(*node.PrecomputedSatellite); ok {
		if pSat.SatIndex >= 0 && pSat.SatIndex < len(p.orderedStates) {
			return p.orderedStates[pSat.SatIndex], nil
		}
	}
	p.mu.RLock()
	state, ok := p.batteryStates[n.GetName()]
	p.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("battery state not found for node %s", n.GetName())
	}
	return state, nil
}

func (p *BatterySimPlugin) GetSOC(n types.Node) (float64, error) {
	state, err := p.GetBatteryState(n)
	if err != nil {
		return 0, err
	}
	return state.SOC, nil
}

func (p *BatterySimPlugin) IsCritical(n types.Node) bool {
	state, err := p.GetBatteryState(n)
	if err != nil {
		return false
	}
	batteryProps := p.getBatteryProperties(n)
	return state.SOC < batteryProps.CriticalSOC
}

func (p *BatterySimPlugin) GetAllStates() map[string]*types.SatellitePhysicalState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.batteryStates
}

func (p *BatterySimPlugin) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.batteryStates = make(map[string]*types.SatellitePhysicalState)
	p.orderedStates = nil
}

// --- Integration with TelemetryExporterPlugin ---
func (p *BatterySimPlugin) GetNetEnergyChange(n types.Node) (float64, error) {
	state, err := p.GetBatteryState(n)
	if err != nil {
		return 0, err
	}
	return state.PowerGeneration - state.PowerConsumption, nil
}
