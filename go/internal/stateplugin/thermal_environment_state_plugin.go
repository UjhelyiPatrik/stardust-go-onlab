package stateplugin

import (
	"encoding/gob"
	"fmt"
	"math"
	"os"
	"reflect"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/pkg/helper"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.StatePlugin = (*ThermalEnvironmentStatePlugin)(nil)
var _ ThermalEnvironmentPlugin = (*ThermalEnvironmentStatePlugin)(nil)
var _ SunStatePlugin = (*ThermalEnvironmentStatePlugin)(nil)

// ThermalEnvironmentPlugin defines the interface for thermal environment plugins
type ThermalEnvironmentPlugin interface {
	types.StatePlugin
	GetEnvironmentalHeat(node types.Node) types.EnvironmentalHeat
	GetOrbitalPosition(node types.Node) types.OrbitalPosition
}

// ThermalEnvironmentStatePlugin calculates and stores environmental heat for satellites
type ThermalEnvironmentStatePlugin struct {
	environmentalHeat map[types.Node]types.EnvironmentalHeat
	orbitalPositions  map[types.Node]types.OrbitalPosition
	states            []map[string]types.EnvironmentalHeat
	thermalProps      map[string]types.ThermalProperties
	powerProps        map[string]types.PowerProperties
	sunVector         types.Vector
	earthRadius       float64
	refTime           time.Time
}

func NewThermalEnvironmentStatePlugin() *ThermalEnvironmentStatePlugin {
	p := &ThermalEnvironmentStatePlugin{
		environmentalHeat: make(map[types.Node]types.EnvironmentalHeat),
		orbitalPositions:  make(map[types.Node]types.OrbitalPosition),
		states:            make([]map[string]types.EnvironmentalHeat, 0),
		thermalProps:      make(map[string]types.ThermalProperties),
		powerProps:        make(map[string]types.PowerProperties),
		sunVector:         types.Vector{X: 1, Y: 0, Z: 0},
		earthRadius:       6371000.0, // Earth radius in meters
		refTime:           time.Now(),
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
			})
		}
	} else {
		fmt.Printf("[WARN] ThermalEnvironmentStatePlugin: Could not load physical config: %v\n", err)
	}

	return p
}

func (p *ThermalEnvironmentStatePlugin) GetName() string {
	return "ThermalEnvironmentPlugin"
}

func (p *ThermalEnvironmentStatePlugin) GetType() reflect.Type {
	var dummy ThermalEnvironmentPlugin
	return reflect.TypeOf(dummy)
}

func (p *ThermalEnvironmentStatePlugin) GetEnvironmentalHeat(node types.Node) types.EnvironmentalHeat {
	return p.environmentalHeat[node]
}

func (p *ThermalEnvironmentStatePlugin) GetOrbitalPosition(node types.Node) types.OrbitalPosition {
	return p.orbitalPositions[node]
}

func (p *ThermalEnvironmentStatePlugin) SetThermalProperties(nodeType string, props types.ThermalProperties) {
	p.thermalProps[nodeType] = props
}

func (p *ThermalEnvironmentStatePlugin) SetPowerProperties(nodeType string, props types.PowerProperties) {
	p.powerProps[nodeType] = props
}

func (p *ThermalEnvironmentStatePlugin) PostSimulationStep(simulationController types.SimulationController) {
	nodes := simulationController.GetSatellites()
	simTime := simulationController.GetSimulationTime()

	p.updateSunVector(simTime)

	for _, node := range nodes {
		p.calculateEnvironmentalHeat(node)
	}
}

func (p *ThermalEnvironmentStatePlugin) updateSunVector(simTime time.Time) {
	days := simTime.Sub(p.refTime).Hours() / 24.0
	angle := days * 2 * math.Pi / 365.25
	epsilon := 23.44 * math.Pi / 180.0

	p.sunVector = types.Vector{
		X: math.Cos(angle),
		Y: math.Sin(angle) * math.Cos(epsilon),
		Z: math.Sin(angle) * math.Sin(epsilon),
	}
}

func (p *ThermalEnvironmentStatePlugin) calculateEnvironmentalHeat(node types.Node) {
	position := node.GetPosition()

	// OPTIMIZATION: Calculate distance and dot product ONCE per satellite
	distSq := position.X*position.X + position.Y*position.Y + position.Z*position.Z
	dist := math.Sqrt(distSq)

	// Projection of satellite position onto the sun vector
	dotProduct := position.X*p.sunVector.X + position.Y*p.sunVector.Y + position.Z*p.sunVector.Z

	// Normalized projection (-1.0 to 1.0) for angular calculations
	normDotProduct := dotProduct / dist

	// Calculate precise orbital position and eclipse state
	orbitalPos := p.calculateOrbitalPosition(position, dist, distSq, dotProduct, normDotProduct)
	p.orbitalPositions[node] = orbitalPos

	thermalProps := p.getThermalProperties(node)

	var solarHeat, albedoHeat, irHeat float64
	sunlightExposure := 1.0

	if orbitalPos.InEclipse {
		// In Umbra (full shadow)
		solarHeat = 0
		albedoHeat = 0
		sunlightExposure = 0
	} else {
		// Calculate precise sunlight exposure (accounts for Penumbra)
		sunlightExposure = p.calculateSunlightExposure(normDotProduct, orbitalPos)
		solarHeat = p.calculateSolarHeat(thermalProps, sunlightExposure)

		// Albedo is only generated from the sunlit side of Earth
		albedoHeat = p.calculateAlbedoHeat(dist, normDotProduct, thermalProps)
	}

	// IR heat is always present (Earth continuously radiates heat)
	irHeat = p.calculateIRHeat(dist, thermalProps)

	totalHeat := solarHeat + albedoHeat + irHeat

	p.environmentalHeat[node] = types.EnvironmentalHeat{
		SolarHeat:        solarHeat,
		AlbedoHeat:       albedoHeat,
		IRHeat:           irHeat,
		TotalHeat:        totalHeat,
		SunlightExposure: sunlightExposure,
		BetaAngle:        orbitalPos.BetaAngle,
		InEclipse:        orbitalPos.InEclipse,
	}
}

