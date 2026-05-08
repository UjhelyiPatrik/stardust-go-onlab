package types

// GroundStation represents a ground station node
type GroundStation interface {
	Node

	// SetVisibleSatellites assigns the list of satellites currently under this GS's control.
	SetVisibleSatellites(sats []Satellite)

	// GetVisibleSatellites returns the list of satellites currently overseen by this GS.
	GetVisibleSatellites() []Satellite

	// EnqueueTask adds a new task to the ground station's local queue.
	EnqueueTask(task DeployableService)

	// GetTaskQueue returns all pending tasks currently queued at this ground station.
	GetTaskQueue() []DeployableService

	// ClearTaskQueue clears the task queue (typically called after successful placement).
	ClearTaskQueue()
}
