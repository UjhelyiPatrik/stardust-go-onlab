package computing

import (
	"fmt"
	"log"
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
func (c *Computing) Tick(deltaT float64, currentTime time.Time) {

	// Change GHz to Cycles: availableCyclesPerSec = GHz * 1e9 (cycles per second)
	availableCyclesPerSec := uint64(c.Cpu * 1e9)
	totalAvailableCycles := uint64(float64(availableCyclesPerSec) * deltaT)

	if totalAvailableCycles == 0 {
		return
	}

	var remainingServices []types.DeployableService
	var completedResults []types.TaskResult // ÚJ: Ide gyűjtjük a kész feladatokat
	var totalConsumedCycles uint64 = 0

	func() {
		c.mu.Lock()
		defer c.mu.Unlock()

		if len(c.Services) == 0 {
			c.CpuUsage = 0.0
			return
		}

		cyclesPerTask := totalAvailableCycles / uint64(len(c.Services))

		for _, service := range c.Services {
			isCompleted, consumed := service.ExecuteCycles(cyclesPerTask)
			totalConsumedCycles += consumed

			if isCompleted {
				c.MemoryUsage -= service.GetMemoryUsage()
				completedResults = append(completedResults, service.CreateResult(service.GetRequiredCycles()))
			} else {
				remainingServices = append(remainingServices, service)
			}
		}

		c.Services = remainingServices

		utilization := float64(totalConsumedCycles) / float64(totalAvailableCycles)
		if utilization > 1.0 {
			utilization = 1.0
		}
		c.CpuUsage = c.Cpu * utilization
	}()

	// --- HÁLÓZATI MŰVELETEK ÉS TELEMETRIA A LOCKON KÍVÜL ---

	if len(completedResults) > 0 {
		netService := network.NewNetworkService()

		for _, result := range completedResults {
			latency := 0
			if c.node != nil && result.GetOriginGS() != nil {
				// Ez a lassú művelet (útvonalkeresés) most már nem blokkolja a memóriát!
				l, err := netService.Transmit(c.node, result.GetOriginGS(), result)
				if err == nil {
					latency = l
				}
			}

			// Telemetria rögzítése
			metrics.RecordTaskCompletion(result.GetCreatedAt(), time.Now(), latency, result.GetConsumedCapacity())

			if c.node != nil {
				log.Printf("[SUCCESS] Computing Node %s completed task: %s. Latency: %d", c.node.GetName(), result.GetServiceName(), latency)
			}
		}
	}
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
