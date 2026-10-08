package node

import (
	"sync"
	"testing"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

func testChannel(name string) *meshcore.ChannelEntry {
	return meshcore.NewChannelFromHashtag(name)
}

func TestChannelTable_SetAndGet(t *testing.T) {
	ct := newChannelTable(DefaultMaxChannels)
	ch := testChannel("test")

	if !ct.set(0, ch) {
		t.Fatal("set(0) returned false")
	}
	got := ct.get(0)
	if got != ch {
		t.Error("get(0) returned different pointer")
	}
}

func TestChannelTable_SetOutOfRange(t *testing.T) {
	ct := newChannelTable(DefaultMaxChannels)
	ch := testChannel("test")

	if ct.set(-1, ch) {
		t.Error("set(-1) should return false")
	}
	if ct.set(DefaultMaxChannels, ch) {
		t.Errorf("set(%d) should return false", DefaultMaxChannels)
	}
}

func TestChannelTable_GetOutOfRange(t *testing.T) {
	ct := newChannelTable(DefaultMaxChannels)
	if ct.get(-1) != nil {
		t.Error("get(-1) should return nil")
	}
	if ct.get(DefaultMaxChannels) != nil {
		t.Errorf("get(%d) should return nil", DefaultMaxChannels)
	}
}

func TestChannelTable_Remove(t *testing.T) {
	ct := newChannelTable(DefaultMaxChannels)
	ch := testChannel("test")
	ct.set(3, ch)

	if !ct.remove(3) {
		t.Error("remove(3) returned false for existing entry")
	}
	if ct.get(3) != nil {
		t.Error("get(3) should be nil after remove")
	}
}

func TestChannelTable_RemoveEmpty(t *testing.T) {
	ct := newChannelTable(DefaultMaxChannels)
	if ct.remove(0) {
		t.Error("remove(0) should return false for empty slot")
	}
}

func TestChannelTable_RemoveOutOfRange(t *testing.T) {
	ct := newChannelTable(DefaultMaxChannels)
	if ct.remove(-1) {
		t.Error("remove(-1) should return false")
	}
	if ct.remove(DefaultMaxChannels) {
		t.Errorf("remove(%d) should return false", DefaultMaxChannels)
	}
}

func TestChannelTable_FindByHash(t *testing.T) {
	ct := newChannelTable(DefaultMaxChannels)
	ch := testChannel("findme")
	ct.set(2, ch)

	got := ct.findByHash(ch.Hash)
	if len(got) != 1 || got[0] != ch {
		t.Error("findByHash did not return the expected channel")
	}
}

func TestChannelTable_FindByHashMiss(t *testing.T) {
	ct := newChannelTable(DefaultMaxChannels)
	ct.set(0, testChannel("one"))

	got := ct.findByHash(0xFF)
	if len(got) != 0 {
		t.Error("findByHash should return empty slice for unmatched hash")
	}
}

func TestChannelTable_FindByHashReturnsAll(t *testing.T) {
	ct := newChannelTable(DefaultMaxChannels)

	ch1 := testChannel("alpha")
	ch2 := testChannel("beta")
	ch2.Hash = ch1.Hash // force collision

	ct.set(1, ch1)
	ct.set(5, ch2)

	got := ct.findByHash(ch1.Hash)
	if len(got) != 2 {
		t.Fatalf("findByHash returned %d matches, want 2", len(got))
	}
	if got[0] != ch1 || got[1] != ch2 {
		t.Error("findByHash should return both colliding channels in order")
	}
}

func TestChannelTable_All(t *testing.T) {
	ct := newChannelTable(DefaultMaxChannels)
	ct.set(0, testChannel("a"))
	ct.set(3, testChannel("b"))
	ct.set(7, testChannel("c"))

	all := ct.all()
	if len(all) != 3 {
		t.Fatalf("all() returned %d entries, want 3", len(all))
	}
}

func TestChannelTable_AllEmpty(t *testing.T) {
	ct := newChannelTable(DefaultMaxChannels)
	all := ct.all()
	if len(all) != 0 {
		t.Errorf("all() returned %d entries, want 0", len(all))
	}
}

func TestChannelTable_Overwrite(t *testing.T) {
	ct := newChannelTable(DefaultMaxChannels)
	ch1 := testChannel("first")
	ch2 := testChannel("second")
	ct.set(0, ch1)
	ct.set(0, ch2)

	if ct.get(0) != ch2 {
		t.Error("set should overwrite existing entry")
	}
}

func TestChannelTable_SetNilClears(t *testing.T) {
	ct := newChannelTable(DefaultMaxChannels)
	ct.set(0, testChannel("x"))
	ct.set(0, nil)

	if ct.get(0) != nil {
		t.Error("setting nil should clear the slot")
	}
	all := ct.all()
	if len(all) != 0 {
		t.Error("all() should skip nil slots")
	}
}

func TestChannelTable_Concurrent(t *testing.T) {
	ct := newChannelTable(DefaultMaxChannels)
	var wg sync.WaitGroup

	for i := range DefaultMaxChannels {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ch := testChannel("concurrent")
			ct.set(idx, ch)
			ct.get(idx)
			ct.findByHash(ch.Hash)
			ct.all()
			ct.remove(idx)
		}(i)
	}
	wg.Wait()
}

