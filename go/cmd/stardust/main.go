package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/polaris-slo-cloud/stardust-go/configs"
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

	var simService types.SimulationController
	if *simulationStateInputFile != "" {
		simService = startSimulationIteration(*simulationConfig, *computingConfig, *routerConfig, *simulationStateInputFile, simulationPluginList)
	} else {
		simService = startSimulation(*simulationConfig, *islConfigString, *groundLinkConfigString, *computingConfig, *routerConfig, simulationStateOutputFile, simulationPluginList, statePluginList)
	}

	myCode(simService, *simulationConfig)
}

func startSimulationIteration(simulationConfig configs.SimulationConfig, computingConfig []configs.ComputingConfig, routerConfig configs.RouterConfig, simulationStateInputFile string, simulationPluginList []string) types.SimulationController {
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

	// Step 5: State Plugin Builder
	statePluginBuilder := stateplugin.NewStatePluginPrecompBuilder(simulationStateInputFile)

	// Step 6: Inject orchestrator (if used)
	orchestrator := deployment.NewDeploymentOrchestrator()

	simStateDeserializer := simulation.NewSimulationStateDeserializer(&simulationConfig, simulationStateInputFile, computingBuilder, routerBuilder, orchestrator, simPlugins, statePluginBuilder)
	return simStateDeserializer.LoadIterator()
}

func startSimulation(simulationConfig configs.SimulationConfig, islConfigString string, groundLinkConfigString string, computingConfig []configs.ComputingConfig, routerConfig configs.RouterConfig, simulationStateOutputFile *string, simulationPluginList []string, statePluginList []string) types.SimulationController {
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

	// Step 5: Initialize simulation service
	simService := simulation.NewSimulationService(&simulationConfig, routerBuilder, computingBuilder, simPlugins, types.NewStatePluginRepository(statePlugins), simulationStateOutputFile)

	// Step 6: Inject orchestrator (if used)
	orchestrator := deployment.NewDeploymentOrchestrator()
	simService.Inject(orchestrator)

	// Step 8: Load satellites using the loader service
	loaderService := satellite.NewSatelliteLoaderService(*islConfig, satBuilder, constellationLoader, simService, fmt.Sprintf("./resources/%s/%s", simulationConfig.SatelliteDataSourceType, simulationConfig.SatelliteDataSource), simulationConfig.SatelliteDataSourceType)
	if err := loaderService.Start(); err != nil {
		log.Fatalf("Failed to load satellites: %v", err)
	}

	// Step 9: Load ground stations using the ground station loader service
	groundLoaderService := ground.NewGroundStationLoaderService(simService, groundStationBuilder, ymlLoader, fmt.Sprintf("./resources/%s/%s", simulationConfig.GroundStationDataSourceType, simulationConfig.GroundStationDataSource), simulationConfig.GroundStationDataSourceType)
	if err := groundLoaderService.Start(); err != nil {
		log.Fatalf("Failed to load ground stations: %v", err)
	}

	return simService
}

