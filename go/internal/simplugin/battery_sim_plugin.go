package simplugin

import (
	"fmt"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/internal/computing"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.SimulationPlugin = (*BatterySimPlugin)(nil)

// BatterySimPlugin simulates battery dynamics for satellites
type BatterySimPlugin struct {
	// Battery states: node name -> physical state
	batteryStates map[string]*types.SatellitePhysicalState

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
	p.batteryProps[nodeType] = props
}

// SetPowerProperties sets the power properties for a node type
func (p *BatterySimPlugin) SetPowerProperties(nodeType string, props types.PowerProperties) {
	p.powerProps[nodeType] = props
}

// SetTimeStep sets the simulation time step
func (p *BatterySimPlugin) SetTimeStep(dt float64) {
	p.timeStep = dt
}

// PostSimulationStep updates battery states for all satellites
func (p *BatterySimPlugin) PostSimulationStep(simulation types.SimulationController) error {
	nodes := simulation.GetSatellites()
	simTime := simulation.GetSimulationTime()

	for _, node := range nodes {
		p.updateBatteryState(node, simTime, simulation)
	}

	visitedLinks := make(map[types.Link]bool)
	for _, node := range nodes {
		for _, link := range node.GetLinkNodeProtocol().Established() {
			if !visitedLinks[link] {
				link.ResetTraffic()
				visitedLinks[link] = true
			}
		}
	}

	// === Calculating Power & Battery Statistics ===
	if len(p.batteryStates) > 0 {
		var minSOC, maxSOC float64
		var maxGeneration, maxConsumption float64
		var minSOCNode, maxSOCNode string
		var maxGenNode, maxConsNode string

		sumSOC := 0.0
		sumConsumption := 0.0
		first := true

		for name, state := range p.batteryStates {
			// Setting initial values for min/max on the first iteration
			if first {
				minSOC = state.SOC
				maxSOC = state.SOC
				maxGeneration = state.PowerGeneration
				maxConsumption = state.PowerConsumption

				minSOCNode = name
				maxSOCNode = name
				maxGenNode = name
				maxConsNode = name
				first = false
			} else {
				// finding minimum and maximum SOC
				if state.SOC < minSOC {
					minSOC = state.SOC
					minSOCNode = name
				}
				if state.SOC > maxSOC {
					maxSOC = state.SOC
					maxSOCNode = name
				}
				// Finding maximum charging (Generated energy)
				if state.PowerGeneration > maxGeneration {
					maxGeneration = state.PowerGeneration
					maxGenNode = name
				}
				// Finding maximum consumption
				if state.PowerConsumption > maxConsumption {
					maxConsumption = state.PowerConsumption
					maxConsNode = name
				}
			}
			sumSOC += state.SOC
			sumConsumption += state.PowerConsumption
		}

		// Calculating averages
		avgSOC := sumSOC / float64(len(p.batteryStates))
		avgConsumption := sumConsumption / float64(len(p.batteryStates))

		// Printing to console (SOC is a value between 0 and 1, so we multiply by 100 to get a percentage)
		fmt.Printf("\n=== Power & Battery Statistics (Simulation Time: %v) ===\n", simTime)
		fmt.Printf("Maximum SOC: %.2f%% (%s)\n", maxSOC*100, maxSOCNode)
		fmt.Printf("Minimum SOC: %.2f%% (%s)\n", minSOC*100, minSOCNode)
		fmt.Printf("Average SOC: %.2f%%\n", avgSOC*100)
		fmt.Printf("Max Power Generation: %.2f W (%s)\n", maxGeneration, maxGenNode)
		fmt.Printf("Max Power Consumption: %.2f W (%s)\n", maxConsumption, maxConsNode)
		fmt.Printf("Average Power Consumption: %.2f W\n", avgConsumption)
		fmt.Println("==========================================================\n ")
	}

	return nil
}

// updateBatteryState updates the battery state for a single satellite
func (p *BatterySimPlugin) updateBatteryState(node types.Node, simTime time.Time, simulation types.SimulationController) {
	nodeName := node.GetName()

	// Get or create battery state
	state, ok := p.batteryStates[nodeName]
	if !ok {
		state = types.NewSatellitePhysicalState(nodeName)
		state.Timestamp = simTime
		state.SOC = 1.0 // Start fully charged
		p.batteryStates[nodeName] = state
		return // Return early on first iteration - no delta time yet
	}

	// Calculate dynamic delta time (seconds elapsed since last update)
	deltaT := simTime.Sub(state.Timestamp).Seconds()
	if deltaT <= 0 {
		return // No time advancement
	}

	// Resolve Thermal Environment Plugin from repository at runtime (Dependency Injection)
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

	// Get properties
	batteryProps := p.getBatteryProperties(node)
	powerProps := p.getPowerProperties(node)

	// Calculate power consumption from computing
	powerConsumption := p.calculatePowerConsumption(node, batteryProps, deltaT)

	// Calculate power generation from solar panels
	powerGeneration := p.calculatePowerGeneration(node, powerProps)

	// Calculate net current
	netPower := powerGeneration - powerConsumption
	netCurrent := netPower / batteryProps.NominalVoltage

	// Update state properties
	state.PowerConsumption = powerConsumption
	state.PowerGeneration = powerGeneration
	state.NetCurrent = netCurrent
	state.Timestamp = simTime

	// Update SOC using Coulomb Counting with dynamic deltaT
	// SOC(t+1) = SOC(t) + (η * I * Δt) / (3600 * C_total)
	capacityAh := batteryProps.Capacity
	capacityAs := capacityAh * 3600 // Convert to Ampere-seconds
	efficiency := batteryProps.CoulombEfficiency

	// Apply Coulomb efficiency using the ACTUAL elapsed time (deltaT)
	deltaSOC := (efficiency * netCurrent * deltaT) / capacityAs
	newSOC := state.SOC + deltaSOC

	// Clamp SOC to valid range
	if newSOC > 1.0 {
		newSOC = 1.0
	} else if newSOC < 0 {
		newSOC = 0
	}

	state.SOC = newSOC
}

// calculatePowerConsumption calculates the power consumption of a satellite
func (p *BatterySimPlugin) calculatePowerConsumption(node types.Node, batteryProps types.BatteryProperties, deltaT float64) float64 {
	powerProps := p.getPowerProperties(node)
	compNode := node.GetComputing()

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

	// --- ÚJ KÓD: Hálózati forgalom áramfogyasztása ---
	networkPower := 0.0
	if deltaT > 0 && powerProps.IslEnergyPerByte > 0 {
		for _, link := range node.GetLinkNodeProtocol().Established() {
			traffic := float64(link.GetTraffic())
			if traffic > 0 {
				// Mivel két végpontja van a linknek, az adás/vétel tranzakció energiáját kiszámoljuk
				// Energia (Joule) = Forgalom (Byte) * Fogyasztás (J/Byte)
				// Teljesítmény (Watt) = Energia (J) / Eltelt Idő (mp)
				networkPower += (traffic * powerProps.IslEnergyPerByte) / deltaT
			}
		}
	}
	// -------------------------------------------------

	return basePower + cpuPower + memoryPower + networkPower
}

// calculatePowerGeneration calculates the solar power generation
func (p *BatterySimPlugin) calculatePowerGeneration(node types.Node, powerProps types.PowerProperties) float64 {
	if p.thermalPlugin == nil {
		return 0
	}

	envHeat := p.thermalPlugin.GetEnvironmentalHeat(node)
	if envHeat.InEclipse {
		return 0
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

func (p *BatterySimPlugin) getBatteryProperties(node types.Node) types.BatteryProperties {
	if props, ok := p.batteryProps[node.GetName()]; ok {
		return props
	}
	if props, ok := p.batteryProps["default"]; ok {
		return props
	}
	return types.DefaultBatteryProperties()
}

func (p *BatterySimPlugin) getPowerProperties(node types.Node) types.PowerProperties {
	if props, ok := p.powerProps[node.GetName()]; ok {
		return props
	}
	if props, ok := p.powerProps["default"]; ok {
		return props
	}
	return types.DefaultPowerProperties()
}

func (p *BatterySimPlugin) GetBatteryState(node types.Node) (*types.SatellitePhysicalState, error) {
	state, ok := p.batteryStates[node.GetName()]
	if !ok {
		return nil, fmt.Errorf("battery state not found for node %s", node.GetName())
	}
	return state, nil
}

func (p *BatterySimPlugin) GetSOC(node types.Node) (float64, error) {
	state, err := p.GetBatteryState(node)
	if err != nil {
		return 0, err
	}
	return state.SOC, nil
}

func (p *BatterySimPlugin) IsCritical(node types.Node) bool {
	state, err := p.GetBatteryState(node)
	if err != nil {
		return false
	}
	batteryProps := p.getBatteryProperties(node)
	return state.SOC < batteryProps.CriticalSOC
}

func (p *BatterySimPlugin) GetAllStates() map[string]*types.SatellitePhysicalState {
	return p.batteryStates
}

func (p *BatterySimPlugin) Reset() {
	p.batteryStates = make(map[string]*types.SatellitePhysicalState)
}

// --- Integration with TelemetryExporterPlugin ---
func (p *BatterySimPlugin) GetNetEnergyChange(node types.Node) (float64, error) {
	state, err := p.GetBatteryState(node)
	if err != nil {
		return 0, err
	}
	return state.PowerGeneration - state.PowerConsumption, nil
}
