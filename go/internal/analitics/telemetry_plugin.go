package analytics

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"

	"github.com/polaris-slo-cloud/stardust-go/internal/network"
	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.SimulationPlugin = (*TelemetryExporterPlugin)(nil)

// TelemetryExporterPlugin gathers raw metrics every tick and writes them to a CSV file.
type TelemetryExporterPlugin struct {
	strategyName  string
	nodeFile      *os.File
	nodeWriter    *csv.Writer
	networkFile   *os.File
	networkWriter *csv.Writer
	thermalPlugin *simplugin.ThermalSimPlugin
	batteryPlugin *simplugin.BatterySimPlugin

	previousTraffic uint64 // Tracks cumulative traffic from the previous tick
}

// NewTelemetryExporterPlugin creates a new exporter that writes to the specified directory.
func NewTelemetryExporterPlugin(strategyName string, outDir string, physicalPlugins []types.SimulationPlugin) *TelemetryExporterPlugin { // Ensure the output directory exists
	if err := os.MkdirAll(outDir, os.ModePerm); err != nil {
		log.Fatalf("Failed to create analytics directory: %v", err)
	}

	// 1. NODE TELEMETRY FILE
	nodeFileName := fmt.Sprintf("telemetry_nodes_%s.csv", strategyName)
	nodeFile, err := os.Create(filepath.Join(outDir, nodeFileName))
	if err != nil {
		log.Fatalf("Failed to create node telemetry file: %v", err)
	}
	nodeWriter := csv.NewWriter(nodeFile)
	nodeWriter.Write([]string{
		"Time", "Strategy", "NodeName", "SunlightState", "Temperature_C", "BatterySoC_Percent", "CpuUtilization_Percent", "ActiveTasks", "NetEnergyChange_W",
	})
	nodeWriter.Flush()

	// 2. NETWORK TELEMETRY FILE
	netFileName := fmt.Sprintf("telemetry_network_%s.csv", strategyName)
	netFile, err := os.Create(filepath.Join(outDir, netFileName))
	if err != nil {
		log.Fatalf("Failed to create network telemetry file: %v", err)
	}
	netWriter := csv.NewWriter(netFile)
	netWriter.Write([]string{
		"Time", "Strategy", "StepTraffic_Bytes", "CumulativeTraffic_Bytes",
	})
	netWriter.Flush()

	plugin := &TelemetryExporterPlugin{
		strategyName:  strategyName,
		nodeFile:      nodeFile,
		nodeWriter:    nodeWriter,
		networkFile:   netFile,
		networkWriter: netWriter,
	}

	// Resolve the physical plugins to read their internal states
	for _, p := range physicalPlugins {
		if tp, ok := p.(*simplugin.ThermalSimPlugin); ok {
			plugin.thermalPlugin = tp
		}
		if bp, ok := p.(*simplugin.BatterySimPlugin); ok {
			plugin.batteryPlugin = bp
		}
	}

	return plugin
}

func (p *TelemetryExporterPlugin) Name() string {
	return "TelemetryExporterPlugin"
}

// PostSimulationStep extracts metrics from all satellites and writes them to the CSV.
func (p *TelemetryExporterPlugin) PostSimulationStep(sim types.SimulationController) error {
	simTime := sim.GetSimulationTime().Format("2006-01-02T15:04:05Z")
	sats := sim.GetSatellites()

	// Retrieve SunStatePlugin dynamically
	var sunPlugin stateplugin.SunStatePlugin
	repo := sim.GetStatePluginRepository()
	if repo != nil {
		for _, sp := range repo.GetAllPlugins() {
			if envPlugin, ok := sp.(stateplugin.SunStatePlugin); ok {
				sunPlugin = envPlugin
				break
			}
		}
	}

	for _, sat := range sats {
		comp := sat.GetComputing()
		if comp == nil {
			continue
		}

		// Extract metrics

		temp := -300.0 // Default fallback for unknown temperature
		if p.thermalPlugin != nil {
			if t, err := p.thermalPlugin.GetTemperatureCelsius(sat); err == nil {
				temp = t
			}
		}

		soc := 200.0 // Default fallback
		netEnergy := 0.0
		if p.batteryPlugin != nil {
			// Replace GetSoC with your actual getter from battery_sim_plugin.go
			if s, err := p.batteryPlugin.GetSOC(sat); err == nil {
				soc = s * 100 // Convert to percentage
			}

			// Replace GetNetEnergyChange with your actual getter from battery_sim_plugin.go
			if n, err := p.batteryPlugin.GetNetEnergyChange(sat); err == nil {
				netEnergy = n
			}
		}

		// 3. Sunlight State (Eclipse check)
		sunlightState := "Sunlight"
		if sunPlugin != nil {
			// A környezeti plugin alapján (ha eklipszisben van)
			if sunPlugin.GetSunlightExposure(sat) < 0.1 { // Threshold for eclipse
				sunlightState = "Eclipse"
			}
		}

		// 4. CPU Utilization (percentage)
		cpuPercent := comp.GetCpuUtilization()
		activeTasks := len(comp.GetServices())

		// Write the row
		p.nodeWriter.Write([]string{
			simTime,
			p.strategyName,
			sat.GetName(),
			sunlightState,
			strconv.FormatFloat(temp, 'f', 2, 64),
			strconv.FormatFloat(soc, 'f', 2, 64),
			strconv.FormatFloat(cpuPercent, 'f', 2, 64),
			strconv.Itoa(activeTasks),
			strconv.FormatFloat(netEnergy, 'f', 2, 64),
		})
	}

	totalCumulativeTraffic := atomic.LoadUint64(&network.TotalTransmittedBytes)

	stepTraffic := totalCumulativeTraffic - p.previousTraffic
	p.previousTraffic = totalCumulativeTraffic

	// Sor kiírása a Hálózat CSV-be
	p.networkWriter.Write([]string{
		simTime,
		p.strategyName,
		strconv.FormatUint(stepTraffic, 10),
		strconv.FormatUint(totalCumulativeTraffic, 10),
	})

	// Kiürítés a lemezre, hogy élőben (valós időben) lehessen követni Pythonból
	p.nodeWriter.Flush()
	p.networkWriter.Flush()

	return nil
}

// Close should be called at the end of the simulation to release file handles.
func (p *TelemetryExporterPlugin) Close() {
	if p.nodeWriter != nil {
		p.nodeWriter.Flush()
		p.nodeFile.Close()
	}
	if p.networkWriter != nil {
		p.networkWriter.Flush()
		p.networkFile.Close()
	}
}
