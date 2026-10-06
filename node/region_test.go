package node

import (
	"sync"
	"testing"

	meshcore "github.com/OwlShack/meshcore-go"
)

func testRegion(name string) *meshcore.Region {
	return meshcore.NewRegionFromHashtag(name)
}

func makeTransportFloodPacket(region *meshcore.Region, payload []byte) *meshcore.Packet {
	pkt := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeTransportFlood, meshcore.PayloadTypeTxtMsg, 0),
		PathLength: 0x00,
		Payload:    payload,
	}
	pkt.TransportCode1 = region.CalcTransportCode(pkt)
	return pkt
}

func makeNonTransportFloodPacket(payload []byte) *meshcore.Packet {
	return &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeTxtMsg, 0),
		PathLength: 0x00,
		Payload:    payload,
	}
}

func TestRegionMap_NewDefaults(t *testing.T) {
	rm := NewRegionMap()
	if rm.Len() != 0 {
		t.Errorf("new RegionMap Len() = %d, want 0", rm.Len())
	}
	w := rm.Wildcard()
	if w.Name != "*" {
		t.Errorf("wildcard Name = %q, want %q", w.Name, "*")
	}
	if w.Flags != 0 {
		t.Errorf("wildcard Flags = %d, want 0", w.Flags)
	}
}

func TestRegionMap_AddAndGet(t *testing.T) {
	rm := NewRegionMap()
	r := testRegion("ch-fr")

	if !rm.Add(r) {
		t.Fatal("Add returned false")
	}
	if rm.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", rm.Len())
	}

	got := rm.Get("#ch-fr")
	if got != r {
		t.Error("Get returned different pointer")
	}
}

func TestRegionMap_AddAutoID(t *testing.T) {
	rm := NewRegionMap()
	r1 := testRegion("alpha")
	r2 := testRegion("beta")

	rm.Add(r1)
	rm.Add(r2)

	if r1.ID == 0 {
		t.Error("first region should have non-zero ID")
	}
	if r2.ID == 0 {
		t.Error("second region should have non-zero ID")
	}
	if r1.ID == r2.ID {
		t.Errorf("IDs should be unique: both got %d", r1.ID)
	}
}

func TestRegionMap_AddPresetID(t *testing.T) {
	rm := NewRegionMap()
	r := testRegion("preset")
	r.ID = 42

	rm.Add(r)
	if r.ID != 42 {
		t.Errorf("preset ID changed to %d", r.ID)
	}
}

func TestRegionMap_AddMaxCapacity(t *testing.T) {
	rm := NewRegionMap()
	for i := range meshcore.MaxRegions {
		r := testRegion("r")
		r.Name = string(rune('A' + i))
		if !rm.Add(r) {
			t.Fatalf("Add(%d) returned false before max", i)
		}
	}
	if rm.Len() != meshcore.MaxRegions {
		t.Fatalf("Len() = %d, want %d", rm.Len(), meshcore.MaxRegions)
	}

	extra := testRegion("overflow")
	if rm.Add(extra) {
		t.Error("Add beyond MaxRegions should return false")
	}
	if rm.Len() != meshcore.MaxRegions {
		t.Errorf("Len() = %d after rejected add, want %d", rm.Len(), meshcore.MaxRegions)
	}
}

func TestRegionMap_Remove(t *testing.T) {
	rm := NewRegionMap()
	r := testRegion("remove-me")
	rm.Add(r)

	if !rm.Remove("#remove-me") {
		t.Error("Remove returned false for existing region")
	}
	if rm.Len() != 0 {
		t.Errorf("Len() = %d after remove, want 0", rm.Len())
	}
	if rm.Get("#remove-me") != nil {
		t.Error("Get should return nil after remove")
	}
}

func TestRegionMap_RemoveMiss(t *testing.T) {
	rm := NewRegionMap()
	rm.Add(testRegion("exists"))

	if rm.Remove("nope") {
		t.Error("Remove should return false for non-existent region")
	}
	if rm.Len() != 1 {
		t.Error("Len should be unchanged after failed remove")
	}
}

func TestRegionMap_GetMiss(t *testing.T) {
	rm := NewRegionMap()
	if rm.Get("missing") != nil {
		t.Error("Get should return nil for non-existent region")
	}
}

