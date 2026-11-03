package metrics_test

import (
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/internal/metrics"
)

func resetAllMetrics() {
	metrics.FlushStepTaskMetrics()
	atomic.StoreUint64(&metrics.TotalCompletedTasks, 0)
	atomic.StoreUint64(&metrics.TotalConsumedCycles, 0)
	atomic.StoreUint64(&metrics.TotalTurnaroundMs, 0)
	atomic.StoreUint64(&metrics.TotalReturnLatencyMs, 0)
	atomic.StoreUint64(&metrics.TotalGeneratedTasks, 0)
	atomic.StoreUint64(&metrics.TotalDeployedTasks, 0)
	atomic.StoreUint64(&metrics.TotalEvictedTasks, 0)
}

func TestMetricsTaskGenerationDeploymentAndEviction(t *testing.T) {
	resetAllMetrics()

	metrics.RecordTasksGenerated(10)
	metrics.RecordTasksDeployed(8)
	metrics.RecordTasksEvicted(2)

	m := metrics.FlushStepTaskMetrics()

	if m.StepGenerated != 10 || m.TotalGenerated != 10 {
		t.Errorf("expected 10 generated tasks, got step %d, tot %d", m.StepGenerated, m.TotalGenerated)
	}
	if m.StepDeployed != 8 || m.TotalDeployed != 8 {
		t.Errorf("expected 8 deployed tasks, got step %d, tot %d", m.StepDeployed, m.TotalDeployed)
	}
	if m.StepEvicted != 2 || m.TotalEvicted != 2 {
		t.Errorf("expected 2 evicted tasks, got step %d, tot %d", m.StepEvicted, m.TotalEvicted)
	}

	// Next step: step counters should be reset, cumulative should persist
	metrics.RecordTasksGenerated(5)
	m2 := metrics.FlushStepTaskMetrics()

	if m2.StepGenerated != 5 {
		t.Errorf("expected 5 step generated tasks in step 2, got %d", m2.StepGenerated)
	}
	if m2.TotalGenerated != 15 {
		t.Errorf("expected 15 cumulative generated tasks in step 2, got %d", m2.TotalGenerated)
	}
}

func TestMetricsTaskCompletionAndTurnaroundAverages(t *testing.T) {
	resetAllMetrics()

	t0 := time.Now()

	// Task 1:
	// createdAt: t0
	// completedAt: t0 + 100ms
	// latency: 20ms
	// consumedCycles: 500,000
	// turnaround = 100ms + 20ms = 120ms
	t1Created := t0
	t1Completed := t0.Add(100 * time.Millisecond)
	metrics.RecordTaskCompletion(t1Created, t1Completed, 20, 500_000)

	// Task 2:
	// createdAt: t0
	// completedAt: t0 + 300ms
	// latency: 40ms
	// consumedCycles: 1,500,000
	// turnaround = 300ms + 40ms = 340ms
	t2Created := t0
	t2Completed := t0.Add(300 * time.Millisecond)
	metrics.RecordTaskCompletion(t2Created, t2Completed, 40, 1_500_000)

	// Flush Step 1:
	// Total tasks = 2
	// Total cycles = 2,000,000
	// Total latency = 20 + 40 = 60ms -> avg = 30ms
	// Total turnaround = 120 + 340 = 460ms -> avg = 230ms
	snap1 := metrics.FlushStepTaskMetrics()

	if snap1.StepCompleted != 2 || snap1.TotalCompleted != 2 {
		t.Errorf("expected 2 completed tasks, got step %d, tot %d", snap1.StepCompleted, snap1.TotalCompleted)
	}
	if snap1.StepConsumedCycles != 2_000_000 || snap1.TotalConsumedCycles != 2_000_000 {
		t.Errorf("expected 2M cycles, got step %d, tot %d", snap1.StepConsumedCycles, snap1.TotalConsumedCycles)
	}
	if math.Abs(snap1.StepAvgLatencyMs-30.0) > 1e-6 {
		t.Errorf("expected StepAvgLatencyMs 30.0, got %f", snap1.StepAvgLatencyMs)
	}
	if math.Abs(snap1.StepAvgTurnaroundMs-230.0) > 1e-6 {
		t.Errorf("expected StepAvgTurnaroundMs 230.0, got %f", snap1.StepAvgTurnaroundMs)
	}

	// Step 2: Add 1 more task
	// Task 3:
	// duration: 50ms, latency: 10ms -> turnaround = 60ms
	// cycles: 1,000,000
	t3Created := t0
	t3Completed := t0.Add(50 * time.Millisecond)
	metrics.RecordTaskCompletion(t3Created, t3Completed, 10, 1_000_000)

	snap2 := metrics.FlushStepTaskMetrics()

	// Step 2 stats: 1 task, latency 10ms, turnaround 60ms, cycles 1M
	if snap2.StepCompleted != 1 {
		t.Errorf("expected Step 2 completed 1, got %d", snap2.StepCompleted)
	}
	if math.Abs(snap2.StepAvgLatencyMs-10.0) > 1e-6 {
		t.Errorf("expected Step 2 AvgLatency 10.0, got %f", snap2.StepAvgLatencyMs)
	}
	if math.Abs(snap2.StepAvgTurnaroundMs-60.0) > 1e-6 {
		t.Errorf("expected Step 2 AvgTurnaround 60.0, got %f", snap2.StepAvgTurnaroundMs)
	}

	// Cumulative stats: 3 tasks
	// Total latency = 60 + 10 = 70ms -> avg = 70/3 = 23.333ms
	// Total turnaround = 460 + 60 = 520ms -> avg = 520/3 = 173.333ms
	// Total cycles = 3,000,000
	if snap2.TotalCompleted != 3 {
		t.Errorf("expected TotalCompleted 3, got %d", snap2.TotalCompleted)
	}
	if snap2.TotalConsumedCycles != 3_000_000 {
		t.Errorf("expected TotalConsumedCycles 3M, got %d", snap2.TotalConsumedCycles)
	}
	expectedTotAvgLat := 70.0 / 3.0
	expectedTotAvgTurn := 520.0 / 3.0
	if math.Abs(snap2.TotalAvgLatencyMs-expectedTotAvgLat) > 1e-4 {
		t.Errorf("expected TotalAvgLatency %f, got %f", expectedTotAvgLat, snap2.TotalAvgLatencyMs)
	}
	if math.Abs(snap2.TotalAvgTurnaroundMs-expectedTotAvgTurn) > 1e-4 {
		t.Errorf("expected TotalAvgTurnaround %f, got %f", expectedTotAvgTurn, snap2.TotalAvgTurnaroundMs)
	}
}
