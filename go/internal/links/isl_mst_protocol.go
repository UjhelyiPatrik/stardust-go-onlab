package links

import (
	"errors"
	"runtime"
	"sort"
	"sync"

	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/internal/links/linktypes"
	"github.com/polaris-slo-cloud/stardust-go/pkg/helper"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.InterSatelliteLinkProtocol = (*IslMstProtocol)(nil)

const maxISLDistanceSq = configs.MaxISLDistance * configs.MaxISLDistance

// mstEdge is a pointer-free 16-byte struct so the Go Garbage Collector never scans candidate edges.
type mstEdge struct {
	distSq float64
	u      uint32
	v      uint32
}

// NodeFilteredLinkProvider allows wrapper protocols (like LinkFilterProtocol) to retrieve
// the pre-partitioned adjacency list for a single node in O(1) instead of scanning all MST edges.
type NodeFilteredLinkProvider interface {
	GetEstablishedForNode(node types.Node) ([]types.Link, bool)
}

// IslMstProtocol builds a global minimum spanning tree (MST) of ISL links
// using pointer-free parallel squared-distance filtering, L2-cached position vectors,
// on-demand link pooling (avoiding 21M pre-allocated link objects), and Union-Find.
type IslMstProtocol struct {
	allLinks        []*linktypes.IslLink        // Used only if AddLink is called explicitly
	linkEdgeU       []int                       // Endpoint 1 integer index per explicit link
	linkEdgeV       []int                       // Endpoint 2 integer index per explicit link
	linkPool        map[uint64]*linktypes.IslLink // On-demand pool of active MST links keyed by (u<<32 | v)
	established     []*linktypes.IslLink        // Currently active MST links
	resultCache     []types.Link                // Cached result of last UpdateLinks
	nodeEstablished map[types.Node][]types.Link // O(1) per-node adjacency lookup for LinkFilterProtocol

	satellite  types.Node         // First mounted reference satellite
	satellites []types.Node       // All mounted satellites
	nodeToID   map[types.Node]int // Fast O(1) node pointer -> integer ID

	position   types.Vector             // Last position when links were updated
	mu         sync.Mutex               // Protects concurrent access
	resetEvent *helper.ManualResetEvent // Signals when ready for reuse
}

// NewIslMstProtocol initializes an empty protocol instance.
func NewIslMstProtocol() *IslMstProtocol {
	return &IslMstProtocol{
		linkPool:        make(map[uint64]*linktypes.IslLink, 16384),
		established:     []*linktypes.IslLink{},
		nodeEstablished: make(map[types.Node][]types.Link),
		satellites:      make([]types.Node, 0, 1024),
		nodeToID:        make(map[types.Node]int, 1024),
		resetEvent:      helper.NewManualResetEvent(true),
	}
}

// SupportsLazyLinks indicates that IslMstProtocol computes pairs directly from mounted nodes
// without requiring O(N^2) pre-allocated IslLink objects at startup.
func (p *IslMstProtocol) SupportsLazyLinks() bool {
	return true
}

// Mount assigns this protocol to a satellite and registers the satellite in the constellation list.
func (p *IslMstProtocol) Mount(s types.Node) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.satellite == nil {
		p.satellite = s
	}
	if _, exists := p.nodeToID[s]; !exists {
		p.nodeToID[s] = len(p.satellites)
		p.satellites = append(p.satellites, s)
	}
}

// AddLink registers a new candidate link if called explicitly.
func (p *IslMstProtocol) AddLink(link types.Link) {
	if isl, ok := link.(*linktypes.IslLink); ok {
		p.mu.Lock()
		p.allLinks = append(p.allLinks, isl)
		p.mu.Unlock()
	}
}

// ConnectLink adds a link to the established set if not already present.
func (p *IslMstProtocol) ConnectLink(link types.Link) error {
	if isl, ok := link.(*linktypes.IslLink); ok {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, l := range p.established {
			if l == isl {
				return nil
			}
		}
		p.established = append(p.established, isl)
	}
	return nil
}

// DisconnectLink removes a link from the established set.
func (p *IslMstProtocol) DisconnectLink(link types.Link) error {
	if isl, ok := link.(*linktypes.IslLink); ok {
		p.mu.Lock()
		defer p.mu.Unlock()
		for i, l := range p.established {
			if l == isl {
				p.established = append(p.established[:i], p.established[i+1:]...)
				break
			}
		}
	}
	return nil
}

