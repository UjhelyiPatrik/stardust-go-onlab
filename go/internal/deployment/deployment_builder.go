package deployment

import (
	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// DeploymentBuilder constructs application-layer and orchestration plugins.
type DeploymentBuilder struct {
	workloadConfig *configs.WorkloadConfig
	strategyName   string
}

// NewDeploymentBuilder initializes a new builder for deployment plugins.
func NewDeploymentBuilder(workloadConfig *configs.WorkloadConfig, strategyName string) *DeploymentBuilder {
	return &DeploymentBuilder{
		workloadConfig: workloadConfig,
		strategyName:   strategyName,
	}
}

// BuildPlugins instantiates all active deployment and workload plugins based on the provided configuration and physical plugins.
func (b *DeploymentBuilder) BuildPlugins(physicalPlugins []types.SimulationPlugin) []types.SimulationPlugin {
	var plugins []types.SimulationPlugin

	// 1. Add the workload generator plugin if workload config is provided
	if b.workloadConfig != nil {
		plugins = append(plugins, NewWorkloadGeneratorPlugin(b.workloadConfig))
	}

	// 2. Add the ground station orchestrator plugin with the selected strategy
	plugins = append(plugins, NewGroundStationOrchestratorPlugin(b.strategyName, physicalPlugins))

	return plugins
}
