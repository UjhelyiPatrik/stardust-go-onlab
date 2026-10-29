package links

import (
	"sync"
	"sync/atomic"

	"github.com/polaris-slo-cloud/stardust-go/pkg/types"
)

var _ types.InterSatelliteLinkProtocol = (*PrecomputedLinkProtocol)(nil)
var _ types.InterSatelliteLinkProtocol = (*NodePrecomputedLinkProtocol)(nil)

// NodePrecomputedLinkProtocol is a per-node, lock-free O(1) link protocol for precomputed mode.
// Each node holds its own pre-filtered slice of established links per simulation step.
type NodePrecomputedLinkProtocol struct {
	node              types.Node
	allLinks          []types.Link
	establishedByStep [][]types.Link
	currentStep       int32
}

// NewNodePrecomputedLinkProtocol initializes a per-node precomputed link protocol for totalSteps steps.
func NewNodePrecomputedLinkProtocol(totalSteps int) *NodePrecomputedLinkProtocol {
	return &NodePrecomputedLinkProtocol{
		allLinks:          make([]types.Link, 0),
		establishedByStep: make([][]types.Link, totalSteps),
		currentStep:       -1,
	}
}

func (p *NodePrecomputedLinkProtocol) Mount(s types.Node) {
	if p.node == nil {
		p.node = s
	}
}

func (p *NodePrecomputedLinkProtocol) GetStepLinksBuffer(stepIdx int, capHint int) []types.Link {
	if stepIdx >= 0 && stepIdx < len(p.establishedByStep) {
		buf := p.establishedByStep[stepIdx]
		if cap(buf) >= capHint {
			return buf[:0]
		}
	}
	return make([]types.Link, 0, capHint)
}

func (p *NodePrecomputedLinkProtocol) SetStepLinks(stepIdx int, stepLinks []types.Link) {
	if stepIdx >= 0 && stepIdx < len(p.establishedByStep) {
		p.establishedByStep[stepIdx] = stepLinks
	}
}

func (p *NodePrecomputedLinkProtocol) SetCurrentStep(stepIdx int) {
	atomic.StoreInt32(&p.currentStep, int32(stepIdx))
}

func (p *NodePrecomputedLinkProtocol) ConnectLink(link types.Link) error {
	return nil
}

func (p *NodePrecomputedLinkProtocol) DisconnectLink(link types.Link) error {
	return nil
}

func (p *NodePrecomputedLinkProtocol) UpdateLinks() ([]types.Link, error) {
	step := int(atomic.LoadInt32(&p.currentStep))
	if step >= 0 && step < len(p.establishedByStep) {
		return p.establishedByStep[step], nil
	}
	return nil, nil
}

func (p *NodePrecomputedLinkProtocol) Established() []types.Link {
	step := int(atomic.LoadInt32(&p.currentStep))
	if step >= 0 && step < len(p.establishedByStep) {
		return p.establishedByStep[step]
	}
	return nil
}

func (p *NodePrecomputedLinkProtocol) Links() []types.Link {
	if len(p.allLinks) > 0 {
		return p.allLinks
	}
	return p.Established()
}

func (p *NodePrecomputedLinkProtocol) AddLink(link types.Link) {
	p.allLinks = append(p.allLinks, link)
}

// PrecomputedLinkProtocol is retained for backward compatibility.
type PrecomputedLinkProtocol struct {
	node        types.Node
	links       []types.Link
	established [][]types.Link
	currentIx   int

	position types.Vector
	mu       sync.Mutex
}

func NewSimulatedLinkProtocol() *PrecomputedLinkProtocol {
	return &PrecomputedLinkProtocol{
		links:     []types.Link{},
		currentIx: -1,
		position:  types.NewVector(-1, -1, -1),
	}
}

func (p *PrecomputedLinkProtocol) InjectEstablishedLinks(links [][]types.Link) {
	p.established = links
}

func (p *PrecomputedLinkProtocol) Mount(s types.Node) {
	if p.node == nil {
		p.node = s
	}
}

func (p *PrecomputedLinkProtocol) ConnectLink(link types.Link) error {
	return nil
}

func (p *PrecomputedLinkProtocol) DisconnectLink(link types.Link) error {
	return nil
}

// UpdateLinks updates the list of active links.
func (p *PrecomputedLinkProtocol) UpdateLinks() ([]types.Link, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.position == p.node.GetPosition() && p.currentIx >= 0 && p.currentIx < len(p.established) {
		return p.established[p.currentIx], nil
	}

	p.currentIx++
	p.position = p.node.GetPosition()
	if p.currentIx >= 0 && p.currentIx < len(p.established) {
		return p.established[p.currentIx], nil
	}
	return nil, nil
}

// Established returns the list of established links.
func (p *PrecomputedLinkProtocol) Established() []types.Link {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.currentIx < 0 || p.currentIx >= len(p.established) {
		return nil
	}
	curr := p.established[p.currentIx]
	out := make([]types.Link, len(curr))
	copy(out, curr)
	return out
}

func (p *PrecomputedLinkProtocol) Links() []types.Link {
	return p.links
}

func (p *PrecomputedLinkProtocol) AddLink(link types.Link) {
	p.links = append(p.links, link)
}
