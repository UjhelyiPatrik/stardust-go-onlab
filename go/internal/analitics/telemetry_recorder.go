package analytics

import (
	"sync/atomic"
	"time"
)

// TelemetryRecorder provides thread-safe recording of task completion statistics.
var (
	TotalCompletedTasks uint64
	TotalConsumedCycles uint64
	TotalTurnaroundMs   uint64
)

// RecordTaskCompletion updates the telemetry metrics when a task is completed, including total completed tasks, consumed cycles, and turnaround time.
func RecordTaskCompletion(createdAt time.Time, currentTime time.Time, latency int, consumedCycles uint64) {
	atomic.AddUint64(&TotalCompletedTasks, 1)
	atomic.AddUint64(&TotalConsumedCycles, consumedCycles)

	// Turnaround time = Execution time + Network latencies
	executionDurationMs := currentTime.Sub(createdAt).Milliseconds()
	totalTurnaround := executionDurationMs + int64(latency)

	if totalTurnaround < 0 {
		totalTurnaround = 0
	}

	atomic.AddUint64(&TotalTurnaroundMs, uint64(totalTurnaround))
}