// GetEstablishedForNode returns the active MST links incident to a specific node in O(1).
func (p *IslMstProtocol) GetEstablishedForNode(node types.Node) ([]types.Link, bool) {
	p.resetEvent.Wait()
	p.mu.Lock()
	links, ok := p.nodeEstablished[node]
	p.mu.Unlock()
	return links, ok
}

// ensureIndexed builds the integer index tables if explicit AddLink was used.
func (p *IslMstProtocol) ensureIndexed() {
	if len(p.allLinks) == 0 || len(p.linkEdgeU) == len(p.allLinks) {
		return
	}

	for _, l := range p.allLinks {
		if _, ok := p.nodeToID[l.Node1]; !ok {
			p.nodeToID[l.Node1] = len(p.satellites)
			p.satellites = append(p.satellites, l.Node1)
		}
		if _, ok := p.nodeToID[l.Node2]; !ok {
			p.nodeToID[l.Node2] = len(p.satellites)
			p.satellites = append(p.satellites, l.Node2)
		}
	}

	p.linkEdgeU = make([]int, len(p.allLinks))
	p.linkEdgeV = make([]int, len(p.allLinks))
	for i, l := range p.allLinks {
		u := p.nodeToID[l.Node1]
		v := p.nodeToID[l.Node2]
		p.linkEdgeU[i] = u
		p.linkEdgeV[i] = v
		if u > v {
			u, v = v, u
		}
		edgeKey := (uint64(u) << 32) | uint64(v)
		p.linkPool[edgeKey] = l
	}
}

func (p *IslMstProtocol) getOrCreateLink(u, v uint32) *linktypes.IslLink {
	if u > v {
		u, v = v, u
	}
	edgeKey := (uint64(u) << 32) | uint64(v)
	if link, ok := p.linkPool[edgeKey]; ok {
		return link
	}
	link := linktypes.NewIslLink(p.satellites[u], p.satellites[v])
	p.linkPool[edgeKey] = link
	return link
}

