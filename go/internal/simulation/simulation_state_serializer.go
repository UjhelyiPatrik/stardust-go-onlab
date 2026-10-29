package simulation

import (
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/polaris-slo-cloud/stardust-go/internal/links/linktypes"
	"github.com/polaris-slo-cloud/stardust-go/internal/stateplugin"
	"github.com/polaris-slo-cloud/stardust-go/pkg/helper"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

const (
	linkPropagationSpeed = types.SpeedOfLight * 0.99
	stepsPerStreamChunk  = 25
)

// SimulationStateSerializer streams deterministic simulation states to chunked gob files
// every stepsPerStreamChunk steps so RAM usage remains O(1) regardless of simulation length.
type SimulationStateSerializer struct {
	mu            sync.Mutex
	outputFile    string
	metadata      types.SimulationMetadata
	linksIxMap    map[string]int
	statePlugins  []types.StatePlugin
	pendingStates []types.SimulationState
	totalSteps    int
	chunkCount    int
}

// NewSimulationStateSerializer initializes a new streaming SimulationStateSerializer.
func NewSimulationStateSerializer(outputFile string, statePlugins []types.StatePlugin) *SimulationStateSerializer {
	return &SimulationStateSerializer{
		outputFile:    outputFile,
		metadata:      types.NewSimulationMetadata(),
		linksIxMap:    make(map[string]int, 16384),
		statePlugins:  statePlugins,
		pendingStates: make([]types.SimulationState, 0, stepsPerStreamChunk),
	}
}

func (s *SimulationStateSerializer) getOrRegisterLinkIndex(link types.Link) (int, bool) {
	n1, n2 := link.Nodes()
	_, isGround := link.(*linktypes.GroundLink)
	if !isGround {
		_, isN1GS := n1.(types.GroundStation)
		_, isN2GS := n2.(types.GroundStation)
		isGround = isN1GS || isN2GS
	}

	name1 := n1.GetName()
	name2 := n2.GetName()
	key := name1 + "|" + name2
	if name1 > name2 {
		key = name2 + "|" + name1
	}

	s.mu.Lock()
	if ix, exists := s.linksIxMap[key]; exists {
		s.mu.Unlock()
		return ix, isGround
	}
	linkIx := len(s.metadata.Links)
	s.linksIxMap[key] = linkIx
	s.metadata.Links = append(s.metadata.Links, types.SimulationLink{
		NodeName1:    name1,
		NodeName2:    name2,
		IsGroundLink: isGround,
		Bandwidth:    link.Bandwidth(),
	})
	s.mu.Unlock()
	return linkIx, isGround
}

func (s *SimulationStateSerializer) AddState(simulationController types.SimulationController) {
	sats := simulationController.GetSatellites()
	gss := simulationController.GetGroundStations()

	// Build fast index maps for Ground Stations and Satellites
	gsIndexByName := make(map[string]int, len(gss))
	for i, gs := range gss {
		gsIndexByName[gs.GetName()] = i
	}
	satIndexByName := make(map[string]int, len(sats))
	for i, sat := range sats {
		satIndexByName[sat.GetName()] = i
	}

	// Resolve ThermalEnvironmentStatePlugin if active
	var thermalEnv stateplugin.ThermalEnvironmentPlugin
	for _, sp := range s.statePlugins {
		if tep, ok := sp.(stateplugin.ThermalEnvironmentPlugin); ok {
			thermalEnv = tep
			break
		}
	}

	satStates := make([]types.PrecomputedSatState, len(sats))
	gsStates := make([]types.PrecomputedGSState, len(gss))

	// 1. Parallel extraction of all precomputable satellite states
	satIndices := make([]int, len(sats))
	for i := range sats {
		satIndices[i] = i
	}

	helper.ParallelFor(satIndices, func(i int) {
		sat := sats[i]
		established := sat.GetLinkNodeProtocol().Established()
		linkIxs := make([]int, len(established))
		linkMetrics := make([]types.PrecomputedLinkMetrics, len(established))

		for j, link := range established {
			linkIx, isGround := s.getOrRegisterLinkIndex(link)
			dist := link.Distance()
			latMs := link.Latency()
			bw := link.Bandwidth()

			linkIxs[j] = linkIx
			linkMetrics[j] = types.PrecomputedLinkMetrics{
				LinkIndex:    linkIx,
				Distance:     dist,
				LatencyMs:    latMs,
				Bandwidth:    bw,
				IsGroundLink: isGround,
			}
		}

		// Coordinating Ground Station distance & communication delay
		coordGSIdx := -1
		coordGSName := ""
		coordGSDist := 0.0
		coordGSLatMs := 0.0
		if coordGS := sat.GetCoordinatingGS(); coordGS != nil {
			coordGSName = coordGS.GetName()
			if idx, ok := gsIndexByName[coordGSName]; ok {
				coordGSIdx = idx
			}
			coordGSDist = sat.DistanceTo(coordGS)
			coordGSLatMs = (coordGSDist / linkPropagationSpeed) * 1000.0
		}

		// Environmental heat, eclipse, sunlight exposure, and solar power generation
		var envHeat types.EnvironmentalHeat
		if thermalEnv != nil {
			envHeat = thermalEnv.GetEnvironmentalHeat(sat)
		}

		satStates[i] = types.PrecomputedSatState{
			SatIndex:                i,
			Name:                    sat.GetName(),
			Position:                sat.GetPosition(),
			Established:             linkIxs,
			LinkMetrics:             linkMetrics,
			CoordinatingGSIndex:     coordGSIdx,
			CoordinatingGSName:      coordGSName,
			CoordinatingGSDistance:  coordGSDist,
			CoordinatingGSLatencyMs: coordGSLatMs,
			InEclipse:               envHeat.InEclipse,
			EclipseDepth:            envHeat.EclipseDepth,
			SunlightExposure:        envHeat.SunlightExposure,
			BetaAngle:               envHeat.BetaAngle,
			SolarHeat:               envHeat.SolarHeat,
			AlbedoHeat:              envHeat.AlbedoHeat,
			IRHeat:                  envHeat.IRHeat,
			TotalEnvironmentalHeat:  envHeat.TotalHeat,
			SolarPowerGeneration:    envHeat.SolarPowerGeneration,
		}
	})

	// 2. Parallel extraction of all precomputable Ground Station states
	gsIndices := make([]int, len(gss))
	for i := range gss {
		gsIndices[i] = i
	}

	helper.ParallelFor(gsIndices, func(i int) {
		gs := gss[i]
		established := gs.GetLinkNodeProtocol().Established()
		linkIxs := make([]int, len(established))
		linkMetrics := make([]types.PrecomputedLinkMetrics, len(established))

		for j, link := range established {
			linkIx, isGround := s.getOrRegisterLinkIndex(link)
			dist := link.Distance()
			latMs := link.Latency()
			bw := link.Bandwidth()

			linkIxs[j] = linkIx
			linkMetrics[j] = types.PrecomputedLinkMetrics{
				LinkIndex:    linkIx,
				Distance:     dist,
				LatencyMs:    latMs,
				Bandwidth:    bw,
				IsGroundLink: isGround,
			}
		}

		visSats := gs.GetVisibleSatellites()
		visIndices := make([]int, 0, len(visSats))
		nearestSatIdx := -1
		nearestSatDist := 0.0
		nearestSatLatMs := 0.0

		for _, vs := range visSats {
			vName := vs.GetName()
			sIdx, ok := satIndexByName[vName]
			if ok {
				visIndices = append(visIndices, sIdx)
			}
			d := gs.DistanceTo(vs)
			if nearestSatIdx == -1 || d < nearestSatDist {
				nearestSatIdx = sIdx
				nearestSatDist = d
				nearestSatLatMs = (d / linkPropagationSpeed) * 1000.0
			}
		}

		gsStates[i] = types.PrecomputedGSState{
			GSIndex:                    i,
			Name:                       gs.GetName(),
			Position:                   gs.GetPosition(),
			Established:                linkIxs,
			LinkMetrics:                linkMetrics,
			VisibleSatIndices:          visIndices,
			NearestVisibleSatIndex:     nearestSatIdx,
			NearestVisibleSatDistance:  nearestSatDist,
			NearestVisibleSatLatencyMs: nearestSatLatMs,
		}
	})

	simState := types.SimulationState{
		Time:      simulationController.GetSimulationTime(),
		SatStates: satStates,
		GSStates:  gsStates,
	}

	s.pendingStates = append(s.pendingStates, simState)
	s.totalSteps++

	// Stream chunk to disk and release RAM immediately once stepsPerStreamChunk is reached
	if len(s.pendingStates) >= stepsPerStreamChunk {
		s.flushPendingChunk()
	}
}

func (s *SimulationStateSerializer) flushPendingChunk() {
	if len(s.pendingStates) == 0 {
		return
	}

	chunkIdx := s.chunkCount
	startStep := s.totalSteps - len(s.pendingStates)
	chunk := types.SimulationStateChunk{
		ChunkIndex: chunkIdx,
		StartStep:  startStep,
		States:     s.pendingStates,
	}

	chunkPath := ChunkFilePath(s.outputFile, chunkIdx)
	f, err := os.Create(chunkPath)
	if err != nil {
		log.Printf("error creating chunk gob file %s: %v", chunkPath, err)
	} else {
		enc := gob.NewEncoder(f)
		if err := enc.Encode(chunk); err != nil {
			log.Printf("error encoding chunk %d: %v", chunkIdx, err)
		}
		_ = f.Close()
	}

	s.chunkCount++
	// Reallocate fresh slice header so old SimulationState arrays can be garbage-collected immediately
	s.pendingStates = make([]types.SimulationState, 0, stepsPerStreamChunk)
}

func ChunkFilePath(baseFile string, chunkIdx int) string {
	return fmt.Sprintf("%s.chunk_%03d.gob", baseFile, chunkIdx)
}

func (s *SimulationStateSerializer) Save(simulationController types.SimulationController) {
	// Flush any remaining states in the buffer
	s.flushPendingChunk()

	satellites := simulationController.GetSatellites()
	s.metadata.Satellites = make([]types.RawSatellite, len(satellites))
	for i, sat := range satellites {
		s.metadata.Satellites[i] = types.RawSatellite{
			Index:         i,
			Name:          sat.GetName(),
			ComputingType: sat.GetComputing().GetComputingType(),
		}
	}

	grounds := simulationController.GetGroundStations()
	s.metadata.Grounds = make([]types.RawGroundStation, len(grounds))
	for i, gs := range grounds {
		s.metadata.Grounds[i] = types.RawGroundStation{
			Index:         i,
			Name:          gs.GetName(),
			ComputingType: gs.GetComputing().GetComputingType(),
		}
	}

	s.metadata.StatePlugins = s.metadata.StatePlugins[:0]
	for _, plugin := range s.statePlugins {
		s.metadata.StatePlugins = append(s.metadata.StatePlugins, plugin.GetName())
	}

	s.metadata.TotalSteps = s.totalSteps
	s.metadata.ChunkCount = s.chunkCount
	s.metadata.States = nil

	file, err := os.Create(s.outputFile)
	if err != nil {
		log.Printf("error creating gob header file %s: %v", s.outputFile, err)
		return
	}
	defer file.Close()

	encoder := gob.NewEncoder(file)
	if err := encoder.Encode(s.metadata); err != nil {
		log.Printf("error encoding gob header state: %v", err)
	}

	log.Printf("Saved precomputed simulation state (%d steps across %d streamed chunks) to %s", s.totalSteps, s.chunkCount, s.outputFile)
}
