package deployment

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// DeployableService represents a deployable service with CPU and memory requirements.
type DeployableService struct {
	ServiceName    string     // The name of the service
	Memory         float64    // Memory required by the service
	sizeBytes      uint64     // Size of the service payload in bytes
	requiredCycles uint64     // Total CPU cycles required to execute the service
	deployed       bool       // Indicates whether the service is currently deployed
	OriginGS       types.Node // The originating Ground Station of the service
	CreatedAt      time.Time  // Timestamp when the service was created
}

// NewDeployableService creates a new instance of DeployableService.
func NewDeployableService(serviceName string, megaCycles uint64, memory float64, sizeBytes uint64, originGS types.Node, createdAt time.Time) (*DeployableService, error) {
	if serviceName == "" {
		return nil, errors.New("serviceName cannot be null or empty")
	}
	if megaCycles <= 0 {
		return nil, fmt.Errorf("megaCycles must be greater than zero, got %d", megaCycles)
	}
	if memory <= 0 {
		return nil, fmt.Errorf("memory must be greater than zero, got %f", memory)
	}

	return &DeployableService{
		ServiceName:    serviceName,
		Memory:         memory,
		requiredCycles: megaCycles * 1000000, // Megacycles to actual cycles
		sizeBytes:      sizeBytes,
		OriginGS:       originGS,
		CreatedAt:      createdAt,
	}, nil
}

func (s *DeployableService) GetServiceName() string    { return s.ServiceName }
func (s *DeployableService) GetRequiredCycles() uint64 { return atomic.LoadUint64(&s.requiredCycles) }
func (s *DeployableService) GetMemoryUsage() float64   { return s.Memory }
func (s *DeployableService) IsDeployed() bool          { return s.deployed }
func (s *DeployableService) Deploy() error             { s.deployed = true; return nil }
func (s *DeployableService) Remove() error             { s.deployed = false; return nil }
func (s *DeployableService) SizeBytes() uint64         { return s.sizeBytes }
func (s *DeployableService) GetOriginGS() types.Node   { return s.OriginGS }
func (s *DeployableService) GetCreatedAt() time.Time   { return s.CreatedAt }

// ExecuteCycles subtracts the processed cycles in a lock-free, thread-safe manner.
// Returns a boolean indicating if the task is completed, and the exact number of cycles consumed.
func (s *DeployableService) ExecuteCycles(cycles uint64) (bool, uint64) {
	for {
		current := atomic.LoadUint64(&s.requiredCycles)
		if current <= cycles {
			// If the remaining cycles are less than or equal to the provided cycles, consume all remaining cycles and mark as completed
			// Only consume the remaining cycles (current)
			if atomic.CompareAndSwapUint64(&s.requiredCycles, current, 0) {
				return true, current
			}
		} else {
			// If there are still cycles left in the task, consume all provided cycles
			if atomic.CompareAndSwapUint64(&s.requiredCycles, current, current-cycles) {
				return false, cycles
			}
		}
		// If the CAS failed (due to concurrent read/write), retry
	}
}

// ÚJ: Implementáljuk a types.DeployableService CreateResult metódusát
func (s *DeployableService) CreateResult(consumedCapacity uint64) types.TaskResult {
	return &TaskResultImpl{
		ServiceName:      s.GetServiceName(),
		OriginGS:         s.GetOriginGS(),
		CreatedAt:        s.GetCreatedAt(),
		ConsumedCapacity: consumedCapacity,
		resultSizeBytes:  s.SizeBytes() / 10,
	}
}

// ==========================================
// TaskResult Implementation
// ==========================================
type TaskResultImpl struct {
	ServiceName      string
	OriginGS         types.Node
	CreatedAt        time.Time
	ConsumedCapacity uint64
	resultSizeBytes  uint64
}

func (r *TaskResultImpl) SizeBytes() uint64           { return r.resultSizeBytes }
func (r *TaskResultImpl) GetServiceName() string      { return r.ServiceName }
func (r *TaskResultImpl) GetOriginGS() types.Node     { return r.OriginGS }
func (r *TaskResultImpl) GetCreatedAt() time.Time     { return r.CreatedAt }
func (r *TaskResultImpl) GetConsumedCapacity() uint64 { return r.ConsumedCapacity }
