package computing

import (
	"fmt"
	"sync"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/internal/metrics"
	"github.com/polaris-slo-cloud/stardust-go/internal/network"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// Computing represents the computing resources of a node.
type Computing struct {
	Cpu         float64 // Total CPU available (e.g., Cores * GHz)
	Memory      float64 // Total memory available (e.g., MB)
	Type        types.ComputingType
	CpuUsage    float64 // DYNAMIC Duty Cycle (calculated per tick)
	MemoryUsage float64 // Statically reserved RAM
	Services    []types.DeployableService
	mu          sync.RWMutex
	node        types.Node
}

// NewComputing creates a new Computing instance.
func NewComputing(cpu, memory float64, ctype types.ComputingType) *Computing {
	return &Computing{
		Cpu:      cpu,
		Memory:   memory,
		Type:     ctype,
		Services: make([]types.DeployableService, 0),
	}
}

func (c *Computing) GetServices() []types.DeployableService {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.Services) == 0 {
		return nil
	}
	servicesCopy := make([]types.DeployableService, len(c.Services))
	copy(servicesCopy, c.Services)
	return servicesCopy
}

func (c *Computing) GetActiveServiceCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.Services)
}

func (c *Computing) Mount(node types.Node) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.node != nil {
		return fmt.Errorf("computing is already mounted to node")
	}
	c.node = node
	return nil
}

// TryPlaceDeploymentAsync reserves memory and queues the service. CPU is dynamically allocated in Tick.
func (c *Computing) TryPlaceDeploymentAsync(service types.DeployableService) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.node == nil {
		return false, fmt.Errorf("computing must be mounted to node before it can be used")
	}

	if !c.canPlaceLockFree(service) {
		return false, nil
	}

	c.Services = append(c.Services, service)
	c.MemoryUsage += service.GetMemoryUsage()

	return true, nil
}

// RemoveDeploymentAsync removes a deployed service prematurely (Eviction).
func (c *Computing) RemoveDeploymentAsync(service types.DeployableService) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, s := range c.Services {
		if s.GetServiceName() == service.GetServiceName() {
			c.Services = append(c.Services[:i], c.Services[i+1:]...)
			c.MemoryUsage -= service.GetMemoryUsage()
			return nil
		}
	}
	return fmt.Errorf("service %s not found on node", service.GetServiceName())
}

// Tick processes the required clock cycles for all running tasks based on elapsed time.
// It sets the precise CpuUsage fraction (Duty Cycle) for the thermal and battery plugins.
func (c *Computing) Tick(deltaT float64, currentTime time.Time) {
	availableCyclesPerSec := uint64(c.Cpu * 1e9)
	totalAvailableCycles := uint64(float64(availableCyclesPerSec) * deltaT)

	if totalAvailableCycles == 0 {
		return
	}

	var completedResults []types.TaskResult
	var totalConsumedCycles uint64 = 0

	func() {
		c.mu.Lock()
		defer c.mu.Unlock()

		if len(c.Services) == 0 {
			c.CpuUsage = 0.0
			return
		}

		cyclesPerTask := totalAvailableCycles / uint64(len(c.Services))
		writeIdx := 0

		for _, service := range c.Services {
			isCompleted, consumed := service.ExecuteCycles(cyclesPerTask)
			totalConsumedCycles += consumed

			if isCompleted {
				c.MemoryUsage -= service.GetMemoryUsage()
				completedResults = append(completedResults, service.CreateResult(service.GetRequiredCycles()))
			} else {
				c.Services[writeIdx] = service
				writeIdx++
			}
		}

		c.Services = c.Services[:writeIdx]

		utilization := float64(totalConsumedCycles) / float64(totalAvailableCycles)
		if utilization > 1.0 {
			utilization = 1.0
		}
		c.CpuUsage = c.Cpu * utilization
	}()

	if len(completedResults) > 0 {
		netService := network.NewNetworkService()

		for _, result := range completedResults {
			latency := 0
			if c.node != nil && result.GetOriginGS() != nil {
				l, err := netService.Transmit(c.node, result.GetOriginGS(), result)
				if err == nil {
					latency = l
				}
			}

			metrics.RecordTaskCompletion(result.GetCreatedAt(), currentTime, latency, result.GetConsumedCapacity())
		}
	}
}

func (c *Computing) CanPlace(service types.DeployableService) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.canPlaceLockFree(service)
}

func (c *Computing) canPlaceLockFree(service types.DeployableService) bool {
	if service.GetMemoryUsage() > (c.Memory - c.MemoryUsage) {
		return false
	}
	for _, s := range c.Services {
		if s.GetServiceName() == service.GetServiceName() {
			return false
		}
	}
	return true
}

func (c *Computing) HostsService(serviceName string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, s := range c.Services {
		if s.GetServiceName() == serviceName {
			return true
		}
	}
	return false
}

func (c *Computing) CpuAvailable() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Cpu - c.CpuUsage
}

func (c *Computing) MemoryAvailable() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Memory - c.MemoryUsage
}

func (c *Computing) Clone() types.Computing {
	c.mu.RLock()
	defer c.mu.RUnlock()

	servicesClone := make([]types.DeployableService, len(c.Services))
	copy(servicesClone, c.Services)

	return &Computing{
		Cpu:         c.Cpu,
		Memory:      c.Memory,
		Type:        c.Type,
		CpuUsage:    c.CpuUsage,
		MemoryUsage: c.MemoryUsage,
		Services:    servicesClone,
		node:        c.node,
	}
}

func (c *Computing) GetComputingType() types.ComputingType {
	return c.Type
}

// GetCpuUtilization returns the CPU usage as a percentage (0.0 to 100.0).
func (c *Computing) GetCpuUtilization() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.Cpu == 0 {
		return 0.0
	}
	return (c.CpuUsage / c.Cpu) * 100.0
}
