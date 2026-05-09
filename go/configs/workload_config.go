package configs

// WorkloadConfig defines the intervals and limits for generating network traffic and computing tasks.
type WorkloadConfig struct {
	MinTasksPerTick int     `yaml:"minTasksPerTick"`
	MaxTasksPerTick int     `yaml:"maxTasksPerTick"`
	MinMegaCycles   uint64  `yaml:"minMegaCycles"`
	MaxMegaCycles   uint64  `yaml:"maxMegaCycles"`
	MinMemory       float64 `yaml:"minMemory"`
	MaxMemory       float64 `yaml:"maxMemory"`
	MinSizeBytes    uint64  `yaml:"minSizeBytes"`
	MaxSizeBytes    uint64  `yaml:"maxSizeBytes"`
}
