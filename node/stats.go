package node

import "sync/atomic"

// RouteStats is a snapshot of a node's routing counters.
type RouteStats struct {
	FloodReceived    uint64 // flood-routed packets received
	DirectReceived   uint64 // direct-routed packets received
	FloodDuplicates  uint64 // flood packets dropped by the dedup cache
	DirectDuplicates uint64 // direct packets dropped by the dedup cache
	FloodRelays      uint64 // packets re-flooded
	DirectRelays     uint64 // packets relayed to a next hop
	Delivered        uint64 // packets handed to local handlers
	RxAirtimeMs      uint64 // estimated airtime of received packets; zero without an airtime estimator
}

type routeCounters struct {
	floodReceived    atomic.Uint64
	directReceived   atomic.Uint64
	floodDuplicates  atomic.Uint64
	directDuplicates atomic.Uint64
	floodRelays      atomic.Uint64
	directRelays     atomic.Uint64
	delivered        atomic.Uint64
	rxAirtimeMs      atomic.Uint64
}

// RouteStats returns a snapshot of this node's routing counters.
func (n *Node) RouteStats() RouteStats {
	c := &n.router.stats
	return RouteStats{
		FloodReceived:    c.floodReceived.Load(),
		DirectReceived:   c.directReceived.Load(),
		FloodDuplicates:  c.floodDuplicates.Load(),
		DirectDuplicates: c.directDuplicates.Load(),
		FloodRelays:      c.floodRelays.Load(),
		DirectRelays:     c.directRelays.Load(),
		Delivered:        c.delivered.Load(),
		RxAirtimeMs:      c.rxAirtimeMs.Load(),
	}
}