func TestRegionMap_All(t *testing.T) {
	rm := NewRegionMap()
	rm.Add(testRegion("a"))
	rm.Add(testRegion("b"))
	rm.Add(testRegion("c"))

	all := rm.All()
	if len(all) != 3 {
		t.Fatalf("All() returned %d, want 3", len(all))
	}
}

func TestRegionMap_AllEmpty(t *testing.T) {
	rm := NewRegionMap()
	all := rm.All()
	if len(all) != 0 {
		t.Errorf("All() returned %d, want 0", len(all))
	}
}

func TestRegionMap_AllReturnsCopy(t *testing.T) {
	rm := NewRegionMap()
	rm.Add(testRegion("x"))

	all := rm.All()
	all[0] = nil

	if rm.Get("#x") == nil {
		t.Error("mutating All() result should not affect the map")
	}
}

func TestRegionMap_SetWildcardFlags(t *testing.T) {
	rm := NewRegionMap()
	rm.SetWildcardFlags(meshcore.RegionDenyFlood | meshcore.RegionDenyDirect)

	w := rm.Wildcard()
	if w.Flags != meshcore.RegionDenyFlood|meshcore.RegionDenyDirect {
		t.Errorf("wildcard Flags = 0x%02X, want 0x%02X",
			w.Flags, meshcore.RegionDenyFlood|meshcore.RegionDenyDirect)
	}
}

func TestFindFloodMatch_TransportPacketMatched(t *testing.T) {
	rm := NewRegionMap()
	r := testRegion("ch-fr")
	rm.Add(r)

	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	pkt := makeTransportFloodPacket(r, payload)

	got := rm.FindFloodMatch(pkt)
	if got != r {
		t.Error("FindFloodMatch should return matching region")
	}
}

func TestFindFloodMatch_TransportPacketNoMatch(t *testing.T) {
	rm := NewRegionMap()
	rm.Add(testRegion("us-west"))

	other := testRegion("eu-east")
	pkt := makeTransportFloodPacket(other, []byte{0x01, 0x02})

	got := rm.FindFloodMatch(pkt)
	if got != nil {
		t.Error("FindFloodMatch should return nil when no region matches")
	}
}

func TestFindFloodMatch_TransportPacketDenyFloodSkipped(t *testing.T) {
	rm := NewRegionMap()
	r := testRegion("denied")
	r.Flags = meshcore.RegionDenyFlood
	rm.Add(r)

	pkt := makeTransportFloodPacket(r, []byte{0xAA, 0xBB})

	got := rm.FindFloodMatch(pkt)
	if got != nil {
		t.Error("FindFloodMatch should skip regions with DenyFlood flag")
	}
}

func TestFindFloodMatch_TransportPacketMatchesFirst(t *testing.T) {
	rm := NewRegionMap()
	r1 := testRegion("first")
	r2 := testRegion("second")
	rm.Add(r1)
	rm.Add(r2)

	pkt := makeTransportFloodPacket(r1, []byte{0x01})

	got := rm.FindFloodMatch(pkt)
	if got != r1 {
		t.Error("FindFloodMatch should return first matching region")
	}
}

func TestFindFloodMatch_NonTransportAllowedByWildcard(t *testing.T) {
	rm := NewRegionMap()
	pkt := makeNonTransportFloodPacket([]byte{0x01})

	got := rm.FindFloodMatch(pkt)
	if got == nil {
		t.Fatal("FindFloodMatch should return wildcard for non-transport packet")
	}
	if got.Name != "*" {
		t.Errorf("expected wildcard region, got %q", got.Name)
	}
}

func TestFindFloodMatch_NonTransportDeniedByWildcard(t *testing.T) {
	rm := NewRegionMap()
	rm.SetWildcardFlags(meshcore.RegionDenyFlood)

	pkt := makeNonTransportFloodPacket([]byte{0x01})

	got := rm.FindFloodMatch(pkt)
	if got != nil {
		t.Error("FindFloodMatch should return nil when wildcard denies flood")
	}
}

