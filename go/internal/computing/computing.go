package computing

import (
	"fmt"
	"log"
	"sync"

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
	mu          sync.Mutex
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
	c.mu.Lock()
	defer c.mu.Unlock()

	servicesCopy := make([]types.DeployableService, len(c.Services))
	copy(servicesCopy, c.Services)
	return servicesCopy
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
	c.MemoryUsage += service.GetMemoryUsage() // CPU is NOT reserved statically anymore!

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
func (c *Computing) Tick(deltaT float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.Services) == 0 {
		c.CpuUsage = 0.0 // True Idle
		return
	}

	// Assumption: 1 unit of CPU capacity = 1 GigaCycle/sec (1,000,000,000 cycles).
	availableCyclesPerSec := uint64(c.Cpu * 1000000000)
	totalAvailableCycles := uint64(float64(availableCyclesPerSec) * deltaT)

	if totalAvailableCycles == 0 {
		return
	}

	// Fair Scheduling: Divide available cycles equally
	cyclesPerTask := totalAvailableCycles / uint64(len(c.Services))

	var remainingServices []types.DeployableService
	var totalConsumedCycles uint64 = 0

	for _, service := range c.Services {
		isCompleted, consumed := service.ExecuteCycles(cyclesPerTask)
		totalConsumedCycles += consumed

		if isCompleted {
			c.MemoryUsage -= service.GetMemoryUsage()
			nodeName := "Unknown"
			if c.node != nil {
				nodeName = c.node.GetName()
			}
			log.Printf("[SUCCESS] Computing Node %s completed task: %s (Consumed %d cycles)", nodeName, service.GetServiceName(), consumed)
		} else {
			remainingServices = append(remainingServices, service)
		}
	}

	c.Services = remainingServices

	// DYNAMIC DUTY CYCLE CALCULATION
	// Represents the exact fraction of CPU utilized during this Tick (e.g. 1.66% instead of 100%)
	utilization := float64(totalConsumedCycles) / float64(totalAvailableCycles)

	// Cap at 1.0 just for floating point safety
	if utilization > 1.0 {
		utilization = 1.0
	}

	c.CpuUsage = c.Cpu * utilization
}

func (c *Computing) CanPlace(service types.DeployableService) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.canPlaceLockFree(service)
}

func (c *Computing) canPlaceLockFree(service types.DeployableService) bool {
	// ONLY Memory is a hard hardware constraint. CPU time is dynamically shared.
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
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, s := range c.Services {
		if s.GetServiceName() == serviceName {
			return true
		}
	}
	return false
}

func (c *Computing) CpuAvailable() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Cpu - c.CpuUsage
}

func (c *Computing) MemoryAvailable() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Memory - c.MemoryUsage
}

func (c *Computing) Clone() types.Computing {
	c.mu.Lock()
	defer c.mu.Unlock()

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
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Cpu == 0 {
		return 0.0
	}
	return (c.CpuUsage / c.Cpu) * 100.0
}
