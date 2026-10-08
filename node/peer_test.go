package node

import (
	"bytes"
	"crypto/ed25519"
	"testing"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

func makeSignedAdvert(id meshcore.LocalIdentity, timestamp uint32, name string) *meshcore.Advert {
	appData := &meshcore.AdvertAppData{Type: "CHAT", Name: name}
	raw, err := appData.ToBytes()
	if err != nil {
		panic(err)
	}
	adv := &meshcore.Advert{
		PublicKey:  id.Identity,
		Timestamp:  timestamp,
		RawAppData: raw,
	}
	adv.Sign(id.PrivateKey())

	b, err := adv.ToBytes()
	if err != nil {
		panic(err)
	}
	parsed, err := meshcore.AdvertFromBytes(b)
	if err != nil {
		panic(err)
	}
	return parsed
}

func peerIdentity(seedByte byte) meshcore.LocalIdentity {
	var seed [ed25519.SeedSize]byte
	seed[0] = seedByte
	return meshcore.NewLocalIdentityFromSeed(seed)
}

func TestPeerTable_UpdateAndLookup(t *testing.T) {
	pt := NewPeerTable(10)
	id := peerIdentity(0x01)
	adv := makeSignedAdvert(id, 100, "alice")

	if ok := pt.Update(adv, -5, -80, false, nil); !ok {
		t.Fatal("Update returned false for new peer")
	}
	if pt.Count() != 1 {
		t.Fatalf("Count = %d, want 1", pt.Count())
	}

	p := pt.Lookup(id.PublicKey())
	if p == nil {
		t.Fatal("Lookup returned nil")
	}
	if p.Name != "alice" {
		t.Errorf("Name = %q, want %q", p.Name, "alice")
	}
	if p.Type != "CHAT" {
		t.Errorf("Type = %q, want %q", p.Type, "CHAT")
	}
	if p.SNR != -5 {
		t.Errorf("SNR = %g, want -5", p.SNR)
	}
	if p.RSSI != -80 {
		t.Errorf("RSSI = %d, want -80", p.RSSI)
	}
	if p.LastAdvertTimestamp != 100 {
		t.Errorf("LastAdvertTimestamp = %d, want 100", p.LastAdvertTimestamp)
	}
}

func TestPeerTable_UpdateExisting(t *testing.T) {
	pt := NewPeerTable(10)
	id := peerIdentity(0x02)

	pt.Update(makeSignedAdvert(id, 100, "bob"), -5, -80, false, nil)
	pt.Update(makeSignedAdvert(id, 200, "bob2"), -3, -70, false, nil)

	if pt.Count() != 1 {
		t.Fatalf("Count = %d, want 1", pt.Count())
	}
	p := pt.Lookup(id.PublicKey())
	if p.Name != "bob2" {
		t.Errorf("Name = %q, want %q", p.Name, "bob2")
	}
	if p.LastAdvertTimestamp != 200 {
		t.Errorf("LastAdvertTimestamp = %d, want 200", p.LastAdvertTimestamp)
	}
	if p.SNR != -3 {
		t.Errorf("SNR = %g, want -3", p.SNR)
	}
}

func TestPeerTable_ReplayProtection(t *testing.T) {
	pt := NewPeerTable(10)
	id := peerIdentity(0x03)

	pt.Update(makeSignedAdvert(id, 100, "carol"), 0, 0, false, nil)

	if ok := pt.Update(makeSignedAdvert(id, 100, "carol-replay"), 0, 0, false, nil); ok {
		t.Fatal("Update should reject equal timestamp (replay)")
	}
	if ok := pt.Update(makeSignedAdvert(id, 50, "carol-old"), 0, 0, false, nil); ok {
		t.Fatal("Update should reject older timestamp")
	}

	p := pt.Lookup(id.PublicKey())
	if p.Name != "carol" {
		t.Errorf("Name = %q, want %q (unchanged)", p.Name, "carol")
	}
}

func TestPeerTable_LookupByHash(t *testing.T) {
	pt := NewPeerTable(10)
	id := peerIdentity(0x04)
	pt.Update(makeSignedAdvert(id, 100, "dave"), 0, 0, false, nil)

	hash := id.Hash()
	peers := pt.LookupByHash(hash)
	if len(peers) == 0 {
		t.Fatal("LookupByHash returned no results")
	}
	found := false
	for _, p := range peers {
		if p.Identity.Matches(id.Identity) {
			found = true
		}
	}
	if !found {
		t.Error("expected peer not found in LookupByHash results")
	}
}

func TestPeerTable_LookupByHash_NoMatch(t *testing.T) {
	pt := NewPeerTable(10)
	id := peerIdentity(0x05)
	pt.Update(makeSignedAdvert(id, 100, "eve"), 0, 0, false, nil)

	peers := pt.LookupByHash([]byte{0xFF})
	for _, p := range peers {
		if p.Identity.Matches(id.Identity) {
			t.Error("should not match unrelated hash")
		}
	}
}

func TestPeerTable_Remove(t *testing.T) {
	pt := NewPeerTable(10)
	id := peerIdentity(0x06)
	pt.Update(makeSignedAdvert(id, 100, "frank"), 0, 0, false, nil)

	if !pt.Remove(id.PublicKey()) {
		t.Fatal("Remove returned false for existing peer")
	}
	if pt.Count() != 0 {
		t.Fatalf("Count = %d, want 0", pt.Count())
	}
	if pt.Remove(id.PublicKey()) {
		t.Error("Remove returned true for already-removed peer")
	}
}

func TestPeerTable_Peers(t *testing.T) {
	pt := NewPeerTable(10)
	for i := range byte(5) {
		id := peerIdentity(0x10 + i)
		pt.Update(makeSignedAdvert(id, uint32(100+i), "peer"), 0, 0, false, nil)
	}

	peers := pt.Peers()
	if len(peers) != 5 {
		t.Fatalf("Peers() returned %d, want 5", len(peers))
	}
}

func TestPeerTable_LRUEviction(t *testing.T) {
	pt := NewPeerTable(3)

	ids := make([]meshcore.LocalIdentity, 4)
	for i := range ids {
		ids[i] = peerIdentity(0x20 + byte(i))
	}

	for i := range 3 {
		pt.Update(makeSignedAdvert(ids[i], uint32(100+i), "p"), 0, 0, false, nil)
		time.Sleep(time.Millisecond)
	}

	if pt.Count() != 3 {
		t.Fatalf("Count = %d, want 3", pt.Count())
	}

	pt.Update(makeSignedAdvert(ids[3], 200, "new"), 0, 0, false, nil)

	if pt.Count() != 3 {
		t.Fatalf("Count after eviction = %d, want 3", pt.Count())
	}

	if p := pt.Lookup(ids[0].PublicKey()); p != nil {
		t.Error("oldest peer should have been evicted")
	}
	if p := pt.Lookup(ids[3].PublicKey()); p == nil {
		t.Error("new peer should exist")
	}
}

func TestPeerTable_LookupReturnsACopy(t *testing.T) {
	pt := NewPeerTable(10)
	id := peerIdentity(0x30)
	pt.Update(makeSignedAdvert(id, 100, "original"), 0, 0, false, nil)

	p := pt.Lookup(id.PublicKey())
	p.Name = "mutated"

	p2 := pt.Lookup(id.PublicKey())
	if p2.Name != "original" {
		t.Errorf("Name = %q, want %q — Lookup should return a copy", p2.Name, "original")
	}
}

func TestPeerTable_UpdateStoresPath(t *testing.T) {
	pt := NewPeerTable(10)
	id := peerIdentity(0x31)
	path := []byte{0xAA, 0xBB, 0xCC}
	pt.Update(makeSignedAdvert(id, 100, "pathtest"), 0, 0, false, path)

	p := pt.Lookup(id.PublicKey())
	if !bytes.Equal(p.OutPath, []byte{0xCC, 0xBB, 0xAA}) {
		t.Errorf("OutPath = %x, want ccbbaa", p.OutPath)
	}
	if p.OutPathHashSize != 1 {
		t.Errorf("OutPathHashSize = %d, want 1", p.OutPathHashSize)
	}
}

func TestPeerTable_UpdateWithHashSize_ReversesHopsNotBytes(t *testing.T) {
	pt := NewPeerTable(10)
	id := peerIdentity(0x32)
	path := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF} // hops AABB, CCDD, EEFF
	pt.UpdateWithHashSize(makeSignedAdvert(id, 100, "path2"), 0, 0, false, path, 2)

	p := pt.Lookup(id.PublicKey())
	if !bytes.Equal(p.OutPath, []byte{0xEE, 0xFF, 0xCC, 0xDD, 0xAA, 0xBB}) {
		t.Errorf("OutPath = %x, want eeffccddaabb", p.OutPath)
	}
	if p.OutPathHashSize != 2 {
		t.Errorf("OutPathHashSize = %d, want 2", p.OutPathHashSize)
	}
}

