package links

import (
	"errors"
	"log"
	"sync"

	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.InterSatelliteLinkProtocol = (*LinkFilterProtocol)(nil)

// LinkFilterProtocol wraps an InterSatelliteLinkProtocol and retains only links involving the mounted node.
// Uses O(1) adjacency lookup when wrapping NodeFilteredLinkProvider (e.g., IslMstProtocol)
// and maintains a flat slice for zero-allocation Established() reads during pathfinding.
type LinkFilterProtocol struct {
	inner            types.InterSatelliteLinkProtocol
	node             types.Node
	links            []types.Link
	innerEstablished []types.Link
	externalLinks    []types.Link // Links connected externally (e.g., GroundLinks)
	combinedCache    []types.Link
	mu               sync.RWMutex
}

// NewLinkFilterProtocol initializes the filter protocol
func NewLinkFilterProtocol(inner types.InterSatelliteLinkProtocol) *LinkFilterProtocol {
	return &LinkFilterProtocol{
		inner:            inner,
		links:            make([]types.Link, 0, 16),
		innerEstablished: make([]types.Link, 0, 4),
		externalLinks:    make([]types.Link, 0, 2),
		combinedCache:    make([]types.Link, 0, 6),
	}
}

func (p *LinkFilterProtocol) Mount(s types.Node) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.node != nil {
		log.Fatalln("LinkFilterProtocol should not be mounted to a node twice")
	}
	p.node = s
	p.inner.Mount(s)
}

func (p *LinkFilterProtocol) SupportsLazyLinks() bool {
	if lazy, ok := p.inner.(interface{ SupportsLazyLinks() bool }); ok {
		return lazy.SupportsLazyLinks()
	}
	return false
}

func (p *LinkFilterProtocol) AddLink(link types.Link) {
	n1, n2 := link.Nodes()
	if n1 == p.node || n2 == p.node {
		p.mu.Lock()
		p.links = append(p.links, link)
		p.mu.Unlock()
	}
	// Forward to shared inner protocol only from the first endpoint to prevent duplicate link registration
	if n1 == p.node || p.node == nil {
		p.inner.AddLink(link)
	}
}

func (p *LinkFilterProtocol) rebuildCombinedLocked() {
	total := len(p.innerEstablished) + len(p.externalLinks)
	if cap(p.combinedCache) < total {
		p.combinedCache = make([]types.Link, total)
	} else {
		p.combinedCache = p.combinedCache[:total]
	}
	n := copy(p.combinedCache, p.innerEstablished)
	copy(p.combinedCache[n:], p.externalLinks)
}

func (p *LinkFilterProtocol) ConnectLink(link types.Link) error {
	p.mu.Lock()
	alreadyExists := false
	for _, l := range p.externalLinks {
		if l == link {
			alreadyExists = true
			break
		}
	}
	if !alreadyExists {
		p.externalLinks = append(p.externalLinks, link)
		p.rebuildCombinedLocked()
	}
	p.mu.Unlock()
	return p.inner.ConnectLink(link)
}

func (p *LinkFilterProtocol) DisconnectLink(link types.Link) error {
	p.mu.Lock()
	for i, l := range p.externalLinks {
		if l == link {
			p.externalLinks = append(p.externalLinks[:i], p.externalLinks[i+1:]...)
			p.rebuildCombinedLocked()
			break
		}
	}
	p.mu.Unlock()
	return p.inner.DisconnectLink(link)
}

func (p *LinkFilterProtocol) UpdateLinks() ([]types.Link, error) {
	if p.node == nil {
		return nil, errors.New("not mounted")
	}

	all, err := p.inner.UpdateLinks()
	if err != nil {
		return nil, err
	}

	var filtered []types.Link
	if provider, ok := p.inner.(NodeFilteredLinkProvider); ok {
		if nodeLinks, found := provider.GetEstablishedForNode(p.node); found {
			filtered = nodeLinks
		}
	} else {
		filtered = make([]types.Link, 0, 4)
		for _, link := range all {
			if involves(link, p.node) {
				filtered = append(filtered, link)
			}
		}
	}

	p.mu.Lock()
	p.innerEstablished = filtered
	p.rebuildCombinedLocked()
	res := p.combinedCache
	p.mu.Unlock()

	return res, nil
}

func (p *LinkFilterProtocol) Links() []types.Link {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.links
}

func (p *LinkFilterProtocol) Established() []types.Link {
	p.mu.RLock()
	res := p.combinedCache
	p.mu.RUnlock()
	return res
}

func involves(link types.Link, node types.Node) bool {
	n1, n2 := link.Nodes()
	return n1 == node || n2 == node
}
