package types

import "time"

// =============================================================================
// Thermal Model Types
// =============================================================================

// ThermalProperties holds the thermal physical properties of a satellite
type ThermalProperties struct {
	// Thermal mass (m * cp) in J/K - determines heat inertia
	ThermalMass float64 `json:"thermalMass" yaml:"thermalMass"`

	// Surface area for radiation in m²
	SurfaceArea float64 `json:"surfaceArea" yaml:"surfaceArea"`

	// Absorptivity (α) - solar absorption factor (0-1)
	Absorptivity float64 `json:"absorptivity" yaml:"absorptivity"`

	// Emissivity (ε) - thermal radiation emission factor (0-1)
	Emissivity float64 `json:"emissivity" yaml:"emissivity"`

	// Maximum safe temperature in Kelvin
	MaxTemperature float64 `json:"maxTemperature" yaml:"maxTemperature"`

	// Minimum safe temperature in Kelvin
	MinTemperature float64 `json:"minTemperature" yaml:"minTemperature"`
}

// DefaultThermalProperties returns default values for a 3U CubeSat
func DefaultThermalProperties() ThermalProperties {
	return ThermalProperties{
		ThermalMass:    3500.0,   // J/K for 3U CubeSat
		SurfaceArea:    0.14,      // m²
		Absorptivity:   0.92,      // typical for solar panels
		Emissivity:     0.85,      // typical for black paint
		MaxTemperature: 333.15,    // 60°C in Kelvin
		MinTemperature: 253.15,    // -20°C in Kelvin
	}
}

// =============================================================================
// Battery Model Types
// =============================================================================

// BatteryProperties holds the battery physical properties of a satellite
type BatteryProperties struct {
	// Total capacity in Ampere-hours
	Capacity float64 `json:"capacity" yaml:"capacity"`

	// Nominal voltage in Volts
	NominalVoltage float64 `json:"nominalVoltage" yaml:"nominalVoltage"`

	// Coulomb efficiency (0-1)
	CoulombEfficiency float64 `json:"coulombEfficiency" yaml:"coulombEfficiency"`

	// Maximum depth of discharge (0-1) - safety limit
	MaxDoD float64 `json:"maxDoD" yaml:"maxDoD"`

	// Critical SOC threshold (0-1) - trigger offloading
	CriticalSOC float64 `json:"criticalSOC" yaml:"criticalSOC"`

	// Internal resistance in Ohms (temperature-dependent)
	InternalResistance float64 `json:"internalResistance" yaml:"internalResistance"`

	// Open circuit voltage at full charge in Volts
	MaxVoltage float64 `json:"maxVoltage" yaml:"maxVoltage"`

	// Open circuit voltage at empty in Volts
	MinVoltage float64 `json:"minVoltage" yaml:"minVoltage"`
}

// DefaultBatteryProperties returns default values for a 3U CubeSat
func DefaultBatteryProperties() BatteryProperties {
	return BatteryProperties{
		Capacity:            10.0,    // 10 Ah
		NominalVoltage:      7.4,     // 2S Li-ion
		CoulombEfficiency:   0.95,    // 95%
		MaxDoD:              0.8,     // 80% max discharge
		CriticalSOC:         0.2,     // 20% critical
		InternalResistance:  0.1,     // 100 mOhm
		MaxVoltage:          8.4,     // 4.2V per cell * 2
		MinVoltage:          6.0,     // 3.0V per cell * 2
	}
}

// =============================================================================
// Power Generation Model Types
// =============================================================================

// PowerProperties holds the power generation properties of a satellite
type PowerProperties struct {
	// Solar panel efficiency (0-1)
	SolarEfficiency float64 `json:"solarEfficiency" yaml:"solarEfficiency"`

	// Total solar panel area in m²
	SolarPanelArea float64 `json:"solarPanelArea" yaml:"solarPanelArea"`

	// Maximum power generation in Watts (at perpendicular sun angle)
	MaxPowerGeneration float64 `json:"maxPowerGeneration" yaml:"maxPowerGeneration"`

	// Power consumption when idle in Watts
	IdlePowerConsumption float64 `json:"idlePowerConsumption" yaml:"idlePowerConsumption"`
}

