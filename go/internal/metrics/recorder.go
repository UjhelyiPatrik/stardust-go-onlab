package metrics

import (
	"sync/atomic"
	"time"
)

// Global cumulative and per-step atomic counters for task and orchestration metrics.
var (
	// Cumulative counters (kept exported for backwards compatibility with telemetry CSV exporter)
	TotalCompletedTasks  uint64
	TotalConsumedCycles  uint64
	TotalTurnaroundMs    uint64
	TotalReturnLatencyMs uint64

	TotalGeneratedTasks uint64
	TotalDeployedTasks  uint64
	TotalEvictedTasks   uint64

	// Per-step delta counters (atomically reset at the end of each simulation step)
	StepGeneratedTasks  uint64
	StepDeployedTasks   uint64
	StepEvictedTasks    uint64
	StepCompletedTasks  uint64
	StepConsumedCycles  uint64
	StepTurnaroundMs    uint64
	StepReturnLatencyMs uint64
)

// StepTaskMetrics holds a consistent snapshot of per-step and cumulative task metrics.
type StepTaskMetrics struct {
	StepGenerated       uint64
	StepDeployed        uint64
	StepEvicted         uint64
	StepCompleted       uint64
	StepConsumedCycles  uint64
	StepAvgLatencyMs    float64
	StepAvgTurnaroundMs float64

	TotalGenerated       uint64
	TotalDeployed        uint64
	TotalEvicted         uint64
	TotalCompleted       uint64
	TotalConsumedCycles  uint64
	TotalAvgLatencyMs    float64
	TotalAvgTurnaroundMs float64
}

// RecordTasksGenerated records the number of new tasks generated in the current step.
func RecordTasksGenerated(count uint64) {
	if count == 0 {
		return
	}
	atomic.AddUint64(&StepGeneratedTasks, count)
	atomic.AddUint64(&TotalGeneratedTasks, count)
}

// RecordTasksDeployed records the number of tasks successfully placed on satellites in the current step.
func RecordTasksDeployed(count uint64) {
	if count == 0 {
		return
	}
	atomic.AddUint64(&StepDeployedTasks, count)
	atomic.AddUint64(&TotalDeployedTasks, count)
}

// RecordTasksEvicted records the number of tasks migrated/evicted between satellites in the current step.
func RecordTasksEvicted(count uint64) {
	if count == 0 {
		return
	}
	atomic.AddUint64(&StepEvictedTasks, count)
	atomic.AddUint64(&TotalEvictedTasks, count)
}

// RecordTaskCompletion thread-safely records a completed task's execution and network latency metrics.
func RecordTaskCompletion(createdAt time.Time, currentTime time.Time, latency int, consumedCycles uint64) {
	atomic.AddUint64(&TotalCompletedTasks, 1)
	atomic.AddUint64(&StepCompletedTasks, 1)

	atomic.AddUint64(&TotalConsumedCycles, consumedCycles)
	atomic.AddUint64(&StepConsumedCycles, consumedCycles)

	if latency > 0 {
		atomic.AddUint64(&TotalReturnLatencyMs, uint64(latency))
		atomic.AddUint64(&StepReturnLatencyMs, uint64(latency))
	}

	executionDurationMs := currentTime.Sub(createdAt).Milliseconds()
	totalTurnaround := executionDurationMs + int64(latency)
	if totalTurnaround < 0 {
		totalTurnaround = 0
	}

	atomic.AddUint64(&TotalTurnaroundMs, uint64(totalTurnaround))
	atomic.AddUint64(&StepTurnaroundMs, uint64(totalTurnaround))
}

// FlushStepTaskMetrics atomically extracts and resets the current step's task counters,
// returning both the step deltas and cumulative totals.
func FlushStepTaskMetrics() StepTaskMetrics {
	stepGen := atomic.SwapUint64(&StepGeneratedTasks, 0)
	stepDep := atomic.SwapUint64(&StepDeployedTasks, 0)
	stepEvict := atomic.SwapUint64(&StepEvictedTasks, 0)
	stepComp := atomic.SwapUint64(&StepCompletedTasks, 0)
	stepCycles := atomic.SwapUint64(&StepConsumedCycles, 0)
	stepTurn := atomic.SwapUint64(&StepTurnaroundMs, 0)
	stepLat := atomic.SwapUint64(&StepReturnLatencyMs, 0)

	totGen := atomic.LoadUint64(&TotalGeneratedTasks)
	totDep := atomic.LoadUint64(&TotalDeployedTasks)
	totEvict := atomic.LoadUint64(&TotalEvictedTasks)
	totComp := atomic.LoadUint64(&TotalCompletedTasks)
	totCycles := atomic.LoadUint64(&TotalConsumedCycles)
	totTurn := atomic.LoadUint64(&TotalTurnaroundMs)
	totLat := atomic.LoadUint64(&TotalReturnLatencyMs)

	var stepAvgLat, stepAvgTurn float64
	if stepComp > 0 {
		stepAvgLat = float64(stepLat) / float64(stepComp)
		stepAvgTurn = float64(stepTurn) / float64(stepComp)
	}

	var totAvgLat, totAvgTurn float64
	if totComp > 0 {
		totAvgLat = float64(totLat) / float64(totComp)
		totAvgTurn = float64(totTurn) / float64(totComp)
	}

	return StepTaskMetrics{
		StepGenerated:        stepGen,
		StepDeployed:         stepDep,
		StepEvicted:          stepEvict,
		StepCompleted:        stepComp,
		StepConsumedCycles:   stepCycles,
		StepAvgLatencyMs:     stepAvgLat,
		StepAvgTurnaroundMs:  stepAvgTurn,
		TotalGenerated:       totGen,
		TotalDeployed:        totDep,
		TotalEvicted:         totEvict,
		TotalCompleted:       totComp,
		TotalConsumedCycles:  totCycles,
		TotalAvgLatencyMs:    totAvgLat,
		TotalAvgTurnaroundMs: totAvgTurn,
	}
}
