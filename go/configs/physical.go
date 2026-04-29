package configs

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// =============================================================================
// Physical Configuration
// =============================================================================

// PhysicalConfig holds all physical configuration for satellites
type PhysicalConfig struct {
	Thermal    ThermalConfig    `yaml:"thermal"`
	Battery    BatteryConfig    `yaml:"battery"`
	Power      PowerConfig      `yaml:"power"`
	Constants  ConstantsConfig  `yaml:"constants"`
	Simulation SimulationConfig `yaml:"simulation"`
}

// ThermalConfig holds thermal configuration for different satellite types
type ThermalConfig map[string]ThermalPropertiesConfig

// ThermalPropertiesConfig holds the thermal properties for a satellite type
type ThermalPropertiesConfig struct {
	ThermalMass    float64 `yaml:"thermalMass"`
	SurfaceArea    float64 `yaml:"surfaceArea"`
	Absorptivity   float64 `yaml:"absorptivity"`
	Emissivity     float64 `yaml:"emissivity"`
	MaxTemperature float64 `yaml:"maxTemperature"`
	MinTemperature float64 `yaml:"minTemperature"`
}

// BatteryConfig holds battery configuration for different satellite types
type BatteryConfig map[string]BatteryPropertiesConfig

// BatteryPropertiesConfig holds the battery properties for a satellite type
type BatteryPropertiesConfig struct {
	Capacity            float64 `yaml:"capacity"`
	NominalVoltage      float64 `yaml:"nominalVoltage"`
	CoulombEfficiency   float64 `yaml:"coulombEfficiency"`
	MaxDoD              float64 `yaml:"maxDoD"`
	CriticalSOC         float64 `yaml:"criticalSOC"`
	InternalResistance float64 `yaml:"internalResistance"`
	MaxVoltage          float64 `yaml:"maxVoltage"`
	MinVoltage          float64 `yaml:"minVoltage"`
}

// PowerConfig holds power configuration for different satellite types
type PowerConfig map[string]PowerPropertiesConfig

// PowerPropertiesConfig holds the power properties for a satellite type
type PowerPropertiesConfig struct {
	SolarEfficiency      float64 `yaml:"solarEfficiency"`
	SolarPanelArea       float64 `yaml:"solarPanelArea"`
	MaxPowerGeneration   float64 `yaml:"maxPowerGeneration"`
	IdlePowerConsumption float64 `yaml:"idlePowerConsumption"`
}

// ConstantsConfig holds physical constants
type ConstantsConfig struct {
	StefanBoltzmann float64 `yaml:"stefanBoltzmann"`
	SolarConstant   float64 `yaml:"solarConstant"`
	EarthAlbedo     float64 `yaml:"earthAlbedo"`
	EarthTemperature float64 `yaml:"earthTemperature"`
	EarthRadius     float64 `yaml:"earthRadius"`
	SpeedOfLight    float64 `yaml:"speedOfLight"`
}

// SimulationConfig holds simulation settings
type SimulationConfig struct {
	TimeStep                   float64 `yaml:"timeStep"`
	EnableCyberPhysicalFeedback bool   `yaml:"enableCyberPhysicalFeedback"`
	ThermalWarningThreshold    float64 `yaml:"thermalWarningThreshold"`
	BatteryCriticalThreshold   float64 `yaml:"batteryCriticalThreshold"`
}

// LoadPhysicalConfig loads the physical configuration from a YAML file
func LoadPhysicalConfig(filename string) (*PhysicalConfig, error) {
	// Try default location if file doesn't exist
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		defaultPath := "resources/configs/physicalConfig.yaml"
		if _, err := os.Stat(defaultPath); err == nil {
			filename = defaultPath
		}
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read physical config file: %w", err)
	}

	var config PhysicalConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse physical config: %w", err)
	}

	// Set defaults for missing values
	config.setDefaults()

	return &config, nil
}

// setDefaults sets default values for missing configuration
func (c *PhysicalConfig) setDefaults() {
	// Set thermal defaults
	if c.Thermal == nil {
		c.Thermal = make(ThermalConfig)
	}
	if _, ok := c.Thermal["default"]; !ok {
		c.Thermal["default"] = ThermalPropertiesConfig{
			ThermalMass:    3500.0,
			SurfaceArea:    0.14,
			Absorptivity:   0.92,
			Emissivity:     0.85,
			MaxTemperature: 333.15,
			MinTemperature: 253.15,
		}
	}

	// Set battery defaults
	if c.Battery == nil {
		c.Battery = make(BatteryConfig)
	}
	if _, ok := c.Battery["default"]; !ok {
		c.Battery["default"] = BatteryPropertiesConfig{
			Capacity:            10.0,
			NominalVoltage:     7.4,
			CoulombEfficiency:  0.95,
			MaxDoD:             0.8,
			CriticalSOC:        0.2,
			InternalResistance: 0.1,
			MaxVoltage:         8.4,
			MinVoltage:         6.0,
		}
	}

	// Set power defaults
	if c.Power == nil {
		c.Power = make(PowerConfig)
	}
	if _, ok := c.Power["default"]; !ok {
		c.Power["default"] = PowerPropertiesConfig{
			SolarEfficiency:      0.28,
			SolarPanelArea:       0.08,
			MaxPowerGeneration:   40.0,
			IdlePowerConsumption: 2.0,
		}
	}

	// Set constants defaults
	if c.Constants.StefanBoltzmann == 0 {
		c.Constants.StefanBoltzmann = 5.670374419e-8
	}
	if c.Constants.SolarConstant == 0 {
		c.Constants.SolarConstant = 1361.0
	}
	if c.Constants.EarthAlbedo == 0 {
		c.Constants.EarthAlbedo = 0.3
	}
	if c.Constants.EarthTemperature == 0 {
		c.Constants.EarthTemperature = 255.0
	}
	if c.Constants.EarthRadius == 0 {
		c.Constants.EarthRadius = 6371000.0
	}
	if c.Constants.SpeedOfLight == 0 {
		c.Constants.SpeedOfLight = 299792458.0
	}

	// Set simulation defaults
	if c.Simulation.TimeStep == 0 {
		c.Simulation.TimeStep = 1.0
	}
}

// GetThermalProperties returns thermal properties for a satellite type
func (c *PhysicalConfig) GetThermalProperties(satelliteType string) ThermalPropertiesConfig {
	if props, ok := c.Thermal[satelliteType]; ok {
		return props
	}
	return c.Thermal["default"]
}

// GetBatteryProperties returns battery properties for a satellite type
func (c *PhysicalConfig) GetBatteryProperties(satelliteType string) BatteryPropertiesConfig {
	if props, ok := c.Battery[satelliteType]; ok {
		return props
	}
	return c.Battery["default"]
}

// GetPowerProperties returns power properties for a satellite type
func (c *PhysicalConfig) GetPowerProperties(satelliteType string) PowerPropertiesConfig {
	if props, ok := c.Power[satelliteType]; ok {
		return props
	}
	return c.Power["default"]
}