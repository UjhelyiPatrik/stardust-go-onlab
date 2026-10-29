package stateplugin

import (
	"encoding/gob"
	"fmt"
	"math"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/internal/node"
	"github.com/polaris-slo-cloud/stardust-go/pkg/helper"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.StatePlugin = (*ThermalEnvironmentStatePlugin)(nil)
var _ ThermalEnvironmentPlugin = (*ThermalEnvironmentStatePlugin)(nil)
var _ SunStatePlugin = (*ThermalEnvironmentStatePlugin)(nil)

const (
	solarRadiusMeters      = 6.957e8       // Physical radius of the Sun (m)
	astronomicalUnitMeters = 1.495978707e11 // 1 AU (mean Earth-Sun distance in m)
)

// ThermalEnvironmentPlugin defines the interface for thermal environment plugins
type ThermalEnvironmentPlugin interface {
	types.StatePlugin
	GetEnvironmentalHeat(node types.Node) types.EnvironmentalHeat
	GetOrbitalPosition(node types.Node) types.OrbitalPosition
}

// ThermalEnvironmentStatePlugin calculates and stores environmental heat and solar power generation for satellites.
// Supports both multi-threaded live calculation and O(1) precomputed playback.
type ThermalEnvironmentStatePlugin struct {
	mu                sync.RWMutex
	environmentalHeat map[types.Node]types.EnvironmentalHeat
	orbitalPositions  map[types.Node]types.OrbitalPosition
	states            []map[string]types.EnvironmentalHeat
	thermalProps      map[string]types.ThermalProperties
	powerProps        map[string]types.PowerProperties
	sunVector         types.Vector
	earthRadius       float64
	refTime           time.Time
	precomputedMode   bool
	currentStep       int
}

func NewThermalEnvironmentStatePlugin() *ThermalEnvironmentStatePlugin {
	// Fixed astronomical reference epoch: 2025 Vernal Equinox (ensures 100% deterministic & accurate solar geometry)
	vernalEquinox2025 := time.Date(2025, 3, 20, 9, 1, 0, 0, time.UTC)

	p := &ThermalEnvironmentStatePlugin{
		environmentalHeat: make(map[types.Node]types.EnvironmentalHeat),
		orbitalPositions:  make(map[types.Node]types.OrbitalPosition),
		states:            make([]map[string]types.EnvironmentalHeat, 0),
		thermalProps:      make(map[string]types.ThermalProperties),
		powerProps:        make(map[string]types.PowerProperties),
		sunVector:         types.Vector{X: 1, Y: 0, Z: 0},
		earthRadius:       6371000.0, // Earth mean radius in meters
		refTime:           vernalEquinox2025,
		precomputedMode:   false,
		currentStep:       -1,
	}

	configPath := "./resources/configs/physicalConfig.yaml"
	physicalConfig, err := configs.LoadPhysicalConfig(configPath)
	if err == nil && physicalConfig != nil {
		for satType, props := range physicalConfig.Thermal {
			p.SetThermalProperties(satType, types.ThermalProperties{
				ThermalMass:    props.ThermalMass,
				SurfaceArea:    props.SurfaceArea,
				Absorptivity:   props.Absorptivity,
				Emissivity:     props.Emissivity,
				MaxTemperature: props.MaxTemperature,
				MinTemperature: props.MinTemperature,
			})
		}
		for satType, props := range physicalConfig.Power {
			p.SetPowerProperties(satType, types.PowerProperties{
				SolarEfficiency:      props.SolarEfficiency,
				SolarPanelArea:       props.SolarPanelArea,
				MaxPowerGeneration:   props.MaxPowerGeneration,
				IdlePowerConsumption: props.IdlePowerConsumption,
				IslEnergyPerByte:     props.IslEnergyPerByte,
			})
		}
	} else {
		fmt.Printf("[WARN] ThermalEnvironmentStatePlugin: Could not load physical config: %v\n", err)
	}

	return p
}

// NewThermalEnvironmentPrecompPlugin creates a precomputed environment plugin that reads directly from PrecomputedSatState or .thermalEnv file.
func NewThermalEnvironmentPrecompPlugin(origFile string) *ThermalEnvironmentStatePlugin {
	p := NewThermalEnvironmentStatePlugin()
	p.precomputedMode = true

	filename := helper.ExtendFilename(origFile, ".thermalEnv")
	if file, err := os.Open(filename); err == nil {
		defer file.Close()
		decoder := gob.NewDecoder(file)
		_ = decoder.Decode(&p.states)
	}

	return p
}

func (p *ThermalEnvironmentStatePlugin) GetName() string {
	return "ThermalEnvironmentStatePlugin"
}

func (p *ThermalEnvironmentStatePlugin) GetType() reflect.Type {
	var dummy ThermalEnvironmentPlugin
	return reflect.TypeOf(dummy)
}

func (p *ThermalEnvironmentStatePlugin) GetEnvironmentalHeat(n types.Node) types.EnvironmentalHeat {
	// Fast O(1) lock-free path when using PrecomputedSatellite
	if pSat, ok := n.(*node.PrecomputedSatellite); ok {
		if st, hasState := pSat.GetCurrentPrecomputedState(); hasState {
			return types.EnvironmentalHeat{
				SolarHeat:            st.SolarHeat,
				AlbedoHeat:           st.AlbedoHeat,
				IRHeat:               st.IRHeat,
				TotalHeat:            st.TotalEnvironmentalHeat,
				SunlightExposure:     st.SunlightExposure,
				BetaAngle:            st.BetaAngle,
				InEclipse:            st.InEclipse,
				EclipseDepth:         st.EclipseDepth,
				SolarPowerGeneration: st.SolarPowerGeneration,
			}
		}
	}

	p.mu.RLock()
	if heat, ok := p.environmentalHeat[n]; ok {
		p.mu.RUnlock()
		return heat
	}
	if p.precomputedMode && p.currentStep >= 0 && p.currentStep < len(p.states) {
		if heat, ok := p.states[p.currentStep][n.GetName()]; ok {
			p.mu.RUnlock()
			return heat
		}
	}
	p.mu.RUnlock()
	return types.EnvironmentalHeat{}
}

func (p *ThermalEnvironmentStatePlugin) GetOrbitalPosition(n types.Node) types.OrbitalPosition {
	if pSat, ok := n.(*node.PrecomputedSatellite); ok {
		if st, hasState := pSat.GetCurrentPrecomputedState(); hasState {
			return types.OrbitalPosition{
				Position:     st.Position,
				BetaAngle:    st.BetaAngle,
				InEclipse:    st.InEclipse,
				EclipseDepth: st.EclipseDepth,
			}
		}
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.orbitalPositions[n]
}

func (p *ThermalEnvironmentStatePlugin) SetThermalProperties(nodeType string, props types.ThermalProperties) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.thermalProps[nodeType] = props
}

func (p *ThermalEnvironmentStatePlugin) SetPowerProperties(nodeType string, props types.PowerProperties) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.powerProps[nodeType] = props
}

