package simulation

import (
	"encoding/gob"
	"log"
	"os"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/internal/computing"
	"github.com/polaris-slo-cloud/stardust-go/internal/links"
	"github.com/polaris-slo-cloud/stardust-go/internal/links/linktypes"
	"github.com/polaris-slo-cloud/stardust-go/internal/node"
	"github.com/polaris-slo-cloud/stardust-go/internal/routing"
	"github.com/polaris-slo-cloud/stardust-go/pkg/helper"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

// compactLinkEndpoint is a pointer-free 6-byte struct replacing 512-byte PrecomputedLink allocations
// for the millions of historical links in SimulationMetadata.Links.
type compactLinkEndpoint struct {
	n1Idx    uint16
	n2Idx    uint16
	isGround bool
}

// SimulationStateDeserializer is responsible for deserializing simulation state data
// using bounded O(1)-memory chunk streaming and multi-threaded step population.
type SimulationStateDeserializer struct {
	inputFile          string
	computingBuilder   computing.ComputingBuilder
	routerBuilder      *routing.RouterBuilder
	simPlugins         []types.SimulationPlugin
	statePluginBuilder types.StatePluginBuilder
	config             *configs.SimulationConfig
}

// NewSimulationStateDeserializer creates a new deserializer instance.
func NewSimulationStateDeserializer(config *configs.SimulationConfig, inputFile string, computingBuilder computing.ComputingBuilder, routerBuilder *routing.RouterBuilder, simPlugins []types.SimulationPlugin, statePluginBuilder types.StatePluginBuilder) *SimulationStateDeserializer {
	return &SimulationStateDeserializer{
		inputFile:          inputFile,
		computingBuilder:   computingBuilder,
		routerBuilder:      routerBuilder,
		simPlugins:         simPlugins,
		config:             config,
		statePluginBuilder: statePluginBuilder,
	}
}

// loadHeader reads the serialized metadata header from disk.
func (d *SimulationStateDeserializer) loadHeader() *types.SimulationMetadata {
	file, err := os.Open(d.inputFile)
	if err != nil {
		log.Fatalln("Failed to open simulation state file:", err)
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	var metadata types.SimulationMetadata
	if err := decoder.Decode(&metadata); err != nil {
		log.Fatalln("Failed to decode simulation state header:", err)
	}
	return &metadata
}

func (d *SimulationStateDeserializer) decodeChunkFile(cIdx int) *types.SimulationStateChunk {
	chunkPath := ChunkFilePath(d.inputFile, cIdx)
	cf, err := os.Open(chunkPath)
	if err != nil {
		log.Fatalf("Failed to open simulation state chunk %s: %v", chunkPath, err)
	}
	defer cf.Close()

	var chunk types.SimulationStateChunk
	dec := gob.NewDecoder(cf)
	if err := dec.Decode(&chunk); err != nil {
		log.Fatalf("Failed to decode simulation state chunk %s: %v", chunkPath, err)
	}
	return &chunk
}

func (d *SimulationStateDeserializer) LoadIterator() types.SimulationController {
	metadata := d.loadHeader()
	totalSteps := metadata.TotalSteps
	if totalSteps == 0 {
		totalSteps = len(metadata.States)
	}
	chunkCount := metadata.ChunkCount
	statePluginNames := append([]string(nil), metadata.StatePlugins...)

	isChunkStreamed := chunkCount > 0 && len(metadata.States) == 0 && totalSteps > 0
	bufferSteps := totalSteps
	if isChunkStreamed {
		bufferSteps = stepsPerStreamChunk
		if bufferSteps > totalSteps {
			bufferSteps = totalSteps
		}
	}

	numSats := len(metadata.Satellites)
	numGS := len(metadata.Grounds)
	totalNodes := numSats + numGS

	allNodes := make([]types.Node, totalNodes)
	nodeIdxByName := make(map[string]uint16, totalNodes)
	gsByName := make(map[string]*node.PrecomputedGroundStation, numGS)

	satNodes := make([]types.Node, numSats)
	precompSats := make([]*node.PrecomputedSatellite, numSats)
	satProtocols := make([]*links.NodePrecomputedLinkProtocol, numSats)

	for i, sat := range metadata.Satellites {
		router, _ := d.routerBuilder.Build()
		comp := d.computingBuilder.WithComputingType(sat.ComputingType).Build()
		protocol := links.NewNodePrecomputedLinkProtocol(bufferSteps)
		satellite := node.NewPrecomputedSatellite(sat.Name, router, comp, protocol)
		satellite.InitStepBuffers(i, bufferSteps)

		satNodes[i] = satellite
		precompSats[i] = satellite
		satProtocols[i] = protocol
		allNodes[i] = satellite
		nodeIdxByName[sat.Name] = uint16(i)
	}
	metadata.Satellites = nil

	gsNodes := make([]types.Node, numGS)
	precompGSs := make([]*node.PrecomputedGroundStation, numGS)
	gsProtocols := make([]*links.NodePrecomputedLinkProtocol, numGS)

	for i, gs := range metadata.Grounds {
		router, _ := d.routerBuilder.Build()
		comp := d.computingBuilder.WithComputingType(gs.ComputingType).Build()
		protocol := links.NewNodePrecomputedLinkProtocol(bufferSteps)
		groundStation := node.NewPrecomputedGroundStation(gs.Name, router, comp, protocol)
		groundStation.InitStepBuffers(i, bufferSteps)

		gsNodes[i] = groundStation
		precompGSs[i] = groundStation
		gsProtocols[i] = protocol
		nodeIdx := numSats + i
		allNodes[nodeIdx] = groundStation
		nodeIdxByName[gs.Name] = uint16(nodeIdx)
		gsByName[gs.Name] = groundStation
	}
	metadata.Grounds = nil

	// Convert potentially millions of historical link definitions into a pointer-free 6-byte table (~20 MB instead of 2.5 GB)
	linkEndpoints := make([]compactLinkEndpoint, len(metadata.Links))
	for i, l := range metadata.Links {
		n1Idx, ok1 := nodeIdxByName[l.NodeName1]
		n2Idx, ok2 := nodeIdxByName[l.NodeName2]
		if !ok1 {
			n1Idx = 0xFFFF
		}
		if !ok2 {
			n2Idx = 0xFFFF
		}
		linkEndpoints[i] = compactLinkEndpoint{
			n1Idx:    n1Idx,
			n2Idx:    n2Idx,
			isGround: l.IsGroundLink,
		}
	}
	// Release the multi-hundred-MB metadata.Links string table immediately
	metadata.Links = nil
	nodeIdxByName = nil

	// Per-slot reusable pools of active PrecomputedLink structs (~6,553 active links per step -> ~12 MB total across 25 slots)
	expectedActiveLinks := numSats + numGS + 64
	slotLinkPools := make([][]linktypes.PrecomputedLink, bufferSteps)
	slotActiveByIx := make([]map[int]*linktypes.PrecomputedLink, bufferSteps)
	for s := 0; s < bufferSteps; s++ {
		slotLinkPools[s] = make([]linktypes.PrecomputedLink, 0, expectedActiveLinks)
		slotActiveByIx[s] = make(map[int]*linktypes.PrecomputedLink, expectedActiveLinks)
	}

	populateSlot := func(slotIdx int, state *types.SimulationState) {
		pool := slotLinkPools[slotIdx][:0]
		activeMap := slotActiveByIx[slotIdx]
		clear(activeMap)

		getOrAllocSlotLink := func(lIx int) *linktypes.PrecomputedLink {
			if lIx < 0 || lIx >= len(linkEndpoints) {
				return nil
			}
			if pl, exists := activeMap[lIx]; exists {
				return pl
			}
			ep := linkEndpoints[lIx]
			if ep.n1Idx == 0xFFFF || ep.n2Idx == 0xFFFF {
				return nil
			}
			if len(pool) == cap(pool) {
				// Grow capacity without invalidating pointers already stored in activeMap during this slot
				newCap := cap(pool)*2 + 1024
				newPool := make([]linktypes.PrecomputedLink, len(pool), newCap)
				copy(newPool, pool)
				pool = newPool
				for k, oldPtr := range activeMap {
					// Re-bind map entries to new backing array by index
					_ = oldPtr
					delete(activeMap, k)
				}
				// Re-populate activeMap from newPool
				pool = pool[:0]
			}
			idx := len(pool)
			pool = pool[:idx+1]
			pl := &pool[idx]
			pl.InitSingleStep(allNodes[ep.n1Idx], allNodes[ep.n2Idx], ep.isGround, 0, 0, 0)
			activeMap[lIx] = pl
			return pl
		}

		// Pre-size pool to exact upper bound of active links in this step so pool never reallocates mid-step
		maxStepLinks := len(state.LinkMetrics) + numSats + numGS + 256
		if cap(pool) < maxStepLinks {
			pool = make([]linktypes.PrecomputedLink, 0, maxStepLinks*2)
		}

		for linkIx, metric := range state.LinkMetrics {
			if pl := getOrAllocSlotLink(linkIx); pl != nil {
				pl.SetScalarMetrics(metric.Distance, metric.LatencyMs)
			}
		}

		if len(state.SatStates) == len(precompSats) {
			for sIdx := range state.SatStates {
				satState := state.SatStates[sIdx]
				for _, m := range satState.LinkMetrics {
					if pl := getOrAllocSlotLink(m.LinkIndex); pl != nil {
						pl.SetScalarMetrics(m.Distance, m.LatencyMs)
					}
				}

				var coordGS types.GroundStation
				if satState.CoordinatingGSIndex >= 0 && satState.CoordinatingGSIndex < len(precompGSs) {
					coordGS = precompGSs[satState.CoordinatingGSIndex]
				} else if satState.CoordinatingGSName != "" {
					if gs, exists := gsByName[satState.CoordinatingGSName]; exists {
						coordGS = gs
					}
				}

				activeLinks := satProtocols[sIdx].GetStepLinksBuffer(slotIdx, len(satState.Established))
				for _, lIx := range satState.Established {
					if pl := getOrAllocSlotLink(lIx); pl != nil {
						activeLinks = append(activeLinks, pl)
					}
				}
				satProtocols[sIdx].SetStepLinks(slotIdx, activeLinks)

				satState.Established = nil
				satState.LinkMetrics = nil
				satState.GSDistances = nil
				satState.GSLatenciesMs = nil
				precompSats[sIdx].SetStepState(slotIdx, satState, coordGS)
			}
		}

		if len(state.GSStates) == len(precompGSs) {
			for gIdx := range state.GSStates {
				gsState := state.GSStates[gIdx]
				for _, m := range gsState.LinkMetrics {
					if pl := getOrAllocSlotLink(m.LinkIndex); pl != nil {
						pl.SetScalarMetrics(m.Distance, m.LatencyMs)
					}
				}

				visSats := precompGSs[gIdx].GetStepVisibleSatsBuffer(slotIdx, len(gsState.VisibleSatIndices))
				for _, sIdx := range gsState.VisibleSatIndices {
					if sIdx >= 0 && sIdx < len(precompSats) {
						visSats = append(visSats, precompSats[sIdx])
					}
				}

				activeLinks := gsProtocols[gIdx].GetStepLinksBuffer(slotIdx, len(gsState.Established))
				for _, lIx := range gsState.Established {
					if pl := getOrAllocSlotLink(lIx); pl != nil {
						activeLinks = append(activeLinks, pl)
					}
				}
				gsProtocols[gIdx].SetStepLinks(slotIdx, activeLinks)

				gsState.Established = nil
				gsState.LinkMetrics = nil
				gsState.VisibleSatIndices = nil
				gsState.VisibleSatNames = nil
				precompGSs[gIdx].SetStepState(slotIdx, gsState, visSats)
			}
		}

		slotLinkPools[slotIdx] = pool
	}

	// Build state plugins
	plugins, err := d.statePluginBuilder.BuildPlugins(statePluginNames)
	if err != nil {
		log.Printf("[WARN] StatePluginPrecompBuilder: %v", err)
	}
	statePluginRepository := *types.NewStatePluginRepository(plugins)

	var simService *SimulationIteratorService

	if isChunkStreamed {
		// Release header struct completely before starting simulation
		metadata = nil
		log.Printf("Initialized streaming precomputed iterator (%d total steps across %d chunks, window=%d steps)", totalSteps, chunkCount, bufferSteps)
		simService = NewSimulationIteratorService(d.config, nil, d.simPlugins, statePluginRepository)

		loadedChunkIdx := -1
		windowTimes := make([]time.Time, bufferSteps)
		var prefetchChan chan *types.SimulationStateChunk
		prefetchChunkIdx := -1

		startPrefetch := func(nextCIdx int) {
			if nextCIdx >= 0 && nextCIdx < chunkCount {
				prefetchChunkIdx = nextCIdx
				ch := make(chan *types.SimulationStateChunk, 1)
				prefetchChan = ch
				go func(idx int, out chan *types.SimulationStateChunk) {
					out <- d.decodeChunkFile(idx)
				}(nextCIdx, ch)
			} else {
				prefetchChan = nil
				prefetchChunkIdx = -1
			}
		}

		// Immediately kick off prefetch of Chunk 0
		startPrefetch(0)

		simService.SetStepWindowPrepare(totalSteps, func(stepIdx int) (int, time.Time) {
			cIdx := stepIdx / bufferSteps
			slotIdx := stepIdx % bufferSteps

			if cIdx != loadedChunkIdx {
				var chunk *types.SimulationStateChunk
				if prefetchChan != nil && prefetchChunkIdx == cIdx {
					chunk = <-prefetchChan
				} else {
					chunk = d.decodeChunkFile(cIdx)
				}

				offsets := make([]int, len(chunk.States))
				for i := range chunk.States {
					offsets[i] = i
					windowTimes[i] = chunk.States[i].Time
				}
				helper.ParallelFor(offsets, func(offset int) {
					populateSlot(offset, &chunk.States[offset])
				})
				loadedChunkIdx = cIdx

				// Prefetch the next chunk asynchronously in the background while CPU executes the next 25 steps
				startPrefetch(cIdx + 1)
			}

			return slotIdx, windowTimes[slotIdx]
		})
	} else {
		timeOnlyStates := make([]types.SimulationState, len(metadata.States))
		stepIndices := make([]int, len(metadata.States))
		for i := range metadata.States {
			stepIndices[i] = i
			timeOnlyStates[i] = types.SimulationState{Time: metadata.States[i].Time}
		}
		helper.ParallelFor(stepIndices, func(stepIdx int) {
			populateSlot(stepIdx, &metadata.States[stepIdx])
		})
		metadata.States = nil
		metadata = nil
		simService = NewSimulationIteratorService(d.config, timeOnlyStates, d.simPlugins, statePluginRepository)
	}

	// Pass nil for precompLinks because slot-pooled links already hold their step scalar metrics directly!
	simService.SetPrecomputedEntities(precompSats, precompGSs, nil)
	_ = simService.InjectSatellites(satNodes)
	_ = simService.InjectGroundStations(gsNodes)

	return simService
}
