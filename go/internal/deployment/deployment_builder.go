package deployment

import (
	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// DeploymentBuilder constructs application-layer and orchestration plugins.
type DeploymentBuilder struct {
	workloadConfig *configs.WorkloadConfig
}

// NewDeploymentBuilder initializes a new builder for deployment plugins.
func NewDeploymentBuilder(workloadConfig *configs.WorkloadConfig) *DeploymentBuilder {
	return &DeploymentBuilder{
		workloadConfig: workloadConfig,
	}
}

// BuildPlugins instantiates all active deployment and workload plugins.
func (b *DeploymentBuilder) BuildPlugins() []types.SimulationPlugin {
	var plugins []types.SimulationPlugin

	// 1. Add the workload generator plugin if workload config is provided
	if b.workloadConfig != nil {
		plugins = append(plugins, NewWorkloadGeneratorPlugin(b.workloadConfig))
	}

	return plugins
}
