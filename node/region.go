package node

import (
	"strings"
	"sync"

	meshcore "github.com/OwlShack/meshcore-go"
)

// RegionMap is a thread-safe set of regions stored by pointer; treat a Region as immutable once added.
type RegionMap struct {
	mu       sync.RWMutex
	regions  []*meshcore.Region
	wildcard meshcore.Region
	nextID   uint16
	def      string
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
		if sameRegionName(r.Name, name) {
			rm.regions = append(rm.regions[:i], rm.regions[i+1:]...)
			return true
		}
	}
	return false
}

// Get finds a region by name, ignoring a leading '#'.
func (rm *RegionMap) Get(name string) *meshcore.Region {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.get(name)
}

func (rm *RegionMap) get(name string) *meshcore.Region {
	for _, r := range rm.regions {
		if sameRegionName(r.Name, name) {
			return r
		}
	}
	return nil
}

func sameRegionName(a, b string) bool {
	return strings.TrimPrefix(a, "#") == strings.TrimPrefix(b, "#")
}

// SetDefault names the region the node's own floods are scoped to; "" clears it.
func (rm *RegionMap) SetDefault(name string) {
	rm.mu.Lock()
	rm.def = name
	rm.mu.Unlock()
}

// Default returns the default region, or nil when none is set or it has no key.
func (rm *RegionMap) Default() *meshcore.Region {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	if r := rm.get(rm.def); rm.def != "" && r != nil && !r.Key.IsZero() {
		return r
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

// ReplyScope returns req's own region, nil for an allowed unscoped flood, otherwise Default.
func (rm *RegionMap) ReplyScope(req *meshcore.Packet) *meshcore.Region {
	if req.IsRouteFlood() {
		if r := rm.FindFloodMatch(req); r != nil {
			if rm.IsWildcard(r) {
				return nil
			}
			return r
		}
	}
	return rm.Default()
}
