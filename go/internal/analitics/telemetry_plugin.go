package analytics

import (
	"encoding/csv"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/internal/metrics"
	"github.com/polaris-slo-cloud/stardust-go/internal/network"
	"github.com/polaris-slo-cloud/stardust-go/internal/simplugin"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/helper"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.SimulationPlugin = (*TelemetryExporterPlugin)(nil)

// ANSI color escape sequences for structured terminal UI
const (
	ansiReset       = "\033[0m"
	ansiBold        = "\033[1m"
	ansiDimGray     = "\033[90m"
	ansiWhite       = "\033[97m"
	ansiBoldWhite   = "\033[1;97m"
	ansiCyan        = "\033[36m"
	ansiBrightCyan  = "\033[96m"
	ansiBoldCyan    = "\033[1;96m"
	ansiGreen       = "\033[32m"
	ansiBrightGreen = "\033[92m"
	ansiBoldGreen   = "\033[1;92m"
	ansiYellow      = "\033[33m"
	ansiBrightYel   = "\033[93m"
	ansiBoldRed     = "\033[1;91m"
	ansiBoldMagenta = "\033[1;95m"
)

// Fixed visual column widths (excluding 1-space left/right cell padding)
var tableColWidths = []int{22, 24, 8, 13, 14, 13, 18, 18, 16}

type TelemetryBatch struct {
	NodeRows [][]string
	NetRow   []string
	CompRow  []string
}

type satStepMetric struct {
	name          string
	tempC         float64
	socPercent    float64
	powerConsW    float64
	powerGenW     float64
	cpuPercent    float64
	activeTasks   int
	inEclipse     bool
	isCritBattery bool
	isOverheating bool
	hasThermal    bool
	hasBattery    bool
}

// TelemetryExporterPlugin gathers raw metrics every tick in parallel across CPU cores,
// flushes them in batches to CSV files asynchronously, and outputs a clean tabular console UI.
type TelemetryExporterPlugin struct {
	strategyName    string
	nodeFile        *os.File
	nodeWriter      *csv.Writer
	networkFile     *os.File
	networkWriter   *csv.Writer
	compFile        *os.File
	compWriter      *csv.Writer
	thermalPlugin   *simplugin.ThermalSimPlugin
	batteryPlugin   *simplugin.BatterySimPlugin
	batchChan       chan TelemetryBatch
	writerWg        sync.WaitGroup
	previousTraffic uint64

	// Console logging and all-time envelope tracking
	bannerPrinted      bool
	runWallStart       time.Time
	totalStepWallTime  time.Duration
	stepsExecuted      int
	maxStepCount       int
	isPrecomputed      bool
	simStartTime       time.Time
	lastSimTime        time.Time
	lastTaskMetrics    metrics.StepTaskMetrics
	lastActiveTasks    int
	lastActiveSats     int
	lastQueuedTasks    int
	lastAvgSOC         float64
	lastAvgTemp        float64
	totalCritBatSteps  int
	totalOverheatSteps int
	globalMinSOC       float64
	globalMinSOCNode   string
	globalMinSOCTime   time.Time
	globalMaxTemp      float64
	globalMaxTempNode  string
	globalMaxTempTime  time.Time
	globalMinTemp      float64
	globalMinTempNode  string
	globalMinTempTime  time.Time
	tempScratchBuffer  []float64
	satMetricsBuffer   []satStepMetric
}

// NewTelemetryExporterPlugin creates a new exporter that writes to the specified directory.
func NewTelemetryExporterPlugin(strategyName string, outDir string, physicalPlugins []types.SimulationPlugin) *TelemetryExporterPlugin {
	enableVirtualTerminal()

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
	_ = nodeWriter.Write([]string{
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
	_ = netWriter.Write([]string{
		"Time", "Strategy", "StepTraffic_Bytes", "CumulativeTraffic_Bytes",
	})
	netWriter.Flush()

	// 3. COMPUTING TELEMETRY FILE
	compFileName := fmt.Sprintf("telemetry_computing_%s.csv", strategyName)
	compFile, err := os.Create(filepath.Join(outDir, compFileName))
	if err != nil {
		log.Fatalf("Failed to create comp telemetry file: %v", err)
	}
	compWriter := csv.NewWriter(compFile)
	_ = compWriter.Write([]string{
		"Time", "Strategy", "TotalCompletedTasks", "TotalConsumedMegaCycles", "AvgTurnaroundTime_Ms",
	})
	compWriter.Flush()

	plugin := &TelemetryExporterPlugin{
		strategyName:  strategyName,
		nodeFile:      nodeFile,
		nodeWriter:    nodeWriter,
		networkFile:   netFile,
		networkWriter: netWriter,
		compFile:      compFile,
		compWriter:    compWriter,
		batchChan:     make(chan TelemetryBatch, 128),
		globalMinSOC:  100.0,
		globalMaxTemp: -1000.0,
		globalMinTemp: 1000.0,
	}

	for _, p := range physicalPlugins {
		if tp, ok := p.(*simplugin.ThermalSimPlugin); ok {
			plugin.thermalPlugin = tp
		}
		if bp, ok := p.(*simplugin.BatterySimPlugin); ok {
			plugin.batteryPlugin = bp
		}
	}

	plugin.writerWg.Add(1)
	go plugin.startWriter()

	return plugin
}

func (p *TelemetryExporterPlugin) Name() string {
	return "TelemetryExporterPlugin"
}

// PostSimulationStep extracts metrics from all satellites concurrently, dispatches a batch to the CSV writer,
// and prints a single aligned row to the console table.
func (p *TelemetryExporterPlugin) PostSimulationStep(sim types.SimulationController) error {
	currentSimTime := sim.GetSimulationTime()
	simTimeStr := currentSimTime.UTC().Format("2006-01-02T15:04:05Z")
	sats := sim.GetSatellites()
	gss := sim.GetGroundStations()

	if !p.bannerPrinted {
		p.bannerPrinted = true
		p.runWallStart = time.Now()
		p.simStartTime = sim.GetSimulationStartTime()
		p.maxStepCount = sim.GetMaxStepCount()
		p.isPrecomputed = sim.IsPrecomputed()
		p.printStartupBanner(sim, len(sats), len(gss))
		p.printTableHeader(true)
	} else if p.stepsExecuted > 0 && p.stepsExecuted%25 == 0 {
		// Repeat column headers every 25 steps so columns remain labeled while scrolling
		p.printTableHeader(false)
	}

	var sunPlugin stateplugin.SunStatePlugin
	var eclipseChecker interface{ IsEclipse(types.Node) bool }
	repo := sim.GetStatePluginRepository()
	if repo != nil {
		for _, sp := range repo.GetAllPlugins() {
			if envPlugin, ok := sp.(stateplugin.SunStatePlugin); ok {
				sunPlugin = envPlugin
				if ec, hasEc := sp.(interface{ IsEclipse(types.Node) bool }); hasEc {
					eclipseChecker = ec
				}
				break
			}
		}
	}

	nodeRows := make([][]string, len(sats))
	if len(p.satMetricsBuffer) != len(sats) {
		p.satMetricsBuffer = make([]satStepMetric, len(sats))
	}
	satMetrics := p.satMetricsBuffer

	indices := make([]int, len(sats))
	for i := range sats {
		indices[i] = i
	}

	helper.ParallelFor(indices, func(i int) {
		sat := sats[i]
		satName := sat.GetName()
		comp := sat.GetComputing()
		if comp == nil {
			return
		}

		temp := -300.0
		hasThermal := false
		isOverheating := false
		if p.thermalPlugin != nil {
			if t, err := p.thermalPlugin.GetTemperatureCelsius(sat); err == nil {
				temp = t
				hasThermal = true
				isOverheating = p.thermalPlugin.IsOverheating(sat)
			}
		}

		soc := 100.0
		netEnergy := 0.0
		powerCons := 0.0
		powerGen := 0.0
		hasBattery := false
		isCritBattery := false
		if p.batteryPlugin != nil {
			if st, err := p.batteryPlugin.GetBatteryState(sat); err == nil && st != nil {
				soc = st.SOC * 100.0
				powerCons = st.PowerConsumption
				powerGen = st.PowerGeneration
				netEnergy = powerGen - powerCons
				hasBattery = true
				isCritBattery = p.batteryPlugin.IsCritical(sat)
			}
		}

		inEclipse := false
		sunlightState := "Sunlight"
		if eclipseChecker != nil {
			if eclipseChecker.IsEclipse(sat) {
				inEclipse = true
				sunlightState = "Eclipse"
			}
		} else if sunPlugin != nil && sunPlugin.GetSunlightExposure(sat) == 0.0 {
			inEclipse = true
			sunlightState = "Eclipse"
		}

		cpuPercent := comp.GetCpuUtilization()
		activeTasks := comp.GetActiveServiceCount()

		satMetrics[i] = satStepMetric{
			name:          satName,
			tempC:         temp,
			socPercent:    soc,
			powerConsW:    powerCons,
			powerGenW:     powerGen,
			cpuPercent:    cpuPercent,
			activeTasks:   activeTasks,
			inEclipse:     inEclipse,
			isCritBattery: isCritBattery,
			isOverheating: isOverheating,
			hasThermal:    hasThermal,
			hasBattery:    hasBattery,
		}

		nodeRows[i] = []string{
			simTimeStr, p.strategyName, satName, sunlightState,
			strconv.FormatFloat(temp, 'f', 2, 64),
			strconv.FormatFloat(soc, 'f', 2, 64),
			strconv.FormatFloat(cpuPercent, 'f', 2, 64),
			strconv.Itoa(activeTasks),
			strconv.FormatFloat(netEnergy, 'f', 2, 64),
		}
	})

	totalCumulativeTraffic := atomic.LoadUint64(&network.TotalTransmittedBytes)
	stepTraffic := totalCumulativeTraffic - p.previousTraffic
	p.previousTraffic = totalCumulativeTraffic

	netRow := []string{
		simTimeStr, p.strategyName,
		strconv.FormatUint(stepTraffic, 10),
		strconv.FormatUint(totalCumulativeTraffic, 10),
	}

	taskStats := metrics.FlushStepTaskMetrics()

	compRow := []string{
		simTimeStr, p.strategyName,
		strconv.FormatUint(taskStats.TotalCompleted, 10),
		strconv.FormatUint(taskStats.TotalConsumedCycles/1000000, 10),
		strconv.FormatFloat(taskStats.TotalAvgTurnaroundMs, 'f', 2, 64),
	}

	p.batchChan <- TelemetryBatch{
		NodeRows: nodeRows,
		NetRow:   netRow,
		CompRow:  compRow,
	}

	p.logStepTableRow(sim, currentSimTime, satMetrics, gss, taskStats)

	return nil
}

func (p *TelemetryExporterPlugin) logStepTableRow(
	sim types.SimulationController,
	currentSimTime time.Time,
	satMetrics []satStepMetric,
	gss []types.GroundStation,
	taskStats metrics.StepTaskMetrics,
) {
	stepWallDur := sim.GetStepWallDuration()
	p.totalStepWallTime += stepWallDur
	p.stepsExecuted++
	p.lastSimTime = currentSimTime
	p.lastTaskMetrics = taskStats

	currentStep := sim.GetCurrentStep()
	if currentStep <= 0 {
		currentStep = p.stepsExecuted
	}
	maxSteps := sim.GetMaxStepCount()
	if maxSteps <= 0 {
		maxSteps = currentStep
	}
	p.maxStepCount = maxSteps

	progressPct := (float64(currentStep) / float64(maxSteps)) * 100.0
	if progressPct > 100.0 {
		progressPct = 100.0
	}

	remainingSteps := maxSteps - currentStep
	var etaDur time.Duration
	if remainingSteps > 0 && p.stepsExecuted > 0 {
		avgStepNs := int64(p.totalStepWallTime) / int64(p.stepsExecuted)
		etaDur = time.Duration(avgStepNs * int64(remainingSteps))
	}

	// Reduce satellite metrics
	minSOC, maxSOC, sumSOC := 100.0, 0.0, 0.0
	minSOCNode := "-"
	critBatteryCount := 0
	batCount := 0

	minTemp, maxTemp, sumTemp := math.MaxFloat64, -math.MaxFloat64, 0.0
	minTempNode, maxTempNode := "-", "-"
	overheatCount := 0
	p.tempScratchBuffer = p.tempScratchBuffer[:0]

	activeTasksTotal, activeSatsCount := 0, 0

	for i := range satMetrics {
		m := &satMetrics[i]
		if m.name == "" {
			continue
		}

		activeTasksTotal += m.activeTasks
		if m.activeTasks > 0 {
			activeSatsCount++
		}

		if m.hasBattery {
			batCount++
			sumSOC += m.socPercent
			if m.socPercent < minSOC || minSOCNode == "-" {
				minSOC = m.socPercent
				minSOCNode = m.name
			}
			if m.socPercent > maxSOC {
				maxSOC = m.socPercent
			}
			if m.isCritBattery {
				critBatteryCount++
			}
		}

		if m.hasThermal {
			p.tempScratchBuffer = append(p.tempScratchBuffer, m.tempC)
			sumTemp += m.tempC
			if m.tempC < minTemp || minTempNode == "-" {
				minTemp = m.tempC
				minTempNode = m.name
			}
			if m.tempC > maxTemp || maxTempNode == "-" {
				maxTemp = m.tempC
				maxTempNode = m.name
			}
			if m.isOverheating {
				overheatCount++
			}
		}
	}

	avgSOC := 100.0
	if batCount > 0 {
		avgSOC = sumSOC / float64(batCount)
		if minSOC < p.globalMinSOC || p.globalMinSOCNode == "" {
			p.globalMinSOC = minSOC
			p.globalMinSOCNode = minSOCNode
			p.globalMinSOCTime = currentSimTime
		}
	} else {
		minSOC = 100.0
	}

	avgTemp := 0.0
	if len(p.tempScratchBuffer) > 0 {
		avgTemp = sumTemp / float64(len(p.tempScratchBuffer))
		if maxTemp > p.globalMaxTemp || p.globalMaxTempNode == "" {
			p.globalMaxTemp = maxTemp
			p.globalMaxTempNode = maxTempNode
			p.globalMaxTempTime = currentSimTime
		}
		if minTemp < p.globalMinTemp || p.globalMinTempNode == "" {
			p.globalMinTemp = minTemp
			p.globalMinTempNode = minTempNode
			p.globalMinTempTime = currentSimTime
		}
	} else {
		minTemp = 0.0
		maxTemp = 0.0
	}

	queuedTasks := 0
	for _, gs := range gss {
		if qLenGetter, ok := gs.(interface{ GetTaskQueueLen() int }); ok {
			queuedTasks += qLenGetter.GetTaskQueueLen()
		} else {
			queuedTasks += len(gs.GetTaskQueue())
		}
	}

	p.lastActiveTasks = activeTasksTotal
	p.lastActiveSats = activeSatsCount
	p.lastQueuedTasks = queuedTasks
	p.lastAvgSOC = avgSOC
	p.lastAvgTemp = avgTemp
	p.totalCritBatSteps += critBatteryCount
	p.totalOverheatSteps += overheatCount

	// Format exact fixed-width cell strings BEFORE applying ANSI colors
	// Col 1 (22 chars): "2000/120  ██░░░   6.0%"
	ratioStr := fmt.Sprintf("%-9s", fmt.Sprintf("%d/%d", maxSteps, currentStep))
	if len(ratioStr) > 9 {
		ratioStr = ratioStr[:9]
	}
	pctStr := fmt.Sprintf("%5.1f%%", progressPct)
	col1 := colorize(ratioStr, ansiBoldWhite) + " " + renderProgressBar(progressPct, 5) + " " + colorize(pctStr, ansiBoldCyan)

	// Col 2 (24 chars): "10-01 05:25:00 (+00d05h)"
	elapsedSim := currentSimTime.Sub(p.simStartTime)
	dateStr := currentSimTime.UTC().Format("01-02 15:04:05")
	elapStr := fmt.Sprintf("(+%s)", formatShortElapsed(elapsedSim)) // 9 chars e.g. "(+00h05m)" or "(+01d05h)"
	col2 := colorize(dateStr, ansiWhite) + " " + colorize(fmt.Sprintf("%-9s", elapStr), ansiCyan)

	// Col 3 (8 chars): "coldest "
	stratStr := fmt.Sprintf("%-8s", p.strategyName)
	if len(stratStr) > 8 {
		stratStr = stratStr[:8]
	}
	col3 := colorize(stratStr, ansiBoldMagenta)

	// Col 4 (13 chars): " +389 /  +343"
	genStr := fmt.Sprintf("+%-4d", taskStats.StepGenerated)
	depStr := fmt.Sprintf("+%-4d", taskStats.StepDeployed)
	col4 := colorize(fmt.Sprintf("%5s", genStr), ansiWhite) + colorize(" / ", ansiDimGray) + colorize(fmt.Sprintf("%5s", depStr), ansiBrightCyan)

	// Col 5 (14 chars): " +344 ( 43.1k)"
	doneStr := fmt.Sprintf("+%-4d", taskStats.StepCompleted)
	totStr := fmt.Sprintf("(%6s)", formatCompactUint(taskStats.TotalCompleted))
	col5 := colorize(fmt.Sprintf("%5s", doneStr), ansiBoldGreen) + " " + colorize(totStr, ansiGreen)

	// Col 6 (13 chars): "  826 /  1409"
	runStr := fmt.Sprintf("%5d", activeTasksTotal)
	qStr := fmt.Sprintf("%5d", queuedTasks)
	qColor := ansiDimGray
	if queuedTasks > 0 {
		qColor = ansiBrightYel
	}
	col6 := colorize(runStr, ansiWhite) + colorize(" / ", ansiDimGray) + colorize(qStr, qColor)

	// Col 7 (18 chars): " 99.6% / min 93.2%"
	avgSocStr := fmt.Sprintf("%5.1f%%", avgSOC)
	minSocStr := fmt.Sprintf("min%5.1f%%", minSOC)
	minSocColor := ansiGreen
	if minSOC < 25.0 {
		minSocColor = ansiBoldRed
	} else if minSOC < 60.0 {
		minSocColor = ansiBrightYel
	}
	col7 := colorize(avgSocStr, ansiBrightGreen) + colorize(" / ", ansiDimGray) + colorize(minSocStr, minSocColor)

	// Col 8 (18 chars): " 57.6° / max 78.8°"
	avgTmpStr := fmt.Sprintf("%5.1f", avgTemp) + "°"
	maxTmpStr := "max" + fmt.Sprintf("%5.1f", maxTemp) + "°"
	maxTmpColor := ansiCyan
	if maxTemp >= 75.0 {
		maxTmpColor = ansiBoldRed
	} else if maxTemp >= 65.0 {
		maxTmpColor = ansiBrightYel
	}
	col8 := colorize(avgTmpStr, ansiWhite) + colorize(" / ", ansiDimGray) + colorize(maxTmpStr, maxTmpColor)

	// Col 9 (16 chars): " 744ms / 25m 36s"
	stepTimeStr := fmt.Sprintf("%6s", formatStepDuration(stepWallDur))
	etaTimeStr := fmt.Sprintf("%7s", formatWallDuration(etaDur))
	col9 := colorize(stepTimeStr, ansiWhite) + colorize(" / ", ansiDimGray) + colorize(etaTimeStr, ansiBrightYel)

	sep := colorize("│", ansiDimGray)
	fmt.Printf("%s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s %s\n",
		sep, col1,
		sep, col2,
		sep, col3,
		sep, col4,
		sep, col5,
		sep, col6,
		sep, col7,
		sep, col8,
		sep, col9,
		sep,
	)
}

func (p *TelemetryExporterPlugin) printTableHeader(isFirst bool) {
	if isFirst {
		fmt.Println(colorize(buildBorder("┌", "┬", "┐"), ansiDimGray))
	} else {
		fmt.Println(colorize(buildBorder("├", "┼", "┤"), ansiDimGray))
	}

	headers := []string{
		fmt.Sprintf("%-22s", "STEP (N/i) & PROGRESS"),
		fmt.Sprintf("%-24s", "SIMULATED TIME (UTC)"),
		fmt.Sprintf("%-8s", "STRATEGY"),
		fmt.Sprintf("%-13s", "+NEW  / +DEP"),
		fmt.Sprintf("%-14s", "+DONE ( TOTAL)"),
		fmt.Sprintf("%-13s", "  RUN / QUEUE"),
		fmt.Sprintf("%-18s", "BATTERY (AVG/MIN)"),
		fmt.Sprintf("%-18s", "THERMAL (AVG/MAX)"),
		fmt.Sprintf("%-16s", "  STEP /   ETA"),
	}

	sep := colorize("│", ansiDimGray)
	var b strings.Builder
	b.WriteString(sep)
	for _, h := range headers {
		b.WriteString(" ")
		b.WriteString(colorize(h, ansiBoldWhite))
		b.WriteString(" ")
		b.WriteString(sep)
	}
	fmt.Println(b.String())
	fmt.Println(colorize(buildBorder("├", "┼", "┤"), ansiDimGray))
}

func buildBorder(left, mid, right string) string {
	var b strings.Builder
	b.WriteString(left)
	for i, w := range tableColWidths {
		b.WriteString(strings.Repeat("─", w+2))
		if i < len(tableColWidths)-1 {
			b.WriteString(mid)
		}
	}
	b.WriteString(right)
	return b.String()
}

func renderProgressBar(pct float64, width int) string {
	filled := int(math.Round((pct / 100.0) * float64(width)))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	empty := width - filled
	return colorize(strings.Repeat("█", filled), ansiBrightCyan) + colorize(strings.Repeat("░", empty), ansiDimGray)
}

func colorize(s, color string) string {
	return color + s + ansiReset
}

func formatCompactUint(n uint64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000.0)
	case n >= 10_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000.0)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func formatShortElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int(d.Seconds())
	days := totalSeconds / 86400
	hours := (totalSeconds % 86400) / 3600
	if days > 0 {
		return fmt.Sprintf("%02dd%02dh", days, hours)
	}
	mins := (totalSeconds % 3600) / 60
	return fmt.Sprintf("%02dh%02dm", hours, mins)
}

func (p *TelemetryExporterPlugin) printStartupBanner(sim types.SimulationController, satCount int, gsCount int) {
	modeDesc := "LIVE (SGP4 + Link Discovery + Cyber-Physical)"
	if p.isPrecomputed {
		modeDesc = "PRECOMPUTED (Multi-threaded O(1) State Iterator)"
	}

	stepDt := sim.GetSimulationTime().Sub(p.simStartTime)
	totalSimDuration := stepDt * time.Duration(p.maxStepCount)
	endTime := p.simStartTime.Add(totalSimDuration)

	fmt.Println()
	fmt.Printf(" %s %s %s  %s %s %s  %s %s\n",
		colorize("STARDUST SIMULATOR", ansiBoldCyan),
		colorize("│", ansiDimGray),
		colorize("Strategy:", ansiDimGray)+" "+colorize(strings.ToUpper(p.strategyName), ansiBoldMagenta),
		colorize("│", ansiDimGray),
		colorize("Mode:", ansiDimGray)+" "+colorize(modeDesc, ansiBoldWhite),
		colorize("│", ansiDimGray),
		colorize("Constellation:", ansiDimGray),
		colorize(fmt.Sprintf("%d Sats / %d GS", satCount, gsCount), ansiBrightGreen),
	)
	fmt.Printf(" %s %s -> %s (%s, %d steps, dt=%.0fs)\n\n",
		colorize("Time Horizon      ", ansiDimGray)+" "+colorize("│", ansiDimGray),
		colorize(p.simStartTime.UTC().Format("2006-01-02 15:04 UTC"), ansiWhite),
		colorize(endTime.UTC().Format("2006-01-02 15:04 UTC"), ansiWhite),
		colorize(formatElapsedSimTime(totalSimDuration), ansiBrightCyan),
		p.maxStepCount,
		stepDt.Seconds(),
	)
}

func (p *TelemetryExporterPlugin) printFinalSummary() {
	if p.stepsExecuted == 0 {
		return
	}

	fmt.Println(colorize(buildBorder("└", "┴", "┘"), ansiDimGray))

	totalWall := time.Since(p.runWallStart)
	avgStep := time.Duration(int64(p.totalStepWallTime) / int64(p.stepsExecuted))
	elapsedSim := p.lastSimTime.Sub(p.simStartTime)

	modeStr := "LIVE"
	if p.isPrecomputed {
		modeStr = "PRECOMPUTED"
	}

	placementRate := 0.0
	if p.lastTaskMetrics.TotalGenerated > 0 {
		placementRate = (float64(p.lastTaskMetrics.TotalDeployed) / float64(p.lastTaskMetrics.TotalGenerated)) * 100.0
	}
	completionRate := 0.0
	if p.lastTaskMetrics.TotalDeployed > 0 {
		completionRate = (float64(p.lastTaskMetrics.TotalCompleted) / float64(p.lastTaskMetrics.TotalDeployed)) * 100.0
	}

	totalTraffic := atomic.LoadUint64(&network.TotalTransmittedBytes)
	totalGigaCycles := float64(p.lastTaskMetrics.TotalConsumedCycles) / 1e9

	fmt.Println()
	fmt.Printf(" %s\n", colorize(fmt.Sprintf("═══ SIMULATION SUMMARY REPORT [%s | %s] ═══", strings.ToUpper(p.strategyName), modeStr), ansiBoldCyan))
	fmt.Printf("  • %-20s : %s (%d/%d steps | +%s)\n",
		"Simulated Progress",
		colorize("100.0%", ansiBoldGreen),
		p.maxStepCount, p.stepsExecuted,
		formatElapsedSimTime(elapsedSim),
	)
	fmt.Printf("  • %-20s : %s (Avg: %s / step)\n",
		"Wall-Clock Runtime",
		colorize(formatWallDuration(totalWall), ansiBoldWhite),
		formatStepDuration(avgStep),
	)
	fmt.Printf("  • %-20s : %s generated │ %s deployed (%.1f%%) │ %s completed (%.1f%%) │ %d evicted\n",
		"Task Orchestration",
		colorize(fmt.Sprintf("%d", p.lastTaskMetrics.TotalGenerated), ansiWhite),
		colorize(fmt.Sprintf("%d", p.lastTaskMetrics.TotalDeployed), ansiBrightCyan),
		placementRate,
		colorize(fmt.Sprintf("%d", p.lastTaskMetrics.TotalCompleted), ansiBoldGreen),
		completionRate,
		p.lastTaskMetrics.TotalEvicted,
	)
	fmt.Printf("  • %-20s : %.2f ms avg latency │ %.1f s avg turnaround │ %.1f GigaCycles │ %s traffic\n",
		"Network & Compute",
		p.lastTaskMetrics.TotalAvgLatencyMs,
		p.lastTaskMetrics.TotalAvgTurnaroundMs/1000.0,
		totalGigaCycles,
		formatBytes(totalTraffic),
	)
	if p.globalMinSOCNode != "" {
		fmt.Printf("  • %-20s : All-Time Min SOC: %s (%s) │ Final Avg SOC: %.2f%%\n",
			"Battery Envelope",
			colorize(fmt.Sprintf("%.2f%%", p.globalMinSOC), ansiBoldGreen),
			p.globalMinSOCNode,
			p.lastAvgSOC,
		)
	}
	if p.globalMaxTempNode != "" {
		fmt.Printf("  • %-20s : Min: %.1f°C (%s) │ Max: %s (%s) │ Final Avg: %.1f°C\n",
			"Thermal Envelope",
			p.globalMinTemp,
			p.globalMinTempNode,
			colorize(fmt.Sprintf("%.1f°C", p.globalMaxTemp), ansiBrightYel),
			p.globalMaxTempNode,
			p.lastAvgTemp,
		)
	}
	fmt.Println()
}

func formatElapsedSimTime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int(d.Seconds())
	days := totalSeconds / 86400
	hours := (totalSeconds % 86400) / 3600
	mins := (totalSeconds % 3600) / 60
	return fmt.Sprintf("%dd %02dh %02dm", days, hours, mins)
}

func formatStepDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func formatWallDuration(d time.Duration) string {
	if d <= 0 {
		return "00s"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	totalSeconds := int(d.Round(time.Second).Seconds())
	hours := totalSeconds / 3600
	mins := (totalSeconds % 3600) / 60
	secs := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%dh%02dm", hours, mins)
	}
	if mins > 0 {
		return fmt.Sprintf("%02dm %02ds", mins, secs)
	}
	return fmt.Sprintf("%02ds", secs)
}

func formatBytes(b uint64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
		tb = 1024 * gb
	)
	switch {
	case b >= tb:
		return fmt.Sprintf("%.2f TB", float64(b)/float64(tb))
	case b >= gb:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func (p *TelemetryExporterPlugin) startWriter() {
	defer p.writerWg.Done()

	for batch := range p.batchChan {
		for _, row := range batch.NodeRows {
			if row != nil {
				_ = p.nodeWriter.Write(row)
			}
		}
		if batch.NetRow != nil {
			_ = p.networkWriter.Write(batch.NetRow)
		}
		if batch.CompRow != nil {
			_ = p.compWriter.Write(batch.CompRow)
		}
	}

	p.nodeWriter.Flush()
	p.networkWriter.Flush()
	p.compWriter.Flush()
}

// Close flushes and closes all telemetry CSV files cleanly and prints the final summary.
func (p *TelemetryExporterPlugin) Close() {
	close(p.batchChan)
	p.writerWg.Wait()

	if p.nodeWriter != nil {
		p.nodeWriter.Flush()
		_ = p.nodeFile.Close()
	}
	if p.networkWriter != nil {
		p.networkWriter.Flush()
		_ = p.networkFile.Close()
	}
	if p.compWriter != nil {
		p.compWriter.Flush()
		_ = p.compFile.Close()
	}

	p.printFinalSummary()
}