func TestFindFloodMatch_TransportPacketIgnoresWildcard(t *testing.T) {
	rm := NewRegionMap()
	pkt := makeTransportFloodPacket(testRegion("unknown"), []byte{0x01})

	got := rm.FindFloodMatch(pkt)
	if got != nil {
		t.Error("transport packets should only match named regions, not wildcard")
	}
}

func TestFindFloodMatch_DenyDirectDoesNotBlockFlood(t *testing.T) {
	rm := NewRegionMap()
	r := testRegion("direct-only-deny")
	r.Flags = meshcore.RegionDenyDirect // deny direct but NOT flood
	rm.Add(r)

	pkt := makeTransportFloodPacket(r, []byte{0xCC})

	got := rm.FindFloodMatch(pkt)
	if got != r {
		t.Error("DenyDirect should not prevent flood matching")
	}
}

func TestRegionMap_Concurrent(t *testing.T) {
	rm := NewRegionMap()
	var wg sync.WaitGroup

	for i := range 20 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r := testRegion("concurrent")
			r.Name = string(rune('A' + idx))
			rm.Add(r)
			rm.Get(r.Name)
			rm.All()
			rm.Len()
			rm.Wildcard()
			rm.SetWildcardFlags(0)

			pkt := makeTransportFloodPacket(r, []byte{byte(idx)})
			rm.FindFloodMatch(pkt)

			rm.Remove(r.Name)
		}(i)
	}
	wg.Wait()
}

func TestNode_AddRegion(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xD0), radio)
	defer n.Stop()

	r := testRegion("node-region")
	if !n.AddRegion(r) {
		t.Fatal("AddRegion returned false")
	}
	if n.Region("#node-region") != r {
		t.Error("Region() mismatch")
	}
}

func TestNode_RemoveRegion(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xD1), radio)
	defer n.Stop()

	n.AddRegion(testRegion("rm"))
	if !n.RemoveRegion("#rm") {
		t.Error("RemoveRegion returned false")
	}
	if n.Region("#rm") != nil {
		t.Error("Region should be nil after remove")
	}
}

func TestNode_Regions(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xD2), radio)
	defer n.Stop()

	if n.Regions() == nil {
		t.Fatal("Regions() should not be nil")
	}
	if n.Regions().Len() != 0 {
		t.Errorf("empty node should have 0 regions, got %d", n.Regions().Len())
	}
}

func TestNode_WithRegions(t *testing.T) {
	radio := &mockRadio{}
	r0 := testRegion("init0")
	r1 := testRegion("init1")
	n := New(seedIdentity(0xD3), radio, WithRegions(r0, r1))
	defer n.Stop()

	if n.Regions().Len() != 2 {
		t.Errorf("Regions().Len() = %d, want 2", n.Regions().Len())
	}
	if n.Region("#init0") != r0 {
		t.Error("Region(#init0) not set by WithRegions")
	}
	if n.Region("#init1") != r1 {
		t.Error("Region(#init1) not set by WithRegions")
	}
}

func TestNode_WithRegionsOverflow(t *testing.T) {
	radio := &mockRadio{}
	regions := make([]*meshcore.Region, meshcore.MaxRegions+5)
	for i := range regions {
		r := testRegion("overflow")
		r.Name = string(rune('A' + i))
		regions[i] = r
	}
	n := New(seedIdentity(0xD4), radio, WithRegions(regions...))
	defer n.Stop()

	if n.Regions().Len() != meshcore.MaxRegions {
		t.Errorf("Regions().Len() = %d, want %d", n.Regions().Len(), meshcore.MaxRegions)
	}
}

func TestRegionMap_IsWildcard(t *testing.T) {
	rm := NewRegionMap()

	if !rm.IsWildcard(rm.Wildcard()) {
		t.Error("IsWildcard(Wildcard()) = false, want true")
	}
	w := *rm.Wildcard()
	if !rm.IsWildcard(&w) {
		t.Error("IsWildcard(copy) = false, want true")
	}
	if rm.IsWildcard(&meshcore.Region{Name: "eu"}) {
		t.Error("IsWildcard(normal region) = true, want false")
	}
	if rm.IsWildcard(nil) {
		t.Error("IsWildcard(nil) = true, want false")
	}
}