// DefaultPowerProperties returns default values for a 3U CubeSat
func DefaultPowerProperties() PowerProperties {
	return PowerProperties{
		SolarEfficiency:      0.28,    // Typical multi-junction GaAs
		SolarPanelArea:       0.08,    // m²
		MaxPowerGeneration:   40.0,    // Watts
		IdlePowerConsumption: 2.0,     // Watts
	}
}

// =============================================================================
// Environmental Heat Input
// =============================================================================

// EnvironmentalHeat represents the environmental heat inputs to a satellite
type EnvironmentalHeat struct {
	// Direct solar radiation (W)
	SolarHeat float64 `json:"solarHeat"`

	// Earth albedo radiation (W)
	AlbedoHeat float64 `json:"albedoHeat"`

	// Earth infrared radiation (W)
	IRHeat float64 `json:"irHeat"`

	// Total incoming heat (W)
	TotalHeat float64 `json:"totalHeat"`

	// Sunlight exposure factor (0-1)
	SunlightExposure float64 `json:"sunlightExposure"`

	// Beta angle in degrees
	BetaAngle float64 `json:"betaAngle"`

	// Is in eclipse (shadow)
	InEclipse bool `json:"inEclipse"`
}

// =============================================================================
// Satellite Physical State
// =============================================================================

// SatellitePhysicalState holds the real-time physical state of a satellite
type SatellitePhysicalState struct {
	// Node name
	NodeName string `json:"nodeName"`

	// Current temperature in Kelvin
	Temperature float64 `json:"temperature"`

	// State of Charge (0-1)
	SOC float64 `json:"soc"`

	// Current power consumption in Watts
	PowerConsumption float64 `json:"powerConsumption"`

	// Current power generation in Watts
	PowerGeneration float64 `json:"powerGeneration"`

	// Net current in Amperes (positive = charging)
	NetCurrent float64 `json:"netCurrent"`

	// Environmental heat inputs
	EnvironmentalHeat EnvironmentalHeat `json:"environmentalHeat"`

	// Timestamp
	Timestamp time.Time `json:"timestamp"`
}

// NewSatellitePhysicalState creates a new physical state with default values
func NewSatellitePhysicalState(nodeName string) *SatellitePhysicalState {
	return &SatellitePhysicalState{
		NodeName:            nodeName,
		Temperature:         293.15, // 20°C in Kelvin
		SOC:                 0.8,    // 80% initial charge
		PowerConsumption:    0,
		PowerGeneration:     0,
		NetCurrent:          0,
		EnvironmentalHeat:   EnvironmentalHeat{},
		Timestamp:          time.Now(),
	}
}

// =============================================================================
// Orbit Types for Thermal Calculation
// =============================================================================

// OrbitalPosition represents a satellite's position in orbit
type OrbitalPosition struct {
	// Position in ECI coordinates
	Position Vector `json:"position"`

	// Velocity vector
	Velocity Vector `json:"velocity"`

	// True anomaly in radians
	TrueAnomaly float64 `json:"trueAnomaly"`

	// Beta angle in degrees
	BetaAngle float64 `json:"betaAngle"`

	// Eclipse flag
	InEclipse bool `json:"inEclipse"`

	// Eclipse depth (0-1, 1 = full umbra)
	EclipseDepth float64 `json:"eclipseDepth"`
}

// =============================================================================
// Constants
// =============================================================================

const (
	// Stefan-Boltzmann constant in W/(m²·K⁴)
	StefanBoltzmannConstant = 5.670374419e-8

	// Solar constant at Earth in W/m²
	SolarConstant = 1361.0

	// Earth albedo factor
	EarthAlbedoFactor = 0.3

	// Earth effective temperature in K
	EarthTemperature = 255.0 // K (effective blackbody temperature)

	// Speed of light in m/s
	SpeedOfLight = 299792458.0
)