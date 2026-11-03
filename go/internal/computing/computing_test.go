package computing_test

import (
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/internal/computing"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

type dummyDeployableTask struct {
	name           string
	memory         float64
	initialCycles  uint64
	requiredCycles uint64
	sizeBytes      uint64
	originGS       types.Node
	createdAt      time.Time
}

func newDummyTask(name string, cycles uint64, memory float64) *dummyDeployableTask {
	return &dummyDeployableTask{
		name:           name,
		initialCycles:  cycles,
		requiredCycles: cycles,
		memory:         memory,
		sizeBytes:      1024,
		createdAt:      time.Now(),
	}
}

func (d *dummyDeployableTask) GetServiceName() string    { return d.name }
func (d *dummyDeployableTask) GetRequiredCycles() uint64 { return atomic.LoadUint64(&d.requiredCycles) }
func (d *dummyDeployableTask) GetMemoryUsage() float64   { return d.memory }
func (d *dummyDeployableTask) IsDeployed() bool          { return true }
func (d *dummyDeployableTask) Deploy() error             { return nil }
func (d *dummyDeployableTask) Remove() error             { return nil }
func (d *dummyDeployableTask) SizeBytes() uint64         { return d.sizeBytes }
func (d *dummyDeployableTask) GetOriginGS() types.Node   { return d.originGS }
func (d *dummyDeployableTask) GetCreatedAt() time.Time   { return d.createdAt }

func (d *dummyDeployableTask) ExecuteCycles(cycles uint64) (bool, uint64) {
	for {
		curr := atomic.LoadUint64(&d.requiredCycles)
		if curr <= cycles {
			if atomic.CompareAndSwapUint64(&d.requiredCycles, curr, 0) {
				return true, curr
			}
		} else {
			if atomic.CompareAndSwapUint64(&d.requiredCycles, curr, curr-cycles) {
				return false, cycles
			}
		}
	}
}

func (d *dummyDeployableTask) CreateResult(consumedCapacity uint64) types.TaskResult {
	return &dummyTaskResult{
		name:             d.name,
		originGS:         d.originGS,
		createdAt:        d.createdAt,
		consumedCapacity: consumedCapacity,
	}
}

type dummyTaskResult struct {
	name             string
	originGS         types.Node
	createdAt        time.Time
	consumedCapacity uint64
}

func (r *dummyTaskResult) GetServiceName() string      { return r.name }
func (r *dummyTaskResult) GetOriginGS() types.Node     { return r.originGS }
func (r *dummyTaskResult) GetCreatedAt() time.Time     { return r.createdAt }
func (r *dummyTaskResult) GetConsumedCapacity() uint64 { return r.consumedCapacity }
func (r *dummyTaskResult) GetResultSizeBytes() uint64  { return 100 }
func (r *dummyTaskResult) SizeBytes() uint64           { return 100 }

type dummyNode struct {
	name string
}

func (n *dummyNode) GetName() string                          { return n.name }
func (n *dummyNode) GetPosition() types.Vector                { return types.Vector{} }
func (n *dummyNode) DistanceTo(other types.Node) float64      { return 0 }
func (n *dummyNode) GetRouter() types.Router                  { return nil }
func (n *dummyNode) GetComputing() types.Computing            { return nil }
func (n *dummyNode) GetLinkNodeProtocol() types.LinkNodeProtocol { return nil }
func (n *dummyNode) UpdatePosition(simTime time.Time)         {}

// =============================================================================
// Computing Memory Management & Placement Tests
// =============================================================================

func TestComputingPlacementAndMemoryManagement(t *testing.T) {
	comp := computing.NewComputing(4.0, 16000.0, types.Edge)
	node := &dummyNode{name: "TestNode"}

	// Before mounting, placement should fail
	task1 := newDummyTask("Task1", 1000, 4000.0)
	placed, err := comp.TryPlaceDeploymentAsync(task1)
	if err == nil || placed {
		t.Errorf("expected placement to fail before mount")
	}

	// Mount to node
	if err := comp.Mount(node); err != nil {
		t.Fatalf("Mount failed: %v", err)
	}

	// Place Task 1: 4000 MB out of 16000 MB
	placed, err = comp.TryPlaceDeploymentAsync(task1)
	if err != nil || !placed {
		t.Fatalf("expected Task 1 to be placed successfully")
	}

	if comp.MemoryUsage != 4000.0 {
		t.Errorf("expected MemoryUsage 4000.0, got %f", comp.MemoryUsage)
	}
	if comp.MemoryAvailable() != 12000.0 {
		t.Errorf("expected MemoryAvailable 12000.0, got %f", comp.MemoryAvailable())
	}
	if !comp.HostsService("Task1") {
		t.Errorf("expected HostsService(Task1) to be true")
	}

	// Placing duplicate task name should fail
	placed, _ = comp.TryPlaceDeploymentAsync(task1)
	if placed {
		t.Errorf("expected duplicate task placement to be rejected")
	}

	// Place Task 2: 10000 MB -> total 14000 MB
	task2 := newDummyTask("Task2", 1000, 10000.0)
	placed, err = comp.TryPlaceDeploymentAsync(task2)
	if err != nil || !placed {
		t.Fatalf("expected Task 2 to be placed successfully")
	}

	if comp.MemoryUsage != 14000.0 {
		t.Errorf("expected MemoryUsage 14000.0, got %f", comp.MemoryUsage)
	}
	if comp.MemoryAvailable() != 2000.0 {
		t.Errorf("expected MemoryAvailable 2000.0, got %f", comp.MemoryAvailable())
	}

	// Placing Task 3 requiring 3000 MB should fail (only 2000 MB available)
	task3 := newDummyTask("Task3", 1000, 3000.0)
	if comp.CanPlace(task3) {
		t.Errorf("expected CanPlace(task3) to return false (insufficient memory)")
	}
	placed, _ = comp.TryPlaceDeploymentAsync(task3)
	if placed {
		t.Errorf("expected placement of task3 to fail due to lack of memory")
	}

	// Remove Task 1 -> should free 4000 MB (leaving 10000 MB used, 6000 MB available)
	if err := comp.RemoveDeploymentAsync(task1); err != nil {
		t.Fatalf("RemoveDeploymentAsync failed: %v", err)
	}
	if comp.MemoryUsage != 10000.0 {
		t.Errorf("expected MemoryUsage 10000.0 after removal, got %f", comp.MemoryUsage)
	}
	if comp.MemoryAvailable() != 6000.0 {
		t.Errorf("expected MemoryAvailable 6000.0 after removal, got %f", comp.MemoryAvailable())
	}
	if comp.HostsService("Task1") {
		t.Errorf("expected HostsService(Task1) to be false after removal")
	}
}

// =============================================================================
// Computing Tick, Cycle Processing & Duty Cycle Tests
// =============================================================================

func TestComputingTickCycleCalculations(t *testing.T) {
	// Node has 2.0 CPU cores (2 GHz = 2 * 10^9 cycles/sec)
	cpuCores := 2.0
	comp := computing.NewComputing(cpuCores, 8000.0, types.Edge)
	node := &dummyNode{name: "ComputeNode"}
	_ = comp.Mount(node)

	// In deltaT = 0.5s:
	// availableCyclesPerSec = 2 * 10^9
	// totalAvailableCycles = 2 * 10^9 * 0.5 = 1,000,000,000 cycles (1 Gcycles)
	deltaT := 0.5

	// Add 2 tasks:
	// Task A: needs 400,000,000 cycles (400 Mcycles)
	// Task B: needs 800,000,000 cycles (800 Mcycles)
	taskA := newDummyTask("TaskA", 400_000_000, 2000.0)
	taskB := newDummyTask("TaskB", 800_000_000, 3000.0)

	_, _ = comp.TryPlaceDeploymentAsync(taskA)
	_, _ = comp.TryPlaceDeploymentAsync(taskB)

	if comp.GetActiveServiceCount() != 2 {
		t.Fatalf("expected 2 active services, got %d", comp.GetActiveServiceCount())
	}

	// Run Tick:
	// cyclesPerTask = 1,000,000,000 / 2 = 500,000,000 cycles
	// Task A consumes 400M (completes!)
	// Task B consumes 500M (remains: 800M - 500M = 300M cycles)
	// Total consumed cycles = 400M + 500M = 900,000,000 cycles
	// Utilization = 900M / 1000M = 0.90 (90%)
	// CpuUsage = 2.0 * 0.90 = 1.80
	// CpuUtilization = (1.80 / 2.0) * 100% = 90.0%
	now := time.Now()
	comp.Tick(deltaT, now)

	// Check Task A completed & removed, Task B still active
	if comp.GetActiveServiceCount() != 1 {
		t.Errorf("expected 1 active service remaining, got %d", comp.GetActiveServiceCount())
	}
	if comp.HostsService("TaskA") {
		t.Errorf("expected TaskA to have completed and been removed")
	}
	if !comp.HostsService("TaskB") {
		t.Errorf("expected TaskB to still be active")
	}

	// Task A memory (2000 MB) should have been freed, leaving only Task B memory (3000 MB)
	if comp.MemoryUsage != 3000.0 {
		t.Errorf("expected MemoryUsage 3000.0, got %f", comp.MemoryUsage)
	}

	// Check remaining cycles on Task B
	remainingB := taskB.GetRequiredCycles()
	expectedRemainingB := uint64(300_000_000)
	if remainingB != expectedRemainingB {
		t.Errorf("expected TaskB remaining cycles to be %d, got %d", expectedRemainingB, remainingB)
	}

	// Check CPU usage and utilization calculations
	expectedCpuUsage := 1.80
	if math.Abs(comp.CpuUsage-expectedCpuUsage) > 1e-4 {
		t.Errorf("expected CpuUsage %f, got %f", expectedCpuUsage, comp.CpuUsage)
	}

	expectedUtilizationPct := 90.0
	actualUtilizationPct := comp.GetCpuUtilization()
	if math.Abs(actualUtilizationPct-expectedUtilizationPct) > 1e-4 {
		t.Errorf("expected CpuUtilization %f%%, got %f%%", expectedUtilizationPct, actualUtilizationPct)
	}

	expectedCpuAvail := cpuCores - expectedCpuUsage // 0.20
	if math.Abs(comp.CpuAvailable()-expectedCpuAvail) > 1e-4 {
		t.Errorf("expected CpuAvailable %f, got %f", expectedCpuAvail, comp.CpuAvailable())
	}

	// Next Tick:
	// Only Task B is active (needs 300M cycles).
	// cyclesPerTask = 1,000,000,000.
	// Task B consumes 300M (completes!).
	// Total consumed cycles = 300M.
	// Utilization = 300M / 1000M = 0.30 (30%).
	// CpuUsage = 2.0 * 0.30 = 0.60.
	comp.Tick(deltaT, now.Add(500*time.Millisecond))

	if comp.GetActiveServiceCount() != 0 {
		t.Errorf("expected 0 active services remaining, got %d", comp.GetActiveServiceCount())
	}
	if comp.MemoryUsage != 0.0 {
		t.Errorf("expected MemoryUsage 0.0 after all tasks complete, got %f", comp.MemoryUsage)
	}

	expectedNextCpuUsage := 0.60
	if math.Abs(comp.CpuUsage-expectedNextCpuUsage) > 1e-4 {
		t.Errorf("expected second step CpuUsage %f, got %f", expectedNextCpuUsage, comp.CpuUsage)
	}

	// Step with no tasks: CpuUsage should reset to 0.0
	comp.Tick(deltaT, now.Add(1000*time.Millisecond))
	if comp.CpuUsage != 0.0 {
		t.Errorf("expected idle CpuUsage 0.0, got %f", comp.CpuUsage)
	}
	if comp.GetCpuUtilization() != 0.0 {
		t.Errorf("expected idle CpuUtilization 0.0%%, got %f%%", comp.GetCpuUtilization())
	}
}