func myCode(simulationController types.SimulationController, simulationConfig configs.SimulationConfig) {
	defer simulationController.Close()

	// Start the simulation loop or run individual code
	if simulationConfig.StepInterval >= 0 {
		done := simulationController.StartAutorun()
		<-done // blocks main goroutine until simulation stops
	} else {
		log.Println("Simulation loaded. Not autorunning as StepInterval < 0.")

		// Read the simulatin step time
		stepSeconds := float64(simulationConfig.StepMultiplier)

		for range simulationConfig.StepCount {
			// Advance simulation time by 10 minutes for demonstration
			simulationController.StepBySeconds(stepSeconds)

			var grounds = simulationController.GetGroundStations()
			var sats = simulationController.GetSatellites()

			// Safety check to ensure we have enough ground stations for the test scenario
			if len(grounds) <= 80 {
				log.Println("Error: Not enough ground stations for this test.")
				return
			}

			var ground1 = grounds[0]
			var ground2 = grounds[80]

			// --- Network (Defensive Programming) ---
			g1Links := ground1.GetLinkNodeProtocol().Established()
			g2Links := ground2.GetLinkNodeProtocol().Established()

			log.Printf("\n=======================================================")
			log.Printf(" SIMULATION STEP (Advanced by %.0f minutes)", stepSeconds/60)
			log.Printf(" Current Simulation Time: %v", simulationController.GetSimulationTime())
			log.Printf(" Network state: %d Satellites | %d Ground Stations", len(sats), len(grounds))
			log.Printf("=======================================================")

			if len(g1Links) == 0 || len(g2Links) == 0 {
				log.Printf("[!] Network Partition: One or both ground stations have NO active uplink.")
				log.Printf("    %s links: %d | %s links: %d\n\n",
					ground1.GetName(), len(g1Links), ground2.GetName(), len(g2Links))
				continue // Ugrás a következő szimulációs lépésre
			}

			// Accessing the first active link for each ground station (for demonstration)
			var l1 = g1Links[0]
			var l2 = g2Links[0]
			var uplinkSat1 = l1.GetOther(ground1)
			var uplinkSat2 = l2.GetOther(ground2)

			// Calculate route
			var route, err = ground1.GetRouter().RouteToNode(ground2, nil)
			var interSatelliteRoute, _ = uplinkSat1.GetRouter().RouteToNode(uplinkSat2, nil)

			// --- Connection Status ---
			if err != nil || !route.Reachable() {
				log.Printf("[X] CONNECTION OFFLINE: %s -> %s", ground1.GetName(), ground2.GetName())
				log.Printf("    Reason: No route found through the constellation.")
			} else {
				log.Printf("[OK] CONNECTION ONLINE: %s -> %s", ground1.GetName(), ground2.GetName())
				log.Printf("     Path structure: [Ground] -> (Uplink Sat) ... (Downlink Sat) <- [Ground]")
				log.Printf("     Used nodes:     [%s] -> (%s) ... (%s) <- [%s]",
					ground1.GetName(), uplinkSat1.GetName(), uplinkSat2.GetName(), ground2.GetName())

				log.Printf("\n  --- Latency Breakdown ---")
				log.Printf("  • Uplink (%s):     %6.2f ms", uplinkSat1.GetName(), l1.Latency())
				log.Printf("  • Space Routing:         %6d ms", interSatelliteRoute.Latency())
				log.Printf("  • Downlink (%s):   %6.2f ms", uplinkSat2.GetName(), l2.Latency())
				log.Printf("  ---------------------------------")
				log.Printf("  TOTAL End-to-End Delay:  %6d ms", route.Latency())

				log.Printf("\n  --- Physical Distances ---")
				log.Printf("  • G1 to Sat1:            %6.2f km", l1.Distance()/1000)
				log.Printf("  • Sat1 to Sat2 (Direct): %6.2f km", uplinkSat1.DistanceTo(uplinkSat2)/1000)
				log.Printf("  • Sat2 to G2:            %6.2f km", l2.Distance()/1000)
			}

			// --- Environmental Info ---
			var statePlugin = types.GetStatePlugin[stateplugin.SunStatePlugin](simulationController.GetStatePluginRepository())
			if statePlugin != nil {
				sunExp1 := statePlugin.GetSunlightExposure(uplinkSat1)
				sunExp2 := statePlugin.GetSunlightExposure(uplinkSat2)
				log.Printf("\n  --- Environmental Info ---")
				log.Printf("  • %s Sunlight Exposure: %.2f%%", uplinkSat1.GetName(), sunExp1*100)
				log.Printf("  • %s Sunlight Exposure: %.2f%%", uplinkSat2.GetName(), sunExp2*100)
			}

			log.Printf("=======================================================\n\n")
		}
	}
}