func (p *ThermalEnvironmentStatePlugin) PostSimulationStep(simulationController types.SimulationController) {
	p.currentStep++
	nodes := simulationController.GetSatellites()
	if len(nodes) == 0 {
		return
	}

	// Fast path in precomputed mode if PrecomputedSatellite already holds the step state
	if pSat, ok := nodes[0].(*node.PrecomputedSatellite); ok {
		if _, hasState := pSat.GetCurrentPrecomputedState(); hasState {
			return
		}
	}

	simTime := simulationController.GetSimulationTime()
	p.updateSunVector(simTime)

	// Multi-threaded calculation across all satellites
	heats := make([]types.EnvironmentalHeat, len(nodes))
	orbPositions := make([]types.OrbitalPosition, len(nodes))
	indices := make([]int, len(nodes))
	for i := range nodes {
		indices[i] = i
	}

	helper.ParallelFor(indices, func(i int) {
		orbPos, heat := p.computeSatelliteEnvironment(nodes[i])
		orbPositions[i] = orbPos
		heats[i] = heat
	})

	p.mu.Lock()
	for i, n := range nodes {
		p.orbitalPositions[n] = orbPositions[i]
		p.environmentalHeat[n] = heats[i]
	}
	p.mu.Unlock()
}

func (p *ThermalEnvironmentStatePlugin) updateSunVector(simTime time.Time) {
	days := simTime.Sub(p.refTime).Hours() / 24.0
	// Mean anomaly & ecliptic longitude of the Sun (high-precision astronomical approximation)
	meanLongitude := 2.0 * math.Pi * days / 365.24219
	meanAnomaly := 2.0*math.Pi*(days/365.259636) - 0.041
	eclipticLongitude := meanLongitude + 0.0334*math.Sin(meanAnomaly) + 0.000349*math.Sin(2.0*meanAnomaly)
	epsilon := 23.439291 * math.Pi / 180.0 // Obliquity of the ecliptic

	sinLon := math.Sin(eclipticLongitude)
	p.sunVector = types.Vector{
		X: math.Cos(eclipticLongitude),
		Y: sinLon * math.Cos(epsilon),
		Z: sinLon * math.Sin(epsilon),
	}.Normalize()
}