func TestPeerTable_DefaultMaxPeers(t *testing.T) {
	pt := NewPeerTable(0)
	if pt.maxPeers != DefaultMaxPeers {
		t.Errorf("maxPeers = %d, want %d", pt.maxPeers, DefaultMaxPeers)
	}
}

func TestPeerTable_SetOutPath(t *testing.T) {
	pt := NewPeerTable(10)
	id := seedIdentity(0x30)
	adv := makeSignedAdvert(id, 1, "test")
	pt.Update(adv, 0, 0, false, nil)

	key := id.PublicKey()

	ok := pt.SetOutPath(key, []byte{0xAA, 0xBB}, 2)
	if !ok {
		t.Fatal("SetOutPath returned false")
	}
	p := pt.Lookup(key)
	if len(p.OutPath) != 2 || p.OutPath[0] != 0xAA || p.OutPath[1] != 0xBB {
		t.Errorf("OutPath = %x, want [AA BB]", p.OutPath)
	}
	if p.OutPathHashSize != 2 {
		t.Errorf("OutPathHashSize = %d, want 2", p.OutPathHashSize)
	}
}

func TestPeerTable_SetOutPath_DirectNeighbor(t *testing.T) {
	pt := NewPeerTable(10)
	id := seedIdentity(0x31)
	adv := makeSignedAdvert(id, 1, "neighbor")
	pt.Update(adv, 0, 0, false, nil)

	key := id.PublicKey()

	// Zero-length non-nil = direct neighbor
	ok := pt.SetOutPath(key, []byte{})
	if !ok {
		t.Fatal("SetOutPath returned false")
	}
	p := pt.Lookup(key)
	if p.OutPath == nil {
		t.Error("OutPath should be non-nil (direct neighbor)")
	}
	if len(p.OutPath) != 0 {
		t.Errorf("OutPath len = %d, want 0", len(p.OutPath))
	}
}

