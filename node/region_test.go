package node

import (
	"sync"
	"testing"
	"time"

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

func TestRegionMap_NameLookupIgnoresHash(t *testing.T) {
	rm := NewRegionMap()
	rm.Add(meshcore.NewRegion("nz"))
	rm.Add(meshcore.NewRegion("#au"))

	if rm.Get("#nz") == nil || rm.Get("au") == nil {
		t.Fatal("Get should ignore a leading '#'")
	}
	if !rm.Remove("#nz") || rm.Get("nz") != nil {
		t.Fatal("Remove should ignore a leading '#'")
	}
}

func TestRegionMap_Default(t *testing.T) {
	rm := NewRegionMap()
	nz := meshcore.NewRegion("nz")
	rm.Add(nz)
	rm.Add(meshcore.NewRegion("$private"))

	if rm.Default() != nil {
		t.Fatal("Default() with nothing set should be nil")
	}
	rm.SetDefault("missing")
	if rm.Default() != nil {
		t.Fatal("Default() naming an unknown region should be nil")
	}
	rm.SetDefault("$private")
	if rm.Default() != nil {
		t.Fatal("Default() naming a keyless region should be nil")
	}
	rm.SetDefault("#nz")
	if rm.Default() != nz {
		t.Fatal("Default() should resolve the named region")
	}
	rm.Remove("nz")
	if rm.Default() != nil {
		t.Fatal("Default() should be nil once its region is removed")
	}
}

func TestRegionMap_ReplyScope(t *testing.T) {
	nz := meshcore.NewRegion("nz")
	au := meshcore.NewRegion("au")
	payload := []byte{0x01, 0x02}

	unscoped := makeNonTransportFloodPacket(payload)
	direct := &meshcore.Packet{Header: meshcore.MakeHeader(meshcore.RouteTypeDirect, meshcore.PayloadTypeTxtMsg, 0), Payload: payload}
	transportDirect := makeTransportFloodPacket(nz, payload)
	transportDirect.Header = meshcore.MakeHeader(meshcore.RouteTypeTransportDirect, meshcore.PayloadTypeTxtMsg, 0)

	cases := []struct {
		name         string
		req          *meshcore.Packet
		denyWildcard bool
		want         *meshcore.Region
	}{
		{"request scope reused", makeTransportFloodPacket(nz, payload), false, nz},
		{"unknown scope falls back to default", makeTransportFloodPacket(meshcore.NewRegion("fr"), payload), false, au},
		{"allowed unscoped flood stays unscoped", unscoped, false, nil},
		{"denied unscoped flood falls back to default", unscoped, true, au},
		{"direct falls back to default", direct, false, au},
		{"transport direct falls back to default", transportDirect, false, au},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rm := NewRegionMap()
			rm.Add(nz)
			rm.Add(au)
			rm.SetDefault("au")
			if c.denyWildcard {
				rm.SetWildcardFlags(meshcore.RegionDenyFlood)
			}
			if got := rm.ReplyScope(c.req); got != c.want {
				t.Errorf("ReplyScope() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestRouter_FloodRelayNeedsRegion(t *testing.T) {
	nz := meshcore.NewRegion("nz")
	payload := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE}
	cases := []struct {
		name         string
		pkt          *meshcore.Packet
		denyWildcard bool
		relay        bool
	}{
		{"known scope relayed", makeTransportFloodPacket(nz, payload), false, true},
		{"unknown scope dropped", makeTransportFloodPacket(meshcore.NewRegion("fr"), payload), false, false},
		{"unscoped relayed by default", makeNonTransportFloodPacket(payload), false, true},
		{"unscoped dropped when wildcard denies flood", makeNonTransportFloodPacket(payload), true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var sent [][]byte
			r := newTestRouter(testRouterOpts{
				identity:     seedIdentity(0x01),
				allowForward: func(*meshcore.Packet) bool { return true },
				send: func(data []byte, _ uint8) error {
					sent = append(sent, append([]byte(nil), data...))
					return nil
				},
			})
			r.node.regions.Add(nz)
			if c.denyWildcard {
				r.node.regions.SetWildcardFlags(meshcore.RegionDenyFlood)
			}

			routeThenRelay(r, c.pkt)
			if got := len(sent) == 1; got != c.relay {
				t.Fatalf("relayed = %v, want %v", got, c.relay)
			}
			if c.relay && c.pkt.IsTransport() {
				out := mustPacketFromBytes(t, sent[0])
				if !nz.MatchesPacket(out) {
					t.Errorf("relayed code = %04x, want the region's code kept", out.TransportCode1)
				}
			}
		})
	}
}

func TestNode_DefaultRegionScopesOwnFloods(t *testing.T) {
	nz := meshcore.NewRegion("nz")
	radio := &mockRadio{}
	ch := testChannel("scope-test")
	n := New(seedIdentity(0x70), radio,
		WithRegions(nz),
		WithDefaultRegion("nz"),
		WithChannels(ch),
		WithAdvertData(meshcore.AdvertAppData{Type: "CHAT", Name: "scoped"}),
		WithAdvertInterval(time.Hour),
	)
	defer n.Stop()

	peer := seedIdentity(0x71).Identity
	if err := n.SendGroupText(ch, testGroupPayload("hi"), 1, time.Second, 0, nil); err != nil {
		t.Fatal(err)
	}
	if err := n.SendTextMessage(peer, []byte("flood"), 0, time.Now(), nil, 1, time.Second, nil); err != nil {
		t.Fatal(err)
	}
	if err := n.SendTextMessage(peer, []byte("direct"), 0, time.Now(), []byte{}, 1, time.Second, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	var scoped, direct int
	for _, data := range radio.sentData() {
		pkt := mustPacketFromBytes(t, data)
		switch {
		case pkt.IsRouteDirect():
			direct++
			if pkt.IsTransport() {
				t.Errorf("direct %s was given transport codes", pkt.PayloadTypeString())
			}
		case nz.MatchesPacket(pkt):
			scoped++
		default:
			t.Errorf("%s flood sent unscoped, want scoped to nz", pkt.PayloadTypeString())
		}
	}
	if scoped != 3 || direct != 1 {
		t.Fatalf("sent %d scoped floods and %d direct, want 3 and 1", scoped, direct)
	}
}

func TestNode_ScopedSendsIgnoreDefault(t *testing.T) {
	nz := meshcore.NewRegion("nz")
	au := meshcore.NewRegion("au")
	ch := testChannel("scope-test")
	peer := seedIdentity(0x73).Identity

	cases := []struct {
		name  string
		scope *meshcore.Region
	}{
		{"explicit region", au},
		{"nil is unscoped", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			radio := &mockRadio{}
			n := New(seedIdentity(0x72), radio, WithRegions(nz, au), WithDefaultRegion("nz"), WithChannels(ch))
			defer n.Stop()

			if err := n.SendGroupTextScoped(ch, c.scope, testGroupPayload("hi"), 1, time.Second, 0, nil); err != nil {
				t.Fatal(err)
			}
			if err := n.SendTextMessageScoped(peer, c.scope, []byte("dm"), 0, time.Now(), nil, 1, time.Second, nil); err != nil {
				t.Fatal(err)
			}
			time.Sleep(200 * time.Millisecond)

			sent := radio.sentData()
			if len(sent) != 2 {
				t.Fatalf("sent %d packets, want 2", len(sent))
			}
			for _, data := range sent {
				pkt := mustPacketFromBytes(t, data)
				if nz.MatchesPacket(pkt) {
					t.Errorf("%s used the node default", pkt.PayloadTypeString())
				}
				if c.scope == nil && pkt.IsTransport() {
					t.Errorf("%s scoped, want unscoped", pkt.PayloadTypeString())
				}
				if c.scope != nil && !c.scope.MatchesPacket(pkt) {
					t.Errorf("%s not scoped to %s", pkt.PayloadTypeString(), c.scope.Name)
				}
			}
		})
	}
}