func (p *ThermalEnvironmentStatePlugin) computeSatelliteEnvironment(n types.Node) (types.OrbitalPosition, types.EnvironmentalHeat) {
	position := n.GetPosition()

	distSq := position.X*position.X + position.Y*position.Y + position.Z*position.Z
	dist := math.Sqrt(distSq)
	if dist == 0 {
		return types.OrbitalPosition{}, types.EnvironmentalHeat{}
	}

	// Projection of satellite position onto the unit sun vector
	dotProduct := position.X*p.sunVector.X + position.Y*p.sunVector.Y + position.Z*p.sunVector.Z
	normDotProduct := math.Max(-1.0, math.Min(1.0, dotProduct/dist))

	// Calculate precise conical (Umbra + Penumbra) orbital eclipse state
	orbitalPos := p.calculateOrbitalPosition(position, dist, distSq, dotProduct, normDotProduct)

	thermalProps := p.getThermalProperties(n)
	powerProps := p.getPowerProperties(n)

	var solarHeat, albedoHeat, irHeat, solarPowerGen float64
	sunlightExposure := 0.0

	if !orbitalPos.InEclipse {
		sunlightExposure = p.calculateSunlightExposure(normDotProduct, orbitalPos)
		solarHeat = p.calculateSolarHeat(thermalProps, sunlightExposure)
		albedoHeat = p.calculateAlbedoHeat(dist, normDotProduct, thermalProps)
		solarPowerGen = powerProps.MaxPowerGeneration * sunlightExposure * powerProps.SolarEfficiency
	}

	// IR heat is continuously radiated from Earth
	irHeat = p.calculateIRHeat(dist, thermalProps)
	totalHeat := solarHeat + albedoHeat + irHeat

	heat := types.EnvironmentalHeat{
		SolarHeat:            solarHeat,
		AlbedoHeat:           albedoHeat,
		IRHeat:               irHeat,
		TotalHeat:            totalHeat,
		SunlightExposure:     sunlightExposure,
		BetaAngle:            orbitalPos.BetaAngle,
		InEclipse:            orbitalPos.InEclipse,
		EclipseDepth:         orbitalPos.EclipseDepth,
		SolarPowerGeneration: solarPowerGen,
	}

	return orbitalPos, heat
}

// calculateOrbitalPosition determines the conical (Umbra + Penumbra) eclipse state.
func (p *ThermalEnvironmentStatePlugin) calculateOrbitalPosition(pos types.Vector, dist float64, distSq float64, dotProduct float64, normDot float64) types.OrbitalPosition {
	orbitalPos := types.OrbitalPosition{
		Position:     pos,
		TrueAnomaly:  0,
		BetaAngle:    math.Acos(math.Min(1.0, math.Abs(normDot))) * 180.0 / math.Pi,
		InEclipse:    false,
		EclipseDepth: 0.0,
	}

	// Satellite can only be in Earth's shadow if it is on the anti-solar side (dotProduct < 0)
	if dotProduct < 0 {
		perpDistSq := distSq - (dotProduct * dotProduct)
		if perpDistSq < 0 {
			perpDistSq = 0
		}
		perpDist := math.Sqrt(perpDistSq)
		shadowAxisDist := -dotProduct

		// Conical Umbra and Penumbra geometry taking atmospheric refraction (+1% effective radius) into account
		effEarthRadius := p.earthRadius * 1.005
		tanUmbra := (solarRadiusMeters - effEarthRadius) / astronomicalUnitMeters
		tanPenumbra := (solarRadiusMeters + effEarthRadius) / astronomicalUnitMeters

		umbraRadius := effEarthRadius - shadowAxisDist*tanUmbra
		penumbraRadius := effEarthRadius + shadowAxisDist*tanPenumbra

		if perpDist <= umbraRadius {
			orbitalPos.InEclipse = true
			orbitalPos.EclipseDepth = 1.0
		} else if perpDist < penumbraRadius && penumbraRadius > umbraRadius {
			// In Penumbra (partial solar occultation)
			orbitalPos.InEclipse = false
			orbitalPos.EclipseDepth = (penumbraRadius - perpDist) / (penumbraRadius - umbraRadius)
		}
	}

	return orbitalPos
}

