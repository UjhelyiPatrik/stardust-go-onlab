package stateplugin

import (
	"encoding/gob"
	"fmt"
	"math"
	"os"
	"reflect"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/pkg/helper"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.StatePlugin = (*ThermalEnvironmentStatePlugin)(nil)
var _ ThermalEnvironmentPlugin = (*ThermalEnvironmentStatePlugin)(nil)
var _ SunStatePlugin = (*ThermalEnvironmentStatePlugin)(nil)

// ThermalEnvironmentPlugin defines the interface for thermal environment plugins
type ThermalEnvironmentPlugin interface {
	types.StatePlugin

	// GetEnvironmentalHeat returns the environmental heat for a satellite
	GetEnvironmentalHeat(node types.Node) types.EnvironmentalHeat

	// GetOrbitalPosition returns the orbital position of a satellite
	GetOrbitalPosition(node types.Node) types.OrbitalPosition
}

// ThermalEnvironmentStatePlugin calculates and stores environmental heat for satellites
type ThermalEnvironmentStatePlugin struct {
	// Environmental heat map: node -> heat values
	environmentalHeat map[types.Node]types.EnvironmentalHeat

	// Orbital positions map: node -> orbital position
	orbitalPositions map[types.Node]types.OrbitalPosition

	// Stored states for serialization
	states []map[string]types.EnvironmentalHeat

	// Physical properties per node type
	thermalProps map[string]types.ThermalProperties
	powerProps   map[string]types.PowerProperties

	// Sun vector (normalized)
	sunVector types.Vector

	// Earth radius in meters
	earthRadius float64

	// Simulation reference time
	refTime time.Time
}

func NewThermalEnvironmentStatePlugin() *ThermalEnvironmentStatePlugin {
	return &ThermalEnvironmentStatePlugin{
		environmentalHeat: make(map[types.Node]types.EnvironmentalHeat),
		orbitalPositions:  make(map[types.Node]types.OrbitalPosition),
		states:            make([]map[string]types.EnvironmentalHeat, 0),
		thermalProps:      make(map[string]types.ThermalProperties),
		powerProps:        make(map[string]types.PowerProperties),
		sunVector:         types.Vector{X: 1, Y: 0, Z: 0}, // Default sun direction
		earthRadius:       6371000.0,                      // Earth radius in meters
		refTime:           time.Now(),
	}
}

// GetName returns the plugin name
func (p *ThermalEnvironmentStatePlugin) GetName() string {
	return "ThermalEnvironmentPlugin"
}

// GetType returns the plugin type
func (p *ThermalEnvironmentStatePlugin) GetType() reflect.Type {
	var dummy ThermalEnvironmentPlugin
	return reflect.TypeOf(dummy)
}

// GetEnvironmentalHeat returns the environmental heat for a node
func (p *ThermalEnvironmentStatePlugin) GetEnvironmentalHeat(node types.Node) types.EnvironmentalHeat {
	return p.environmentalHeat[node]
}

// GetOrbitalPosition returns the orbital position for a node
func (p *ThermalEnvironmentStatePlugin) GetOrbitalPosition(node types.Node) types.OrbitalPosition {
	return p.orbitalPositions[node]
}

// SetThermalProperties sets the thermal properties for a node type
func (p *ThermalEnvironmentStatePlugin) SetThermalProperties(nodeType string, props types.ThermalProperties) {
	p.thermalProps[nodeType] = props
}

// SetPowerProperties sets the power properties for a node type
func (p *ThermalEnvironmentStatePlugin) SetPowerProperties(nodeType string, props types.PowerProperties) {
	p.powerProps[nodeType] = props
}

// PostSimulationStep calculates the environmental heat for all satellites
func (p *ThermalEnvironmentStatePlugin) PostSimulationStep(simulationController types.SimulationController) {
	nodes := simulationController.GetSatellites()
	simTime := simulationController.GetSimulationTime()

	// Update sun vector based on simulation time (simplified)
	p.updateSunVector(simTime)

	for _, node := range nodes {
		p.calculateEnvironmentalHeat(node, simTime)
	}
}

// updateSunVector updates the sun direction vector based on time
func (p *ThermalEnvironmentStatePlugin) updateSunVector(simTime time.Time) {
	days := simTime.Sub(p.refTime).Hours() / 24.0
	angle := days * 2 * math.Pi / 365.25

	// Tengelyferdeség (obliquity of the ecliptic) ~ 23.44 fok
	epsilon := 23.44 * math.Pi / 180.0

	p.sunVector = types.Vector{
		X: math.Cos(angle),
		Y: math.Sin(angle) * math.Cos(epsilon),
		Z: math.Sin(angle) * math.Sin(epsilon),
	}
}

// calculateEnvironmentalHeat calculates all heat inputs for a satellite
func (p *ThermalEnvironmentStatePlugin) calculateEnvironmentalHeat(node types.Node, simTime time.Time) {
	position := node.GetPosition()

	// Calculate orbital position
	orbitalPos := p.calculateOrbitalPosition(node, position)
	p.orbitalPositions[node] = orbitalPos

	// Get thermal properties (use defaults if not set)
	thermalProps := p.getThermalProperties(node)
	_ = p.getPowerProperties(node) // Power properties available for future use

	// Calculate heat components
	var solarHeat, albedoHeat, irHeat float64
	sunlightExposure := 1.0

	if orbitalPos.InEclipse {
		// In eclipse: no direct solar or albedo
		solarHeat = 0
		albedoHeat = 0
		sunlightExposure = 0
	} else {
		// Calculate solar heat
		sunlightExposure = p.calculateSunlightExposure(position, orbitalPos)
		solarHeat = p.calculateSolarHeat(position, thermalProps, sunlightExposure)

		// Calculate albedo heat
		albedoHeat = p.calculateAlbedoHeat(position, thermalProps)

		// Calculate IR heat (always present, even in eclipse)
		irHeat = p.calculateIRHeat(position, thermalProps)
	}

	// Total incoming heat
	totalHeat := solarHeat + albedoHeat + irHeat

	// Store environmental heat
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

// calculateOrbitalPosition determines the orbital position and eclipse state
func (p *ThermalEnvironmentStatePlugin) calculateOrbitalPosition(node types.Node, position types.Vector) types.OrbitalPosition {
	pos := types.OrbitalPosition{
		Position:     position,
		TrueAnomaly:  0,
		BetaAngle:    0,
		InEclipse:    false,
		EclipseDepth: 0,
	}

	// Calculate distance from Earth center
	dist := math.Sqrt(position.X*position.X + position.Y*position.Y + position.Z*position.Z)

	// Calculate dot product with sun vector
	dotProduct := (position.X*p.sunVector.X + position.Y*p.sunVector.Y + position.Z*p.sunVector.Z) / dist

	// Calculate beta angle (angle between position vector and sun direction)
	// Simplified: use dot product as proxy
	pos.BetaAngle = math.Acos(math.Abs(dotProduct)) * 180 / math.Pi

	// Check for eclipse (Earth shadow)
	// Simplified: if satellite is on the night side and behind Earth
	if dotProduct < 0 {
		// Calculate penumbra/umbra
		earthAngularRadius := math.Asin(p.earthRadius / dist)

		// Sun angular radius (approx 0.27 degrees)
		sunAngularRadius := 0.00465 // radians

		// Angle from Earth's shadow axis
		shadowAngle := math.Acos(-dotProduct)

		if shadowAngle > earthAngularRadius+sunAngularRadius {
			pos.InEclipse = false
			pos.EclipseDepth = 0
		} else if shadowAngle > earthAngularRadius-sunAngularRadius {
			// Partial eclipse (penumbra)
			pos.InEclipse = true
			pos.EclipseDepth = 1.0 - (shadowAngle-earthAngularRadius+sunAngularRadius)/(2*sunAngularRadius)
			if pos.EclipseDepth > 1.0 {
				pos.EclipseDepth = 1.0
			}
		} else {
			// Full eclipse (umbra)
			pos.InEclipse = true
			pos.EclipseDepth = 1.0
		}
	}

	return pos
}

// calculateSunlightExposure calculates the sunlight exposure factor (0-1)
func (p *ThermalEnvironmentStatePlugin) calculateSunlightExposure(position types.Vector, orbitalPos types.OrbitalPosition) float64 {
	if orbitalPos.InEclipse {
		return 0.0
	}

	// Calculate the angle between the satellite position and sun direction
	dist := math.Sqrt(position.X*position.X + position.Y*position.Y + position.Z*position.Z)
	dotProduct := (position.X*p.sunVector.X + position.Y*p.sunVector.Y + position.Z*p.sunVector.Z) / dist

	// Exposure is based on how directly the sun illuminates the satellite
	// For a LEO satellite, we assume the solar panels are oriented toward the sun
	return math.Max(0, dotProduct)
}

// calculateSolarHeat calculates direct solar heat input
func (p *ThermalEnvironmentStatePlugin) calculateSolarHeat(position types.Vector, thermalProps types.ThermalProperties, exposure float64) float64 {
	// Q_solar = α * S * A * cos(θ)
	// Where S is solar constant, A is surface area, α is absorptivity
	solarConstant := types.SolarConstant

	// Simplified: assume constant solar constant at Earth distance
	// In a full implementation, this would use: distanceFactor := (r / 1 AU)^2

	return thermalProps.Absorptivity * solarConstant * thermalProps.SurfaceArea * exposure
}

// calculateAlbedoHeat calculates Earth albedo heat input
func (p *ThermalEnvironmentStatePlugin) calculateAlbedoHeat(position types.Vector, thermalProps types.ThermalProperties) float64 {
	dist := math.Sqrt(position.X*position.X + position.Y*position.Y + position.Z*position.Z)

	// Föld-Nap megvilágítási szög (dot product)
	dotProduct := (position.X*p.sunVector.X + position.Y*p.sunVector.Y + position.Z*p.sunVector.Z) / dist

	// Ha a műhold a sötét oldalon van, nincs albedó
	if dotProduct <= 0 {
		return 0.0
	}

	viewFactor := math.Pow(p.earthRadius/dist, 2)
	albedoFactor := types.EarthAlbedoFactor
	solarConstant := types.SolarConstant

	// Az albedó intenzitása függ a beesési szögtől is (Lambert-féle koszinusz törvény)
	return thermalProps.Absorptivity * solarConstant * albedoFactor * thermalProps.SurfaceArea * viewFactor * dotProduct
}

// calculateIRHeat calculates Earth infrared heat input
func (p *ThermalEnvironmentStatePlugin) calculateIRHeat(position types.Vector, thermalProps types.ThermalProperties) float64 {
	// Distance from Earth center
	dist := math.Sqrt(position.X*position.X + position.Y*position.Y + position.Z*position.Z)

	// View factor to Earth
	viewFactor := math.Pow(p.earthRadius/dist, 2)

	// IR heat = ε * σ * A * F * T_earth^4
	// Using emissivity instead of absorptivity for IR (Kirchhoff's law)
	earthTemp := types.EarthTemperature
	sigma := types.StefanBoltzmannConstant

	return thermalProps.Emissivity * sigma * thermalProps.SurfaceArea * viewFactor * math.Pow(earthTemp, 4)
}

// getThermalProperties returns thermal properties for a node (with defaults)
func (p *ThermalEnvironmentStatePlugin) getThermalProperties(node types.Node) types.ThermalProperties {
	if props, ok := p.thermalProps[node.GetName()]; ok {
		return props
	}
	if props, ok := p.thermalProps["default"]; ok {
		return props
	}
	return types.DefaultThermalProperties()
}

// getPowerProperties returns power properties for a node (with defaults)
func (p *ThermalEnvironmentStatePlugin) getPowerProperties(node types.Node) types.PowerProperties {
	if props, ok := p.powerProps[node.GetName()]; ok {
		return props
	}
	if props, ok := p.powerProps["default"]; ok {
		return props
	}
	return types.DefaultPowerProperties()
}

// AddState saves the current state for serialization
func (p *ThermalEnvironmentStatePlugin) AddState(simulationController types.SimulationController) {
	stateMap := make(map[string]types.EnvironmentalHeat)
	for node, heat := range p.environmentalHeat {
		stateMap[node.GetName()] = heat
	}
	p.states = append(p.states, stateMap)
}

// Save saves the states to a file
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

// GetSunlightExposure returns the current sunlight exposure for a satellite (0.0 to 1.0)
func (p *ThermalEnvironmentStatePlugin) GetSunlightExposure(node types.Node) float64 {
	if heat, ok := p.environmentalHeat[node]; ok {
		return heat.SunlightExposure
	}
	return 0.0 // Default if not calculated yet
}
