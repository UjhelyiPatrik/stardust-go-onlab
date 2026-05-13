package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/polaris-slo-cloud/stardust-go/configs"
	analytics "github.com/polaris-slo-cloud/stardust-go/internal/analitics"
	"github.com/polaris-slo-cloud/stardust-go/internal/computing"
	"github.com/polaris-slo-cloud/stardust-go/internal/deployment"
	"github.com/polaris-slo-cloud/stardust-go/internal/ground"
	"github.com/polaris-slo-cloud/stardust-go/internal/routing"
	"github.com/polaris-slo-cloud/stardust-go/internal/satellite"
	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/internal/simulation"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

func main() {
	simulationConfigString := flag.String(
		"simulationConfig",
		"./resources/configs/simulationAutorunConfig.yaml",
		"Path to the simulation config file",
	)
	islConfigString := flag.String(
		"islConfig",
		"./resources/configs/islMstConfig.yaml",
		"Path to inter satellite link config file",
	)
	groundLinkConfigString := flag.String(
		"groundLinkConfig",
		"./resources/configs/groundLinkNearestConfig.yaml",
		"Path to ground link config file",
	)
	computingConfigString := flag.String(
		"computingConfig",
		"./resources/configs/computingConfig.yaml",
		"Path to computing config file",
	)
	routerConfigString := flag.String(
		"routerConfig",
		"./resources/configs/routerAStarConfig.yaml",
		"Path to router config file",
	)
	simulationStateOutputFile := flag.String(
		"simulationStateOutputFile",
		"./simulation_state_output.gob",
		"Path to output the simulation state (optional)",
	)
	simulationStateInputFile := flag.String(
		"simulationStateInputFile",
		"",
		"Path to input the simulation state (optional)",
	)
	simulationPluginString := flag.String(
		"simulationPlugins",
		"",
		"Plugin names (optional, comma-separated list)",
	)
	statePluginString := flag.String(
		"statePlugins",
		"",
		"Plugin names (optional, comma-separated list)",
	)
	orchestrationStrategyString := flag.String(
		"orchestrator",
		"sunlight", // Default strategy
		"Task orchestration strategy: sunlight, coldest, balanced",
	)
	workloadConfigString := flag.String(
		"workloadConfig",
		"./resources/configs/workloadConfig.yaml",
		"Path to workload config file",
	)
	flag.Parse()

	simulationPluginList := strings.Split(*simulationPluginString, ",")
	if *simulationPluginString == "" {
		simulationPluginList = []string{}
	}

	statePluginList := strings.Split(*statePluginString, ",")
	if *statePluginString == "" {
		statePluginList = []string{}
	}

	// Step 1: Load configuration
	simulationConfig, err := configs.LoadConfigFromFile[configs.SimulationConfig](*simulationConfigString)
	if err != nil {
		log.Fatalf("Failed to load simulation configuration: %v", err)
	}

	computingConfig, err := configs.LoadConfigFromFile[[]configs.ComputingConfig](*computingConfigString)
	if err != nil {
		log.Fatalf("Failed to load simulation configuration: %v", err)
	}

	routerConfig, err := configs.LoadConfigFromFile[configs.RouterConfig](*routerConfigString)
	if err != nil {
		log.Fatalf("Failed to load simulation configuration: %v", err)
	}

	workloadConfig, err := configs.LoadConfigFromFile[configs.WorkloadConfig](*workloadConfigString)
	if err != nil {
		log.Fatalf("Failed to load workload configuration: %v", err)
	}

	var simService types.SimulationController
	if *simulationStateInputFile != "" {
		simService = startSimulationIteration(*simulationConfig, *computingConfig, *routerConfig, *simulationStateInputFile, simulationPluginList, workloadConfig, *orchestrationStrategyString)
	} else {
		simService = startSimulation(*simulationConfig, *islConfigString, *groundLinkConfigString, *computingConfig, *routerConfig, simulationStateOutputFile, simulationPluginList, statePluginList, *orchestrationStrategyString, workloadConfig)
	}

	defer simService.Close()

	if simulationConfig.StepInterval >= 0 {
		log.Printf("Starting autorun simulation with orchestrator strategy: %s", *orchestrationStrategyString)
		done := simService.StartAutorun()
		<-done // blocks main goroutine until simulation stops
	} else {
		log.Printf("Simulation loaded (Manual mode). Orchestrator strategy: %s", *orchestrationStrategyString)
		stepSeconds := float64(simulationConfig.StepMultiplier)
		for range simulationConfig.StepCount {
			simService.StepBySeconds(stepSeconds)
		}
	}
}