// UpdateLinks builds a new MST if the satellite's position has changed.
func (p *IslMstProtocol) UpdateLinks() ([]types.Link, error) {
	if p.satellite == nil {
		return nil, errors.New("satellite not mounted")
	}

	p.mu.Lock()
	if p.position.Equals(p.satellite.GetPosition()) {
		p.mu.Unlock()
		p.resetEvent.Wait()
		return p.resultCache, nil
	}
	p.position = p.satellite.GetPosition()
	p.resetEvent.Reset()
	p.mu.Unlock()

	p.ensureIndexed()
	numSats := len(p.satellites)

	// Cache positions in a contiguous flat array (155 KB for 6469 sats -> fits entirely in L2 cache!)
	positions := make([]types.Vector, numSats)
	for i, sat := range p.satellites {
		positions[i] = sat.GetPosition()
	}

	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers < 1 {
		numWorkers = 1
	}
	workerEdges := make([][]mstEdge, numWorkers)
	workerIndices := make([]int, numWorkers)
	for w := 0; w < numWorkers; w++ {
		workerIndices[w] = w
	}

	if len(p.allLinks) == 0 {
		// Fast pointer-free upper-triangular evaluation with interleaved row striding for perfect load balance
		helper.ParallelFor(workerIndices, func(w int) {
			local := make([]mstEdge, 0, numSats*4)
			for u := w; u < numSats; u += numWorkers {
				p1 := positions[u]
				u32 := uint32(u)
				for v := u + 1; v < numSats; v++ {
					p2 := positions[v]
					dx := p1.X - p2.X
					dy := p1.Y - p2.Y
					dz := p1.Z - p2.Z
					distSq := dx*dx + dy*dy + dz*dz
					if distSq <= maxISLDistanceSq {
						local = append(local, mstEdge{
							distSq: distSq,
							u:      u32,
							v:      uint32(v),
						})
					}
				}
			}
			workerEdges[w] = local
		})
	} else {
		totalLinks := len(p.allLinks)
		chunkSize := (totalLinks + numWorkers - 1) / numWorkers
		helper.ParallelFor(workerIndices, func(w int) {
			start := w * chunkSize
			if start >= totalLinks {
				return
			}
			end := start + chunkSize
			if end > totalLinks {
				end = totalLinks
			}

			local := make([]mstEdge, 0, (end-start)/16+64)
			for idx := start; idx < end; idx++ {
				u := p.linkEdgeU[idx]
				v := p.linkEdgeV[idx]
				p1 := positions[u]
				p2 := positions[v]

				dx := p1.X - p2.X
				dy := p1.Y - p2.Y
				dz := p1.Z - p2.Z
				distSq := dx*dx + dy*dy + dz*dz

				if distSq <= maxISLDistanceSq {
					local = append(local, mstEdge{
						distSq: distSq,
						u:      uint32(u),
						v:      uint32(v),
					})
				}
			}
			workerEdges[w] = local
		})
	}

	totalEligible := 0
	for _, we := range workerEdges {
		totalEligible += len(we)
	}
	edges := make([]mstEdge, 0, totalEligible)
	for _, we := range workerEdges {
		edges = append(edges, we...)
	}

	// Sort eligible edges by squared distance
	sort.Slice(edges, func(i, j int) bool {
		return edges[i].distSq < edges[j].distSq
	})

	// Array-backed Disjoint-Set (Union-Find) with path halving and union by rank
	parent := make([]int, numSats)
	rank := make([]int, numSats)
	for i := 0; i < numSats; i++ {
		parent[i] = i
	}

	find := func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}

	minHorizonDist := configs.EarthRadius + 10_000.0
	minHorizonDistSq := minHorizonDist * minHorizonDist

	mst := make([]*linktypes.IslLink, 0, numSats)
	nodeAdj := make(map[types.Node][]types.Link, numSats)

	for _, e := range edges {
		u := int(e.u)
		v := int(e.v)
		rootU := find(u)
		rootV := find(v)
		if rootU == rootV {
			continue
		}

		// Fast inline line-of-sight reachability check using cached positions
		p1 := positions[u]
		p2 := positions[v]
		vx := p2.X - p1.X
		vy := p2.Y - p1.Y
		vz := p2.Z - p1.Z
		cx := vy*p1.Z - vz*p1.Y
		cy := vz*p1.X - vx*p1.Z
		cz := vx*p1.Y - vy*p1.X
		crossMagSq := cx*cx + cy*cy + cz*cz
		if e.distSq <= 0 || (crossMagSq/e.distSq) <= minHorizonDistSq {
			continue
		}

		if rank[rootU] < rank[rootV] {
			parent[rootU] = rootV
		} else if rank[rootU] > rank[rootV] {
			parent[rootV] = rootU
		} else {
			parent[rootV] = rootU
			rank[rootU]++
		}

		link := p.getOrCreateLink(e.u, e.v)
		mst = append(mst, link)
		n1 := p.satellites[u]
		n2 := p.satellites[v]
		nodeAdj[n1] = append(nodeAdj[n1], link)
		nodeAdj[n2] = append(nodeAdj[n2], link)

		if len(mst) == numSats-1 {
			break
		}
	}

	p.mu.Lock()
	if len(p.allLinks) == 0 && len(p.linkPool) > numSats*4 {
		clear(p.linkPool)
		for _, l := range mst {
			u := p.nodeToID[l.Node1]
			v := p.nodeToID[l.Node2]
			if u > v {
				u, v = v, u
			}
			edgeKey := (uint64(u) << 32) | uint64(v)
			p.linkPool[edgeKey] = l
		}
	}
	p.established = mst
	p.nodeEstablished = nodeAdj
	p.resultCache = make([]types.Link, len(mst))
	for i, l := range mst {
		p.resultCache[i] = l
	}
	p.mu.Unlock()

	p.resetEvent.Set()
	return p.resultCache, nil
}

// Links returns a snapshot of all pooled or registered links.
func (p *IslMstProtocol) Links() []types.Link {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.allLinks) > 0 {
		result := make([]types.Link, len(p.allLinks))
		for i, l := range p.allLinks {
			result[i] = l
		}
		return result
	}
	result := make([]types.Link, 0, len(p.linkPool))
	for _, l := range p.linkPool {
		result = append(result, l)
	}
	return result
}

// Established returns all currently active MST links.
func (p *IslMstProtocol) Established() []types.Link {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]types.Link, len(p.established))
	for i, l := range p.established {
		result[i] = l
	}
	return result
}