func (p *ThermalEnvironmentStatePlugin) calculateSunlightExposure(normDot float64, orbitalPos types.OrbitalPosition) float64 {
	if orbitalPos.InEclipse {
		return 0.0
	}

	// Sun-tracking solar array orientation with penumbra attenuation
	baseExposure := 1.0 - orbitalPos.EclipseDepth
	if baseExposure < 0 {
		baseExposure = 0
	}
	if baseExposure > 1.0 {
		baseExposure = 1.0
	}
	return baseExposure
}

func (p *ThermalEnvironmentStatePlugin) calculateSolarHeat(thermalProps types.ThermalProperties, exposure float64) float64 {
	return thermalProps.Absorptivity * types.SolarConstant * thermalProps.SurfaceArea * exposure
}

func (p *ThermalEnvironmentStatePlugin) calculateAlbedoHeat(dist float64, normDot float64, thermalProps types.ThermalProperties) float64 {
	if normDot <= 0 {
		return 0.0
	}
	viewFactor := math.Pow(p.earthRadius/dist, 2)
	return thermalProps.Absorptivity * types.SolarConstant * types.EarthAlbedoFactor * thermalProps.SurfaceArea * viewFactor * normDot
}

func (p *ThermalEnvironmentStatePlugin) calculateIRHeat(dist float64, thermalProps types.ThermalProperties) float64 {
	viewFactor := math.Pow(p.earthRadius/dist, 2)
	return thermalProps.Emissivity * types.StefanBoltzmannConstant * thermalProps.SurfaceArea * viewFactor * math.Pow(types.EarthTemperature, 4)
}

func (p *ThermalEnvironmentStatePlugin) getThermalProperties(n types.Node) types.ThermalProperties {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if props, ok := p.thermalProps[n.GetName()]; ok {
		return props
	}
	if props, ok := p.thermalProps["default"]; ok {
		return props
	}
	return types.DefaultThermalProperties()
}

func (p *ThermalEnvironmentStatePlugin) getPowerProperties(n types.Node) types.PowerProperties {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if props, ok := p.powerProps[n.GetName()]; ok {
		return props
	}
	if props, ok := p.powerProps["default"]; ok {
		return props
	}
	return types.DefaultPowerProperties()
}

func (p *ThermalEnvironmentStatePlugin) AddState(simulationController types.SimulationController) {
	// No-op: thermal and solar states are serialized directly inside PrecomputedSatState chunks.
}

func (p *ThermalEnvironmentStatePlugin) Save(origFile string) {
	// No-op: thermal and solar states are serialized directly inside PrecomputedSatState chunks.
}

// IsEclipse is required to satisfy the SunStatePlugin interface for the Analytics telemetry
func (p *ThermalEnvironmentStatePlugin) IsEclipse(n types.Node) bool {
	if pSat, ok := n.(*node.PrecomputedSatellite); ok {
		if st, hasState := pSat.GetCurrentPrecomputedState(); hasState {
			return st.InEclipse
		}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if pos, ok := p.orbitalPositions[n]; ok {
		return pos.InEclipse
	}
	if p.precomputedMode && p.currentStep >= 0 && p.currentStep < len(p.states) {
		if heat, ok := p.states[p.currentStep][n.GetName()]; ok {
			return heat.InEclipse
		}
	}
	return false
}

// GetSunlightExposure returns the current sunlight exposure for a satellite (0.0 to 1.0)
func (p *ThermalEnvironmentStatePlugin) GetSunlightExposure(n types.Node) float64 {
	if pSat, ok := n.(*node.PrecomputedSatellite); ok {
		if st, hasState := pSat.GetCurrentPrecomputedState(); hasState {
			return st.SunlightExposure
		}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if heat, ok := p.environmentalHeat[n]; ok {
		return heat.SunlightExposure
	}
	if p.precomputedMode && p.currentStep >= 0 && p.currentStep < len(p.states) {
		if heat, ok := p.states[p.currentStep][n.GetName()]; ok {
			return heat.SunlightExposure
		}
	}
	return 0.0
}