func startSimulationIteration(simulationConfig configs.SimulationConfig, computingConfig []configs.ComputingConfig, routerConfig configs.RouterConfig, simulationStateInputFile string, simulationPluginList []string, workloadConfig *configs.WorkloadConfig, orchestrationStrategyString string) types.SimulationController {
	// Step 2: Build computing builder with configured strategies
	var computingBuilder computing.ComputingBuilder = computing.NewComputingBuilder(computingConfig)

	// Step 3: Build router builder
	routerBuilder := routing.NewRouterBuilder(routerConfig)

	// Step 4.1: Initialize plugin builder
	simPluginBuilder := simplugin.NewPluginBuilder()
	simPlugins, err := simPluginBuilder.BuildPlugins(simulationPluginList)
	if err != nil {
		log.Fatalf("Failed to build simualtion plugins: %v", err)
		return nil
	}

	// Step 4.2: Initialize deployment plugin builder
	deploymentBuilder := deployment.NewDeploymentBuilder(workloadConfig, orchestrationStrategyString)
	deploymentPlugins := deploymentBuilder.BuildPlugins(simPlugins)

	// Add the deployment plugins to the simulation plugins list
	simPlugins = append(simPlugins, deploymentPlugins...)

	// Step 5: State Plugin Builder
	statePluginBuilder := stateplugin.NewStatePluginPrecompBuilder(simulationStateInputFile)

	simStateDeserializer := simulation.NewSimulationStateDeserializer(&simulationConfig, simulationStateInputFile, computingBuilder, routerBuilder, simPlugins, statePluginBuilder)
	return simStateDeserializer.LoadIterator()
}

func startSimulation(simulationConfig configs.SimulationConfig, islConfigString string, groundLinkConfigString string, computingConfig []configs.ComputingConfig, routerConfig configs.RouterConfig, simulationStateOutputFile *string, simulationPluginList []string, statePluginList []string, orchestrationStrategyString string, workloadConfig *configs.WorkloadConfig) types.SimulationController {
	islConfig, err := configs.LoadConfigFromFile[configs.InterSatelliteLinkConfig](islConfigString)
	if err != nil {
		log.Fatalf("Failed to load isl configuration: %v", err)
	}

	groundLinkConfig, err := configs.LoadConfigFromFile[configs.GroundLinkConfig](groundLinkConfigString)
	if err != nil {
		log.Fatalf("Failed to load isl configuration: %v", err)
	}

	// Step 2: Build computing builder with configured strategies
	computingBuilder := computing.NewComputingBuilder(computingConfig)

	// Step 3: Build router builder
	routerBuilder := routing.NewRouterBuilder(routerConfig)

	// Step 4.1: Initialize plugin builder
	simPluginBuilder := simplugin.NewPluginBuilder()
	simPlugins, err := simPluginBuilder.BuildPlugins(simulationPluginList)
	if err != nil {
		log.Fatalf("Failed to build simualtion plugins: %v", err)
		return nil
	}

	// Step 4.2: Initialize deployment plugin builder
	deploymentBuilder := deployment.NewDeploymentBuilder(workloadConfig, orchestrationStrategyString) // workloadConfig paraméter átadva
	deploymentPlugins := deploymentBuilder.BuildPlugins(simPlugins)

	// Add the deployment plugins to the simulation plugins list
	simPlugins = append(simPlugins, deploymentPlugins...)

	// Step 4.2: Initialize state plugin builder
	statePluginBuilder := stateplugin.NewStatePluginBuilder()
	statePlugins, err := statePluginBuilder.BuildPlugins(statePluginList)
	if err != nil {
		log.Fatalf("Failed to build state plugins: %v", err)
		return nil
	}

	// Step 5.1: Initialize the satellite builder
	satBuilder := satellite.NewSatelliteBuilder(routerBuilder, computingBuilder, *islConfig)
	tleLoader := satellite.NewTleLoader(*islConfig, satBuilder)

	// Step 4.2: Initialize the ground station loader
	groundStationBuilder := ground.NewGroundStationBuilder(simulationConfig.SimulationStartTime, routerBuilder, computingBuilder, *groundLinkConfig)
	ymlLoader := ground.NewGroundStationYmlLoader(*groundLinkConfig, groundStationBuilder)

	// Step 4.3: Initialize constellation loader and register TLE loader
	constellationLoader := satellite.NewSatelliteConstellationLoader()
	constellationLoader.RegisterDataSourceLoader("tle", tleLoader)

	// Step 6: Initialize telemetry plugin for analytics
	telemetryPlugin := analytics.NewTelemetryExporterPlugin(
		orchestrationStrategyString, // This should hold the strategy name e.g. "coldest", "dark"
		"./results/analytics",       // Output directory
		simPlugins,
	)
	simPlugins = append(simPlugins, telemetryPlugin)

	// Step 5: Initialize simulation service
	simService := simulation.NewSimulationService(&simulationConfig, routerBuilder, computingBuilder, simPlugins, types.NewStatePluginRepository(statePlugins), simulationStateOutputFile)

	// Step 6: Load satellites using the loader service
	loaderService := satellite.NewSatelliteLoaderService(*islConfig, satBuilder, constellationLoader, simService, fmt.Sprintf("./resources/%s/%s", simulationConfig.SatelliteDataSourceType, simulationConfig.SatelliteDataSource), simulationConfig.SatelliteDataSourceType)
	if err := loaderService.Start(); err != nil {
		log.Fatalf("Failed to load satellites: %v", err)
	}

	// Step 7: Load ground stations using the ground station loader service
	groundLoaderService := ground.NewGroundStationLoaderService(simService, groundStationBuilder, ymlLoader, fmt.Sprintf("./resources/%s/%s", simulationConfig.GroundStationDataSourceType, simulationConfig.GroundStationDataSource), simulationConfig.GroundStationDataSourceType)
	if err := groundLoaderService.Start(); err != nil {
		log.Fatalf("Failed to load ground stations: %v", err)
	}

	return simService
}