// calculateOrbitalPosition determines the true cylindrical eclipse state
func (p *ThermalEnvironmentStatePlugin) calculateOrbitalPosition(pos types.Vector, dist float64, distSq float64, dotProduct float64, normDot float64) types.OrbitalPosition {
	orbitalPos := types.OrbitalPosition{
		Position:     pos,
		TrueAnomaly:  0,
		BetaAngle:    math.Acos(math.Abs(normDot)) * 180 / math.Pi,
		InEclipse:    false,
		EclipseDepth: 0,
	}

	// ARCHITECTURAL FIX: Cylindrical Shadow Model
	// A satellite is only in eclipse if it is BEHIND the Earth relative to the sun (dotProduct < 0)
	if dotProduct < 0 {
		// Calculate the perpendicular distance squared from the Earth-Sun axis
		// Pythagorean theorem: perpendicular^2 = hypotenuse^2 - adjacent^2
		distToAxisSq := distSq - (dotProduct * dotProduct)
		earthRadiusSq := p.earthRadius * p.earthRadius

		// If the satellite is further from the axis than the Earth's radius, it is in sunlight
		if distToAxisSq < earthRadiusSq {
			// Satellite is physically within the cylinder of Earth's shadow

			// Calculate penumbra/umbra transitions
			earthAngularRadius := math.Asin(p.earthRadius / dist)
			sunAngularRadius := 0.00465 // Approx 0.27 degrees in radians
			shadowAngle := math.Acos(-normDot)

			if shadowAngle > earthAngularRadius+sunAngularRadius {
				orbitalPos.InEclipse = false
			} else if shadowAngle > earthAngularRadius-sunAngularRadius {
				// Penumbra (Partial Eclipse)
				orbitalPos.InEclipse = false // Treat as sunlight, but with reduced exposure
				orbitalPos.EclipseDepth = 1.0 - (shadowAngle-earthAngularRadius+sunAngularRadius)/(2*sunAngularRadius)
			} else {
				// Umbra (Full Eclipse)
				orbitalPos.InEclipse = true
				orbitalPos.EclipseDepth = 1.0
			}
		}
	}

	return orbitalPos
}

func (p *ThermalEnvironmentStatePlugin) calculateSunlightExposure(normDot float64, orbitalPos types.OrbitalPosition) float64 {
	if orbitalPos.InEclipse {
		return 0.0
	}

	// Base exposure based on the angle facing the sun (assuming optimal solar panel orientation)
	baseExposure := math.Max(0, normDot)

	// Apply penumbra dimming if applicable
	if orbitalPos.EclipseDepth > 0 {
		baseExposure = baseExposure * (1.0 - orbitalPos.EclipseDepth)
	}

	return baseExposure
}

func (p *ThermalEnvironmentStatePlugin) calculateSolarHeat(thermalProps types.ThermalProperties, exposure float64) float64 {
	return thermalProps.Absorptivity * types.SolarConstant * thermalProps.SurfaceArea * exposure
}

func (p *ThermalEnvironmentStatePlugin) calculateAlbedoHeat(dist float64, normDot float64, thermalProps types.ThermalProperties) float64 {
	// If the satellite is over the dark side of Earth, there is no albedo reflection
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

func (p *ThermalEnvironmentStatePlugin) getThermalProperties(node types.Node) types.ThermalProperties {
	if props, ok := p.thermalProps[node.GetName()]; ok {
		return props
	}
	if props, ok := p.thermalProps["default"]; ok {
		return props
	}
	return types.DefaultThermalProperties()
}

func (p *ThermalEnvironmentStatePlugin) getPowerProperties(node types.Node) types.PowerProperties {
	if props, ok := p.powerProps[node.GetName()]; ok {
		return props
	}
	if props, ok := p.powerProps["default"]; ok {
		return props
	}
	return types.DefaultPowerProperties()
}

func (p *ThermalEnvironmentStatePlugin) AddState(simulationController types.SimulationController) {
	stateMap := make(map[string]types.EnvironmentalHeat)
	for node, heat := range p.environmentalHeat {
		stateMap[node.GetName()] = heat
	}
	p.states = append(p.states, stateMap)
}

func (p *ThermalEnvironmentStatePlugin) Save(origFile string) {
	filename := helper.ExtendFilename(origFile, ".thermalEnv")
	file, err := os.Create(filename)
	if err != nil {
		fmt.Printf("Error creating thermal environment file: %v\n", err)
		return
	}
	defer file.Close()
	encoder := gob.NewEncoder(file)
	encoder.Encode(p.states)
}

// IsEclipse is required to satisfy the SunStatePlugin interface for the Analytics telemetry
func (p *ThermalEnvironmentStatePlugin) IsEclipse(node types.Node) bool {
	if pos, ok := p.orbitalPositions[node]; ok {
		return pos.InEclipse
	}
	return false
}

// GetSunlightExposure returns the current sunlight exposure for a satellite (0.0 to 1.0)
func (p *ThermalEnvironmentStatePlugin) GetSunlightExposure(node types.Node) float64 {
	if heat, ok := p.environmentalHeat[node]; ok {
		return heat.SunlightExposure
	}
	return 0.0
}