func TestNode_SetChannel(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xC0), radio)
	defer n.Stop()

	ch := testChannel("node-ch")
	if !n.SetChannel(0, ch) {
		t.Fatal("SetChannel(0) returned false")
	}
	if n.Channel(0) != ch {
		t.Error("Channel(0) mismatch")
	}
}

func TestNode_RemoveChannel(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xC1), radio)
	defer n.Stop()

	n.SetChannel(2, testChannel("rm"))
	if !n.RemoveChannel(2) {
		t.Error("RemoveChannel(2) returned false")
	}
	if n.Channel(2) != nil {
		t.Error("Channel(2) should be nil after remove")
	}
}

func TestNode_ChannelsByHash(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xC2), radio)
	defer n.Stop()

	ch := testChannel("lookup")
	n.SetChannel(4, ch)

	got := n.ChannelsByHash(ch.Hash)
	if len(got) == 0 {
		t.Fatal("ChannelsByHash returned no matches")
	}
	found := false
	for _, g := range got {
		if g == ch {
			found = true
		}
	}
	if !found {
		t.Error("ChannelsByHash did not return expected channel")
	}
}

func TestNode_Channels(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xC3), radio)
	defer n.Stop()

	n.SetChannel(0, testChannel("a"))
	n.SetChannel(5, testChannel("b"))

	all := n.Channels()
	if len(all) != 2 {
		t.Errorf("Channels() returned %d, want 2", len(all))
	}
}

func TestNode_WithChannels(t *testing.T) {
	radio := &mockRadio{}
	ch0 := testChannel("init0")
	ch1 := testChannel("init1")
	n := New(seedIdentity(0xC4), radio, WithChannels(ch0, ch1))
	defer n.Stop()

	if n.Channel(0) != ch0 {
		t.Error("Channel(0) not set by WithChannels")
	}
	if n.Channel(1) != ch1 {
		t.Error("Channel(1) not set by WithChannels")
	}
	if n.Channel(2) != nil {
		t.Error("Channel(2) should be nil")
	}
}

func TestNode_WithChannelsOverflow(t *testing.T) {
	radio := &mockRadio{}
	chs := make([]*meshcore.ChannelEntry, DefaultMaxChannels+3)
	for i := range chs {
		chs[i] = testChannel("overflow")
	}
	n := New(seedIdentity(0xC5), radio, WithChannels(chs...))
	defer n.Stop()

	all := n.Channels()
	if len(all) != DefaultMaxChannels {
		t.Errorf("Channels() = %d, want %d (extras should be ignored)", len(all), DefaultMaxChannels)
	}
}

func TestNode_WithChannels_OrderIndependent(t *testing.T) {
	ch0 := testChannel("order0")
	ch1 := testChannel("order1")
	orders := map[string][]Option{
		"channels before max": {WithChannels(ch0, ch1), WithMaxChannels(4)},
		"channels after max":  {WithMaxChannels(4), WithChannels(ch0, ch1)},
	}
	for name, opts := range orders {
		t.Run(name, func(t *testing.T) {
			n := New(seedIdentity(0xC6), &mockRadio{}, opts...)
			defer n.Stop()
			if n.channels.maxChannels() != 4 {
				t.Errorf("maxChannels = %d, want 4", n.channels.maxChannels())
			}
			if n.Channel(0) != ch0 || n.Channel(1) != ch1 {
				t.Error("channels lost; option order should not matter")
			}
		})
	}
}