func TestPeerTable_SetOutPath_NilClearsPath(t *testing.T) {
	pt := NewPeerTable(10)
	id := seedIdentity(0x32)
	adv := makeSignedAdvert(id, 1, "clearme")
	pt.Update(adv, 0, 0, false, nil)

	key := id.PublicKey()

	pt.SetOutPath(key, []byte{0x01, 0x02}, 1)
	p := pt.Lookup(key)
	if p.OutPath == nil || p.OutPathHashSize == 0 {
		t.Fatal("precondition: path should be set")
	}

	pt.SetOutPath(key, nil)
	p = pt.Lookup(key)
	if p.OutPath != nil {
		t.Errorf("OutPath should be nil after clear, got %x", p.OutPath)
	}
	if p.OutPathHashSize != 0 {
		t.Errorf("OutPathHashSize should be 0 after clear, got %d", p.OutPathHashSize)
	}
}

func TestPeerTable_SetOutPath_HashSizePreserved(t *testing.T) {
	pt := NewPeerTable(10)
	id := seedIdentity(0x33)
	adv := makeSignedAdvert(id, 1, "preserve")
	pt.Update(adv, 0, 0, false, nil)

	key := id.PublicKey()

	pt.SetOutPath(key, []byte{0xAA, 0xBB, 0xCC, 0xDD}, 2)
	p := pt.Lookup(key)
	if p.OutPathHashSize != 2 {
		t.Fatalf("OutPathHashSize = %d, want 2", p.OutPathHashSize)
	}

	pt.SetOutPath(key, []byte{0x11, 0x22})
	p = pt.Lookup(key)
	if p.OutPathHashSize != 2 {
		t.Errorf("OutPathHashSize = %d, want 2 (preserved)", p.OutPathHashSize)
	}
	if len(p.OutPath) != 2 || p.OutPath[0] != 0x11 {
		t.Errorf("OutPath = %x, want [11 22]", p.OutPath)
	}
}

