package node

import (
	"sync"

	meshcore "github.com/OwlShack/meshcore-go"
)

// RegionMap is a thread-safe set of regions stored by pointer; treat a Region as immutable once added.
type RegionMap struct {
	mu       sync.RWMutex
	regions  []*meshcore.Region
	wildcard meshcore.Region
	nextID   uint16
}

func NewRegionMap() *RegionMap {
	return &RegionMap{
		wildcard: meshcore.Region{
			Name: "*",
		},
		nextID: 1,
	}
}

// Wildcard returns a copy of the wildcard region.
func (rm *RegionMap) Wildcard() *meshcore.Region {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	w := rm.wildcard
	return &w
}

// IsWildcard reports whether r is the wildcard region, compared by name.
func (rm *RegionMap) IsWildcard(r *meshcore.Region) bool {
	if r == nil {
		return false
	}
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return r.Name == rm.wildcard.Name
}

func (rm *RegionMap) SetWildcardFlags(flags uint8) {
	rm.mu.Lock()
	rm.wildcard.Flags = flags
	rm.mu.Unlock()
}

func (rm *RegionMap) Add(r *meshcore.Region) bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if len(rm.regions) >= meshcore.MaxRegions {
		return false
	}
	if r.ID == 0 {
		r.ID = rm.nextID
		rm.nextID++
	}
	rm.regions = append(rm.regions, r)
	return true
}

func (rm *RegionMap) Remove(name string) bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	for i, r := range rm.regions {
		if r.Name == name {
			rm.regions = append(rm.regions[:i], rm.regions[i+1:]...)
			return true
		}
	}
	return false
}

func (rm *RegionMap) Get(name string) *meshcore.Region {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	for _, r := range rm.regions {
		if r.Name == name {
			return r
		}
	}
	return nil
}

func (rm *RegionMap) All() []*meshcore.Region {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	out := make([]*meshcore.Region, len(rm.regions))
	copy(out, rm.regions)
	return out
}

func (rm *RegionMap) Len() int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return len(rm.regions)
}

// FindFloodMatch returns the region that matches a flood packet, or nil.
func (rm *RegionMap) FindFloodMatch(pkt *meshcore.Packet) *meshcore.Region {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if pkt.IsTransport() {
		for _, r := range rm.regions {
			if r.Flags&meshcore.RegionDenyFlood != 0 {
				continue
			}
			if r.MatchesPacket(pkt) {
				return r
			}
		}
		return nil
	}

	if rm.wildcard.Flags&meshcore.RegionDenyFlood != 0 {
		return nil
	}
	w := rm.wildcard
	return &w
}
