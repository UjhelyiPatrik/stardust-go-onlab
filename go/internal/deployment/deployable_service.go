package deployment

import (
	"errors"
	"fmt"
)

// DeployableService represents a deployable service with CPU and memory requirements.
type DeployableService struct {
	ServiceName string  // The name of the service
	Cpu         float64 // CPU required by the service
	Memory      float64 // Memory required by the service
	sizeBytes   uint64  // Size of the service payload in bytes
	deployed    bool    // Indicates whether the service is currently deployed
}

// NewDeployableService creates a new instance of DeployableService.
func NewDeployableService(serviceName string, cpu, memory float64, sizeBytes uint64) (*DeployableService, error) {
	if serviceName == "" {
		return nil, errors.New("serviceName cannot be null or empty")
	}
	if cpu <= 0 {
		return nil, fmt.Errorf("cpu must be greater than zero, got %f", cpu)
	}
	if memory <= 0 {
		return nil, fmt.Errorf("memory must be greater than zero, got %f", memory)
	}

	return &DeployableService{
		ServiceName: serviceName,
		Cpu:         cpu,
		Memory:      memory,
		sizeBytes:   sizeBytes,
	}, nil
}

func (s *DeployableService) GetServiceName() string {
	return s.ServiceName
}

func (s *DeployableService) GetCpuUsage() float64 {
	return s.Cpu
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