func TestPeerTable_SetOutPath_NotFound(t *testing.T) {
	pt := NewPeerTable(10)
	var key [meshcore.PubKeySize]byte
	key[0] = 0xFF
	if pt.SetOutPath(key, []byte{0x01}) {
		t.Error("SetOutPath should return false for unknown peer")
	}
}

func TestPeerTable_ResetOutPath(t *testing.T) {
	pt := NewPeerTable(10)
	id := seedIdentity(0x34)
	adv := makeSignedAdvert(id, 1, "reset")
	pt.Update(adv, 0, 0, false, nil)

	key := id.PublicKey()
	pt.SetOutPath(key, []byte{0xAA}, 1)

	ok := pt.ResetOutPath(key)
	if !ok {
		t.Fatal("ResetOutPath returned false")
	}
	p := pt.Lookup(key)
	if p.OutPath != nil {
		t.Errorf("OutPath should be nil after reset, got %x", p.OutPath)
	}
	if p.OutPathHashSize != 0 {
		t.Errorf("OutPathHashSize should be 0 after reset, got %d", p.OutPathHashSize)
	}
}

func TestPeerTable_Insert_WithOutPathHashSize(t *testing.T) {
	pt := NewPeerTable(10)
	id := seedIdentity(0x35)

	p := &Peer{
		Identity:        meshcore.NewIdentity(id.PublicKey()),
		Name:            "inserted",
		OutPath:         []byte{0x01, 0x02},
		OutPathHashSize: 2,
		LastSeen:        time.Now(),
	}
	pt.Insert(p)

	key := id.PublicKey()
	got := pt.Lookup(key)
	if got == nil {
		t.Fatal("peer not found after Insert")
	}
	if got.OutPathHashSize != 2 {
		t.Errorf("OutPathHashSize = %d, want 2", got.OutPathHashSize)
	}
	if len(got.OutPath) != 2 {
		t.Errorf("OutPath = %x, want [01 02]", got.OutPath)
	}
}

func TestPeerTable_LearnedPathsOnly(t *testing.T) {
	id := peerIdentity(0x20)
	pathHashes := []byte{0xAA, 0xBB}

	def := NewPeerTable(10)
	def.Update(makeSignedAdvert(id, 100, "x"), 0, 0, false, pathHashes)
	if p := def.Lookup(id.PublicKey()); p == nil || len(p.OutPath) == 0 {
		t.Error("default mode: expected OutPath to be set from the advert path")
	}

	lp := NewPeerTable(10)
	lp.learnedPathsOnly = true
	lp.Update(makeSignedAdvert(id, 100, "x"), 0, 0, false, pathHashes)
	p := lp.Lookup(id.PublicKey())
	if p == nil {
		t.Fatal("peer not inserted")
	}
	if len(p.OutPath) != 0 {
		t.Errorf("learnedPathsOnly: OutPath = %x, want empty (advert path must be ignored)", p.OutPath)
	}

	if !lp.SetOutPath(id.PublicKey(), pathHashes) {
		t.Fatal("SetOutPath returned false")
	}
	if p := lp.Lookup(id.PublicKey()); len(p.OutPath) == 0 {
		t.Error("learnedPathsOnly: SetOutPath should still set OutPath")
	}
}

func TestNode_LearnedPathsOnly_OrderIndependent(t *testing.T) {
	id := peerIdentity(0x21)
	orders := map[string][]Option{
		"flag before maxpeers": {WithLearnedPathsOnly(), WithMaxPeers(8)},
		"flag after maxpeers":  {WithMaxPeers(8), WithLearnedPathsOnly()},
	}
	for name, opts := range orders {
		t.Run(name, func(t *testing.T) {
			n := New(id, &mockRadio{}, opts...)
			defer n.Stop()
			if !n.peers.learnedPathsOnly {
				t.Error("learnedPathsOnly was lost; option order should not matter")
			}
		})
	}
}
