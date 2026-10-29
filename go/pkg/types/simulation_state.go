package types

import (
	"time"
)

type SimulationLink struct {
	NodeName1    string
	NodeName2    string
	IsGroundLink bool
	Bandwidth    float64
}

// PrecomputedLinkMetrics holds the precomputed distance, communication delay, and bandwidth of a link at a specific tick.
type PrecomputedLinkMetrics struct {
	LinkIndex    int     `json:"linkIndex"`
	Distance     float64 `json:"distance"`     // Distance in meters
	LatencyMs    float64 `json:"latencyMs"`    // Propagation delay in milliseconds
	Bandwidth    float64 `json:"bandwidth"`    // Link bandwidth in bps
	IsGroundLink bool    `json:"isGroundLink"` // True if this is a Ground-to-Satellite link
}

// PrecomputedSatState stores all deterministic, precomputable physical, orbital, topological,
// ground-station distance, link latency, solar, eclipse, thermal, and power generation states
// for a single satellite at a single simulation time step.
type PrecomputedSatState struct {
	SatIndex int    `json:"satIndex"`
	Name     string `json:"name"`

	// Orbital position (ECEF / ECI vector in meters)
	Position Vector `json:"position"`

	// Active links (ISL + GSL) and their precomputed communication latencies/distances
	Established []int                    `json:"established"`
	LinkMetrics []PrecomputedLinkMetrics `json:"linkMetrics"`

	// Control Plane & Ground Station Distance / Communication Delay
	CoordinatingGSIndex     int     `json:"coordinatingGSIndex"`     // Index of the overseeing GS (-1 if none)
	CoordinatingGSName      string  `json:"coordinatingGSName"`      // Name of the overseeing GS
	CoordinatingGSDistance  float64 `json:"coordinatingGSDistance"`  // Distance to coordinating GS in meters
	CoordinatingGSLatencyMs float64 `json:"coordinatingGSLatencyMs"` // One-way communication delay to coordinating GS in ms

	// Distances (m) and one-way propagation latencies (ms) to all Ground Stations (indexed by GSIndex)
	GSDistances   []float64 `json:"gsDistances,omitempty"`
	GSLatenciesMs []float64 `json:"gsLatenciesMs,omitempty"`

	// Solar & Eclipse Geometry (Conical Umbra/Penumbra model)
	InEclipse        bool    `json:"inEclipse"`
	EclipseDepth     float64 `json:"eclipseDepth"`     // 0.0 = full sun, (0..1) = penumbra, 1.0 = full umbra
	SunlightExposure float64 `json:"sunlightExposure"` // [0.0, 1.0]
	BetaAngle        float64 `json:"betaAngle"`        // Orbital beta angle in degrees

	// External Thermal Environment Inputs (Watts)
	SolarHeat              float64 `json:"solarHeat"`
	AlbedoHeat             float64 `json:"albedoHeat"`
	IRHeat                 float64 `json:"irHeat"`
	TotalEnvironmentalHeat float64 `json:"totalEnvironmentalHeat"`

	// Solar Panel Energy Harvesting (Watts)
	SolarPowerGeneration float64 `json:"solarPowerGeneration"`
}

// PrecomputedGSState stores all deterministic, precomputable states for a Ground Station at a single time step.
type PrecomputedGSState struct {
	GSIndex int    `json:"gsIndex"`
	Name    string `json:"name"`

	// Earth-fixed rotated position vector in meters
	Position Vector `json:"position"`

	// Active Ground-to-Satellite links and their precomputed communication latencies/distances
	Established []int                    `json:"established"`
	LinkMetrics []PrecomputedLinkMetrics `json:"linkMetrics"`

	// Satellites overseen by this Ground Station in the Control Plane
	VisibleSatIndices          []int    `json:"visibleSatIndices"`
	VisibleSatNames            []string `json:"visibleSatNames"`
	NearestVisibleSatIndex     int      `json:"nearestVisibleSatIndex"`
	NearestVisibleSatDistance  float64  `json:"nearestVisibleSatDistance"`
	NearestVisibleSatLatencyMs float64  `json:"nearestVisibleSatLatencyMs"`
}

type SimulationMetadata struct {
	StatePlugins []string
	Satellites   []RawSatellite
	Grounds      []RawGroundStation
	Links        []SimulationLink
	States       []SimulationState
	ChunkCount   int
	TotalSteps   int
}

// SimulationStateChunk represents a slice of simulation steps saved in a separate file for parallel I/O loading.
type SimulationStateChunk struct {
	ChunkIndex int
	StartStep  int
	States     []SimulationState
}

type SimulationState struct {
	Time        time.Time
	NodeStates  []NodeState
	SatStates   []PrecomputedSatState
	GSStates    []PrecomputedGSState
	LinkMetrics map[int]PrecomputedLinkMetrics
}

type NodeState struct {
	Name        string
	Position    Vector
	Established []int
}

type RawSatellite struct {
	Index         int
	Name          string
	ComputingType ComputingType
}

type RawGroundStation struct {
	Index         int
	Name          string
	ComputingType ComputingType
}

func NewSimulationMetadata() SimulationMetadata {
	return SimulationMetadata{
		StatePlugins: []string{},
		Links:        []SimulationLink{},
		States:       []SimulationState{},
	}
}

func NewSimulationState(time time.Time, nodes []NodeState) SimulationState {
	return SimulationState{
		Time:       time,
		NodeStates: nodes,
	}
}

func NewNodeState(name string, position Vector, established []int) NodeState {
	return NodeState{
		Name:        name,
		Position:    position,
		Established: established,
	}
}
