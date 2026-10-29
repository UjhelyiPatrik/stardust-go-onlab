// Package routing contains routing algorithms used for simulation
package routing

import (
	"container/heap"
	"errors"
	"sort"
	"sync"

	"github.com/polaris-slo-cloud/stardust-go/configs"
	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

type dijkstraNodeItem struct {
	node  types.Node
	gCost float64
	fCost float64
}

type minLatencyHeap []dijkstraNodeItem

func (h minLatencyHeap) Len() int           { return len(h) }
func (h minLatencyHeap) Less(i, j int) bool { return h[i].fCost < h[j].fCost }
func (h minLatencyHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minLatencyHeap) Push(x any)        { *h = append(*h, x.(dijkstraNodeItem)) }
func (h *minLatencyHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1].node = nil
	*h = old[:n-1]
	return item
}

type astarScratch struct {
	gScore   map[types.Node]float64
	cameFrom map[types.Node]types.Link
	pq       minLatencyHeap
}

var astarScratchPool = sync.Pool{
	New: func() any {
		return &astarScratch{
			gScore:   make(map[types.Node]float64, 256),
			cameFrom: make(map[types.Node]types.Link, 256),
			pq:       make(minLatencyHeap, 0, 128),
		}
	},
}

// AStarRouter implements shortest-path routing with per-tick Shortest-Path Tree (SPT) caching
// on Ground Stations and zero-retention pooled A* search for Satellite-to-Satellite offloading.
// This prevents retaining 6,469 full-constellation routing maps (3.44 GB RAM) on satellites.
type AStarRouter struct {
	self types.Node

	mu         sync.RWMutex
	lastPos    types.Vector
	treeValid  bool
	gScore     map[types.Node]float64
	cameFrom   map[types.Node]types.Link
	routeCache map[types.Node]types.RouteResult
}

// NewAStarRouter creates a new AStarRouter instance.
func NewAStarRouter() *AStarRouter {
	return &AStarRouter{}
}

// Mount binds the router to a node.
func (r *AStarRouter) Mount(n types.Node) error {
	if r.self != nil {
		return errors.New("router already mounted")
	}
	r.self = n
	return nil
}

func (r *AStarRouter) CanPreRouteCalc() bool { return false }
func (r *AStarRouter) CanOnRouteCalc() bool  { return true }

// CalculateRoutingTable invalidates the per-tick SPT cache when topology changes.
func (r *AStarRouter) CalculateRoutingTable() error {
	r.mu.Lock()
	r.treeValid = false
	if r.routeCache != nil {
		clear(r.routeCache)
	}
	r.mu.Unlock()
	return nil
}

func (r *AStarRouter) AdvertiseNewServiceAsync(serviceName string) error {
	return nil
}

func (r *AStarRouter) ReceiveServiceAdvertismentsAsync(serviceName string, outlink types.Link, route types.RouteResult) error {
	return nil
}

// RouteToService finds the nearest node that hosts the service and routes to it.
func (r *AStarRouter) RouteToService(serviceName string) (types.RouteResult, error) {
	if r.self == nil {
		return nil, errors.New("router not mounted")
	}

	var candidates []types.Node
	for _, n := range r.getNeighbourhood() {
		if n.GetComputing().HostsService(serviceName) {
			candidates = append(candidates, n)
		}
	}

	if len(candidates) == 0 {
		return UnreachableRouteResultInstance, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return r.self.DistanceTo(candidates[i]) < r.self.DistanceTo(candidates[j])
	})
	return r.RouteTo(candidates[0])
}

// RouteToNode routes to a specific target node.
func (r *AStarRouter) RouteToNode(target types.Node) (types.RouteResult, error) {
	if r.self == nil {
		return nil, errors.New("router not mounted")
	}
	return r.RouteTo(target)
}

