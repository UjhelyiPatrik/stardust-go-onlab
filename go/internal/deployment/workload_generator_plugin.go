package deployment

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.SimulationPlugin = (*WorkloadGeneratorPlugin)(nil)

// WorkloadGeneratorPlugin creates new tasks based on config and assigns them to random ground stations.
type WorkloadGeneratorPlugin struct {
	config *configs.WorkloadConfig
	rng    *rand.Rand
}

// NewWorkloadGeneratorPlugin initializes the plugin.
func NewWorkloadGeneratorPlugin(config *configs.WorkloadConfig) *WorkloadGeneratorPlugin {
	return &WorkloadGeneratorPlugin{
		config: config,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (p *WorkloadGeneratorPlugin) Name() string {
	return "WorkloadGeneratorPlugin"
}

// PostSimulationStep executes the workload generation logic in each tick.
func (p *WorkloadGeneratorPlugin) PostSimulationStep(sim types.SimulationController) error {
	gss := sim.GetGroundStations()
	if len(gss) == 0 {
		return nil // No ground stations available to receive tasks
	}

	// 1. Determine how many tasks to generate this tick
	taskCount := p.config.MinTasksPerTick
	if p.config.MaxTasksPerTick > p.config.MinTasksPerTick {
		taskCount += p.rng.Intn(p.config.MaxTasksPerTick - p.config.MinTasksPerTick + 1)
	}

	for i := 0; i < taskCount; i++ {
		// 2. Generate random attributes within configured bounds
		megaCycles := p.config.MinMegaCycles + p.rng.Uint64()%(p.config.MaxMegaCycles-p.config.MinMegaCycles+1)
		memory := p.config.MinMemory + p.rng.Float64()*(p.config.MaxMemory-p.config.MinMemory)

		var sizeBytes uint64 = p.config.MinSizeBytes
		if p.config.MaxSizeBytes > p.config.MinSizeBytes {
			rangeBytes := int64(p.config.MaxSizeBytes - p.config.MinSizeBytes)
			sizeBytes += uint64(p.rng.Int63n(rangeBytes))
		}

		taskName := fmt.Sprintf("Task-%d-%d", sim.GetSimulationTime().Unix(), i)

		// 3. Create the deployable service (which now acts as a types.Payload)
		task, err := NewDeployableService(taskName, megaCycles, memory, sizeBytes)
		if err != nil {
			continue // Skip on invalid parameters
		}

		// 4. Select a random Ground Station and enqueue the task
		randomGS := gss[p.rng.Intn(len(gss))]
		randomGS.EnqueueTask(task)
	}

	return nil
}
