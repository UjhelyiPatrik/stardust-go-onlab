package types

// IDeployedService defines the structure for a deployed service.
type DeployableService interface {
	Payload

	// GetServiceName returns the name of the service.
	GetServiceName() string

	// GetMemoryUsage returns the current memory usage of the deployed service.
	GetMemoryUsage() float64

	// GetRequiredCycles returns the total number of CPU cycles required to execute the service.
	GetRequiredCycles() uint64

	// ExecuteCycles subtracts processed cycles. Returns (isCompleted, actualConsumedCycles)
	ExecuteCycles(cycles uint64) (bool, uint64)

	// IsDeployed checks if the service has been successfully deployed.
	IsDeployed() bool

	// Deploy starts the service deployment process.
	Deploy() error

	// Remove stops the service and removes the deployment.
	Remove() error
}