// ensureSPT builds the single-source shortest-path tree (SPT) from r.self once per tick,
// reusing map memory in-place via clear() so zero map bucket allocations occur after warmup.
func (r *AStarRouter) ensureSPT(currPos types.Vector) {
	r.mu.RLock()
	if r.treeValid && r.lastPos == currPos {
		r.mu.RUnlock()
		return
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.treeValid && r.lastPos == currPos {
		return
	}

	if r.gScore == nil {
		r.gScore = make(map[types.Node]float64, 1024)
		r.cameFrom = make(map[types.Node]types.Link, 1024)
		r.routeCache = make(map[types.Node]types.RouteResult, 128)
	} else {
		clear(r.gScore)
		clear(r.cameFrom)
		clear(r.routeCache)
	}

	scratch := astarScratchPool.Get().(*astarScratch)
	pq := scratch.pq[:0]

	r.gScore[r.self] = 0
	heap.Push(&pq, dijkstraNodeItem{node: r.self, gCost: 0, fCost: 0})

	for pq.Len() > 0 {
		item := heap.Pop(&pq).(dijkstraNodeItem)
		current := item.node

		if item.gCost > r.gScore[current] {
			continue
		}

		for _, l := range current.GetLinkNodeProtocol().Established() {
			neighbor := l.GetOther(current)
			if neighbor == nil {
				continue
			}
			alt := item.gCost + l.Latency()
			if prev, visited := r.gScore[neighbor]; !visited || alt < prev {
				r.gScore[neighbor] = alt
				r.cameFrom[neighbor] = l
				heap.Push(&pq, dijkstraNodeItem{node: neighbor, gCost: alt, fCost: alt})
			}
		}
	}

	scratch.pq = pq[:0]
	astarScratchPool.Put(scratch)

	r.lastPos = currPos
	r.treeValid = true
}

// extractReverseRouteFromTargetSPT extracts the reverse shortest path from target's SPT.
// If target is a GroundStation, it ensures target's SPT is built first so that Satellites
// transmitting task completion results in Computing.Tick never build SPTs on themselves.
func (r *AStarRouter) extractReverseRouteFromTargetSPT(target types.Node) (types.RouteResult, bool) {
	targetRouter, ok := target.GetRouter().(*AStarRouter)
	if !ok || targetRouter == nil {
		return nil, false
	}

	targetPos := target.GetPosition()
	if _, isTargetGS := target.(types.GroundStation); isTargetGS {
		targetRouter.ensureSPT(targetPos)
	}

	targetRouter.mu.RLock()
	defer targetRouter.mu.RUnlock()

	if !targetRouter.treeValid || targetRouter.lastPos != targetPos {
		return nil, false
	}

	dist, reachable := targetRouter.gScore[r.self]
	if !reachable {
		return UnreachableRouteResultInstance, true
	}

	var path []types.Link
	curr := r.self
	for curr != target {
		link, exists := targetRouter.cameFrom[curr]
		if !exists || link == nil {
			return UnreachableRouteResultInstance, true
		}
		path = append(path, link)
		curr = link.GetOther(curr)
	}

	return NewOnRouteResult(int(dist), len(path), path), true
}

// routeTransientAStar executes a targeted, early-exit A* search from r.self to target
// using pooled scratch memory so Satellites retain 0 bytes of routing state between calls.
func (r *AStarRouter) routeTransientAStar(target types.Node) (types.RouteResult, error) {
	scratch := astarScratchPool.Get().(*astarScratch)
	gScore := scratch.gScore
	cameFrom := scratch.cameFrom
	pq := scratch.pq[:0]

	clear(gScore)
	clear(cameFrom)

	gScore[r.self] = 0
	heap.Push(&pq, dijkstraNodeItem{
		node:  r.self,
		gCost: 0,
		fCost: heuristic(r.self, target),
	})

	found := false
	var finalDist float64

	for pq.Len() > 0 {
		item := heap.Pop(&pq).(dijkstraNodeItem)
		current := item.node

		if current == target {
			found = true
			finalDist = item.gCost
			break
		}

		if item.gCost > gScore[current] {
			continue
		}

		for _, l := range current.GetLinkNodeProtocol().Established() {
			neighbor := l.GetOther(current)
			if neighbor == nil {
				continue
			}
			alt := item.gCost + l.Latency()
			if prev, visited := gScore[neighbor]; !visited || alt < prev {
				gScore[neighbor] = alt
				cameFrom[neighbor] = l
				heap.Push(&pq, dijkstraNodeItem{
					node:  neighbor,
					gCost: alt,
					fCost: alt + heuristic(neighbor, target),
				})
			}
		}
	}

	var res types.RouteResult = UnreachableRouteResultInstance
	if found {
		var path []types.Link
		curr := target
		valid := true
		for curr != r.self {
			link := cameFrom[curr]
			if link == nil {
				valid = false
				break
			}
			path = append(path, link)
			curr = link.GetOther(curr)
		}
		if valid {
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			res = NewOnRouteResult(int(finalDist), len(path), path)
		}
	}

	clear(gScore)
	clear(cameFrom)
	scratch.pq = pq[:0]
	astarScratchPool.Put(scratch)

	return res, nil
}

// RouteTo resolves the shortest route from r.self to target.
func (r *AStarRouter) RouteTo(target types.Node) (types.RouteResult, error) {
	if r.self == nil {
		return nil, errors.New("router not mounted")
	}
	if r.self == target {
		return NewOnRouteResult(0, 0, nil), nil
	}

	// 1. Symmetry optimization: if target is a GroundStation (or already built its SPT this tick),
	// extract the reverse route in O(hops) without building an SPT on this node.
	if revRoute, found := r.extractReverseRouteFromTargetSPT(target); found {
		return revRoute, nil
	}

	// 2. If r.self is a Satellite routing to another Satellite (e.g. proactive offloading),
	// run pooled transient A* so the 6,469 Satellites never retain 6,554-node maps in RAM.
	if _, isSelfGS := r.self.(types.GroundStation); !isSelfGS {
		return r.routeTransientAStar(target)
	}

	currPos := r.self.GetPosition()

	// 3. Check local Ground Station route cache
	r.mu.RLock()
	if r.treeValid && r.lastPos == currPos {
		if cached, ok := r.routeCache[target]; ok {
			r.mu.RUnlock()
			return cached, nil
		}
	}
	r.mu.RUnlock()

	// 4. Ensure Ground Station SPT is built for this tick
	r.ensureSPT(currPos)

	r.mu.Lock()
	defer r.mu.Unlock()

	if cached, ok := r.routeCache[target]; ok {
		return cached, nil
	}

	dist, reachable := r.gScore[target]
	if !reachable {
		r.routeCache[target] = UnreachableRouteResultInstance
		return UnreachableRouteResultInstance, nil
	}

	var path []types.Link
	curr := target
	for curr != r.self {
		link := r.cameFrom[curr]
		if link == nil {
			r.routeCache[target] = UnreachableRouteResultInstance
			return UnreachableRouteResultInstance, nil
		}
		path = append(path, link)
		curr = link.GetOther(curr)
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	res := NewOnRouteResult(int(dist), len(path), path)
	r.routeCache[target] = res
	return res, nil
}

// getNeighbourhood returns all nodes connected to the current node (BFS).
func (r *AStarRouter) getNeighbourhood() []types.Node {
	visited := map[types.Node]bool{}
	queue := []types.Node{r.self}
	var result []types.Node

	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if visited[n] {
			continue
		}
		visited[n] = true
		result = append(result, n)
		for _, l := range n.GetLinkNodeProtocol().Established() {
			other := l.GetOther(n)
			if !visited[other] {
				queue = append(queue, other)
			}
		}
	}
	return result
}

// heuristic estimates the minimum latency from node a to node b (in ms).
func heuristic(a, b types.Node) float64 {
	d := a.DistanceTo(b)
	return d / configs.SpeedOfLight * 1000
}
