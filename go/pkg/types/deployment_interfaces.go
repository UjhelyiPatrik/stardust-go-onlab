package types

// IDeployedService defines the structure for a deployed service.
type DeployableService interface {
	Payload

	// GetServiceName returns the name of the service.
	GetServiceName() string

	// GetCpuUsage returns the current CPU usage of the deployed service.
	GetCpuUsage() float64

	// GetMemoryUsage returns the current memory usage of the deployed service.
	GetMemoryUsage() float64

	// IsDeployed checks if the service has been successfully deployed.
	IsDeployed() bool

	// Deploy starts the service deployment process.
	Deploy() error

	// Remove stops the service and removes the deployment.
	Remove() error
}
