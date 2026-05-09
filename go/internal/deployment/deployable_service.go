package deployment

import (
	"errors"
	"fmt"
	"sync/atomic"
)

// DeployableService represents a deployable service with CPU and memory requirements.
type DeployableService struct {
	ServiceName    string  // The name of the service
	Memory         float64 // Memory required by the service
	sizeBytes      uint64  // Size of the service payload in bytes
	requiredCycles uint64  // Total CPU cycles required to execute the service
	deployed       bool    // Indicates whether the service is currently deployed
}

// NewDeployableService creates a new instance of DeployableService.
func NewDeployableService(serviceName string, megaCycles uint64, memory float64, sizeBytes uint64) (*DeployableService, error) {
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
	}, nil
}

func (s *DeployableService) GetServiceName() string {
	return s.ServiceName
}

// ExecuteCycles subtracts the processed cycles in a lock-free, thread-safe manner.
// Returns a boolean indicating if the task is completed, and the exact number of cycles consumed.
func (s *DeployableService) ExecuteCycles(cycles uint64) (bool, uint64) {
	for {
		current := atomic.LoadUint64(&s.requiredCycles)
		if current <= cycles {
			// Ha a maradék ciklus kevesebb vagy egyenlő a kapottnál, a feladat kész
			// Csak annyi ciklust fogyasztott el, amennyi hátra volt (current)
			if atomic.CompareAndSwapUint64(&s.requiredCycles, current, 0) {
				return true, current
			}
		} else {
			// Ha még van hátra a feladatból, az összes kapott ciklust felhasználja
			if atomic.CompareAndSwapUint64(&s.requiredCycles, current, current-cycles) {
				return false, cycles
			}
		}
		// Ha a CAS elbukott (párhuzamos olvasás/írás miatt), újrapróbálja
	}
}

// GetRequiredCycles lock-free read
func (s *DeployableService) GetRequiredCycles() uint64 {
	return atomic.LoadUint64(&s.requiredCycles)
}

func (s *DeployableService) GetMemoryUsage() float64 {
	return s.Memory
}

func (s *DeployableService) IsDeployed() bool {
	return s.deployed
}

func (s *DeployableService) Deploy() error {
	s.deployed = true
	return nil
}

func (s *DeployableService) Remove() error {
	s.deployed = false
	return nil
}

// SizeBytes returns the size of the payload in bytes. Fulfills the types.Payload interface.
func (s *DeployableService) SizeBytes() uint64 {
	return s.sizeBytes
}
