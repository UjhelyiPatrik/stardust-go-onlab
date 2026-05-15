package metrics

import (
	"sync/atomic"
	"time"
)

// Globális változók a computing metrikák gyűjtésére
var (
	TotalCompletedTasks uint64
	TotalConsumedCycles uint64
	TotalTurnaroundMs   uint64
)

// RecordTaskCompletion szálbiztosan rögzíti az eredményeket
func RecordTaskCompletion(createdAt time.Time, currentTime time.Time, latency int, consumedCycles uint64) {
	atomic.AddUint64(&TotalCompletedTasks, 1)
	atomic.AddUint64(&TotalConsumedCycles, consumedCycles)

	executionDurationMs := currentTime.Sub(createdAt).Milliseconds()
	totalTurnaround := executionDurationMs + int64(latency)

	if totalTurnaround < 0 {
		totalTurnaround = 0
	}

	atomic.AddUint64(&TotalTurnaroundMs, uint64(totalTurnaround))
}