func makeGroupTextPacket(t *testing.T, ch *meshcore.ChannelEntry, sender, text string) *meshcore.Packet {
	t.Helper()
	payload := &meshcore.GroupTextPayload{
		Timestamp: uint32(time.Now().Unix()),
		Sender:    sender,
		Text:      text,
	}
	gt, err := payload.Encrypt(ch.Hash, ch.PSK[:])
	if err != nil {
		t.Fatalf("encrypt group text: %v", err)
	}
	raw, err := gt.ToBytes()
	if err != nil {
		t.Fatalf("serialize group text: %v", err)
	}
	return &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeGrpTxt, 0),
		PathLength: 0x00,
		Payload:    raw,
	}
}

func makeGroupDataPacket(t *testing.T, ch *meshcore.ChannelEntry, plaintext []byte) *meshcore.Packet {
	t.Helper()
	encrypted, err := meshcore.EncryptThenMAC(ch.PSK[:], plaintext)
	if err != nil {
		t.Fatalf("encrypt group data: %v", err)
	}
	gd := &meshcore.GroupData{
		ChannelHash:      ch.Hash,
		MAC:              [2]byte{encrypted[0], encrypted[1]},
		EncryptedPayload: encrypted[2:],
	}
	raw, err := gd.ToBytes()
	if err != nil {
		t.Fatalf("serialize group data: %v", err)
	}
	return &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeGrpData, 0),
		PathLength: 0x00,
		Payload:    raw,
	}
}

func TestNode_DecryptGroupText(t *testing.T) {
	radio := &mockRadio{}
	ch := testChannel("grptxt")
	n := New(seedIdentity(0xD0), radio, WithChannels(ch))
	defer n.Stop()

	pkt := makeGroupTextPacket(t, ch, "Alice", "hello")

	msg, got, err := n.DecryptGroupText(pkt)
	if err != nil {
		t.Fatalf("DecryptGroupText: %v", err)
	}
	if got != ch {
		t.Error("returned channel does not match")
	}
	if msg.Sender != "Alice" || msg.Text != "hello" {
		t.Errorf("got sender=%q text=%q, want Alice/hello", msg.Sender, msg.Text)
	}
}

func TestNode_DecryptGroupText_NoChannel(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xD1), radio)
	defer n.Stop()

	ch := testChannel("unknown")
	pkt := makeGroupTextPacket(t, ch, "Bob", "hi")

	_, _, err := n.DecryptGroupText(pkt)
	if err != ErrNoMatchingChannel {
		t.Fatalf("expected ErrNoMatchingChannel, got %v", err)
	}
}

func TestNode_DecryptGroupText_WrongKey(t *testing.T) {
	radio := &mockRadio{}
	ch1 := testChannel("real")
	ch2 := testChannel("fake")
	ch2.Hash = ch1.Hash
	n := New(seedIdentity(0xD2), radio, WithChannels(ch2))
	defer n.Stop()

	pkt := makeGroupTextPacket(t, ch1, "Eve", "secret")

	_, _, err := n.DecryptGroupText(pkt)
	if err != ErrNoMatchingChannel {
		t.Fatalf("expected ErrNoMatchingChannel, got %v", err)
	}
}

func TestNode_DecryptGroupData(t *testing.T) {
	radio := &mockRadio{}
	ch := testChannel("grpdata")
	n := New(seedIdentity(0xD3), radio, WithChannels(ch))
	defer n.Stop()

	plaintext := []byte("sensor data here")
	pkt := makeGroupDataPacket(t, ch, plaintext)

	got, gotCh, err := n.DecryptGroupData(pkt)
	if err != nil {
		t.Fatalf("DecryptGroupData: %v", err)
	}
	if gotCh != ch {
		t.Error("returned channel does not match")
	}
	if string(got[:len(plaintext)]) != string(plaintext) {
		t.Errorf("got %q, want %q", got[:len(plaintext)], plaintext)
	}
}

func TestNode_DecryptGroupData_NoChannel(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xD4), radio)
	defer n.Stop()

	ch := testChannel("missing")
	pkt := makeGroupDataPacket(t, ch, []byte("data"))

	_, _, err := n.DecryptGroupData(pkt)
	if err != ErrNoMatchingChannel {
		t.Fatalf("expected ErrNoMatchingChannel, got %v", err)
	}
}
