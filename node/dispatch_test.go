package node

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

func TestNode_FloodConsumedNotRelayed(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x01), radio, WithAllowForwardHandler(func(*meshcore.Packet) bool { return true }))
	defer n.Stop()

	delivered := false
	n.OnPacket(meshcore.PayloadTypeTxtMsg, func(pkt *meshcore.Packet) {
		delivered = true
		pkt.MarkDoNotRetransmit()
	})

	radio.inject(makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0x01, 0x02, 0x03, 0x04, 0x05}))
	time.Sleep(100 * time.Millisecond)

	if !delivered {
		t.Fatal("flood packet not delivered to local handlers")
	}
	if got := len(radio.sentData()); got != 0 {
		t.Fatalf("sends = %d, want 0 (packet was marked do-not-retransmit)", got)
	}
}

func TestNode_FloodRelayedWhenNotConsumed(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x01), radio, WithAllowForwardHandler(func(*meshcore.Packet) bool { return true }))
	defer n.Stop()

	delivered := false
	n.OnPacket(meshcore.PayloadTypeTxtMsg, func(*meshcore.Packet) { delivered = true })

	radio.inject(makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0x01, 0x02, 0x03, 0x04, 0x05}))
	time.Sleep(100 * time.Millisecond)

	if !delivered {
		t.Fatal("flood packet not delivered to local handlers")
	}
	if got := len(radio.sentData()); got != 1 {
		t.Fatalf("sends = %d, want 1", got)
	}
}

func TestNode_FloodDeliveredBeforeRelay(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x01), radio, WithAllowForwardHandler(func(pkt *meshcore.Packet) bool {
		return !pkt.IsMarkedDoNotRetransmit()
	}))
	defer n.Stop()

	n.OnPacket(meshcore.PayloadTypeGrpTxt, func(pkt *meshcore.Packet) { pkt.MarkDoNotRetransmit() })

	radio.inject(makeFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{0x0A, 0x0B, 0x0C, 0x0D}))
	time.Sleep(100 * time.Millisecond)

	if got := len(radio.sentData()); got != 0 {
		t.Fatalf("sends = %d, want 0", got)
	}
}

func TestNode_RxDelayHoldsFlood(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(1), radio,
		WithRxDelay(func(*meshcore.Packet, int, uint32) time.Duration { return 200 * time.Millisecond }))
	defer n.Stop()

	got := make(chan struct{}, 1)
	n.OnPacket(meshcore.PayloadTypeTxtMsg, func(*meshcore.Packet) { got <- struct{}{} })

	radio.inject(makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0x01, 0, 0, 0, 0}))
	select {
	case <-got:
		t.Fatal("packet dispatched before the rx delay elapsed")
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("packet never dispatched after the rx delay")
	}
}

func TestNode_RxDelayImmediateCases(t *testing.T) {
	identity := seedIdentity(1)
	tests := []struct {
		name  string
		delay time.Duration
		data  []byte
	}{
		{"below threshold", 10 * time.Millisecond, makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0x01, 0, 0, 0, 0})},
		{"direct not held", time.Hour, makeDirectPacket(meshcore.PayloadTypeTxtMsg, nil, []byte{0x02, 0, 0, 0, 0})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			radio := &mockRadio{}
			n := New(identity, radio, WithRxDelay(func(*meshcore.Packet, int, uint32) time.Duration { return tt.delay }))
			defer n.Stop()

			dispatched := make(chan struct{}, 1)
			n.OnPacket(meshcore.PayloadTypeTxtMsg, func(*meshcore.Packet) { dispatched <- struct{}{} })
			radio.inject(tt.data)
			select {
			case <-dispatched:
			case <-time.After(time.Second):
				t.Fatal("packet was held, want prompt dispatch")
			}
		})
	}
}

func TestRxDelayForScore(t *testing.T) {
	// A score of 0.85 makes the exponent zero, so the delay is exactly zero.
	if got := RxDelayForScore(0.85, 500); got != 0 {
		t.Errorf("RxDelayForScore(0.85, 500) = %v, want 0", got)
	}
	low, high := RxDelayForScore(0.6, 500), RxDelayForScore(0.2, 500)
	if low <= 0 || high <= low {
		t.Errorf("delay should grow as score falls: %v then %v", low, high)
	}
}

func multiAckPacket(path []byte, crc []byte, remaining uint8) []byte {
	mp := meshcore.MultiPart{Remaining: remaining, WrappedType: meshcore.PayloadTypeAck, WrappedPayload: crc}
	payload, err := mp.ToBytes()
	if err != nil {
		panic(err)
	}
	return makeDirectPacket(meshcore.PayloadTypeMultiPart, path, payload)
}

func TestNode_MultiPartACKDeliveredLocally(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(1), radio)
	defer n.Stop()

	const crc uint32 = 0xefbeadde
	done := make(chan struct{}, 1)
	n.acks.expect(crc, time.Minute, func(time.Duration) { done <- struct{}{} }, nil)

	radio.inject(multiAckPacket(nil, []byte{0xde, 0xad, 0xbe, 0xef}, 1))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("multipart ACK never reached the ACK tracker")
	}
}

func TestNode_RxDelaySerializesDispatch(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(1), radio,
		WithRxDelay(func(pkt *meshcore.Packet, _ int, _ uint32) time.Duration {
			if pkt.Payload[0] == 0x01 {
				return 100 * time.Millisecond
			}
			return 0
		}))
	defer n.Stop()

	var mu sync.Mutex
	inFlight, maxInFlight, seen := 0, 0, 0
	done := make(chan struct{})
	n.OnPacket(meshcore.PayloadTypeTxtMsg, func(*meshcore.Packet) {
		mu.Lock()
		inFlight++
		maxInFlight = max(maxInFlight, inFlight)
		mu.Unlock()

		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		inFlight--
		seen++
		if seen == 6 {
			close(done)
		}
		mu.Unlock()
	})

	radio.inject(makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0x01, 0, 0, 0, 0}))
	for i := range 5 {
		radio.inject(makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0x02, byte(i), 0, 0, 0}))
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("not all packets were dispatched")
	}
	mu.Lock()
	defer mu.Unlock()
	if maxInFlight != 1 {
		t.Fatalf("max concurrent handlers = %d, want 1", maxInFlight)
	}
}

// datagram encrypts plain into a dest/src/MAC payload from sender to dest.
func datagram(t *testing.T, sender, dest meshcore.LocalIdentity, plain []byte) []byte {
	t.Helper()
	secret, err := sender.SharedSecret(dest.Identity)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := meshcore.EncryptThenMAC(secret, plain)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte{dest.Hash()[0], sender.Hash()[0]}, enc...)
}

func newPeerNode(self, peer meshcore.LocalIdentity, opts ...Option) (*Node, *mockTxRadio) {
	radio := &mockTxRadio{}
	n := New(self, radio, append([]Option{WithAllowForwardHandler(func(*meshcore.Packet) bool { return true })}, opts...)...)
	n.peers.Insert(&Peer{Identity: peer.Identity, Name: "peer"})
	return n, radio
}

func TestNode_DatagramForUsNotRelayed(t *testing.T) {
	self, peer, stranger := seedIdentity(0x01), seedIdentity(0x02), seedIdentity(0x03)
	for _, typ := range []byte{meshcore.PayloadTypeTxtMsg, meshcore.PayloadTypeReq, meshcore.PayloadTypeResponse} {
		n, radio := newPeerNode(self, peer)
		var got []*meshcore.Packet
		n.OnPacket(typ, func(p *meshcore.Packet) { got = append(got, p) })

		radio.inject(makeFloodPacket(typ, datagram(t, peer, self, []byte("hello"))))
		if len(got) != 1 || !got[0].IsMarkedDoNotRetransmit() {
			t.Fatalf("type %d from a peer: delivered %d, want 1 marked do-not-retransmit", typ, len(got))
		}
		if calls := radio.enqueued(); len(calls) != 0 {
			t.Fatalf("type %d from a peer: %d sends, want 0", typ, len(calls))
		}

		radio.inject(makeFloodPacket(typ, datagram(t, stranger, self, []byte("hello"))))
		if len(got) != 2 || got[1].IsMarkedDoNotRetransmit() {
			t.Fatalf("type %d from a stranger: want delivered unmarked", typ)
		}
		if calls := radio.enqueued(); len(calls) != 1 {
			t.Fatalf("type %d from a stranger: %d sends, want 1 relay", typ, len(calls))
		}
		n.Stop()
	}
}

func TestNode_ShortDirectDatagramNotDecrypted(t *testing.T) {
	self := seedIdentity(0x01)
	n, radio := newPeerNode(self, seedIdentity(0x02), WithAllowPacketHandler(func(*meshcore.Packet) bool { return true }))
	defer n.Stop()
	delivered := 0
	n.OnPacket(meshcore.PayloadTypeTxtMsg, func(*meshcore.Packet) { delivered++ })

	us := self.Hash()[0]
	for _, payload := range [][]byte{{}, {us}, {us, seedIdentity(0x02).Hash()[0]}} {
		radio.dataH(&meshcore.Packet{
			Header:     meshcore.MakeHeader(meshcore.RouteTypeDirect, meshcore.PayloadTypeTxtMsg, 0),
			PathLength: 0x01,
			Path:       []byte{^us},
			Payload:    payload[:len(payload):len(payload)],
		})
	}
	if delivered != 3 {
		t.Fatalf("delivered %d short overheard datagrams, want 3", delivered)
	}
}

// collidingIdentities returns count identities sharing one 1-byte hash.
func collidingIdentities(count int) []meshcore.LocalIdentity {
	var out []meshcore.LocalIdentity
	for i := 0; len(out) < count; i++ {
		var seed [ed25519.SeedSize]byte
		binary.LittleEndian.PutUint16(seed[1:], uint16(i))
		id := meshcore.NewLocalIdentityFromSeed(seed)
		if len(out) == 0 || id.Hash()[0] == out[0].Hash()[0] {
			out = append(out, id)
		}
	}
	return out
}

func TestNode_DatagramFromCollidingPeers(t *testing.T) {
	self := seedIdentity(0x01)
	peers := collidingIdentities(3)
	n, radio := newPeerNode(self, peers[0])
	defer n.Stop()
	for _, p := range peers[1:] {
		n.peers.Insert(&Peer{Identity: p.Identity, Name: "peer"})
	}
	var got []*meshcore.Packet
	n.OnPacket(meshcore.PayloadTypeTxtMsg, func(p *meshcore.Packet) { got = append(got, p) })

	for i := range 4 {
		for _, p := range peers {
			radio.inject(makeFloodPacket(meshcore.PayloadTypeTxtMsg, datagram(t, p, self, []byte{byte(i)})))
		}
	}
	if len(got) != 12 {
		t.Fatalf("delivered %d, want 12", len(got))
	}
	for i, p := range got {
		if !p.IsMarkedDoNotRetransmit() {
			t.Fatalf("message %d not decrypted by its sender's secret", i)
		}
	}
	if calls := radio.enqueued(); len(calls) != 0 {
		t.Fatalf("%d relays, want 0", len(calls))
	}
}

func TestNode_AnonReqForUsNotRelayed(t *testing.T) {
	self, sender := seedIdentity(0x01), seedIdentity(0x04)
	n, radio := newPeerNode(self, seedIdentity(0x02))
	defer n.Stop()

	secret, _ := sender.SharedSecret(self.Identity)
	req, err := meshcore.NewAnonReq(sender, self.Identity, []byte("login"), secret)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := req.ToBytes()
	var got *meshcore.Packet
	n.OnPacket(meshcore.PayloadTypeAnonReq, func(p *meshcore.Packet) { got = p })

	radio.inject(makeFloodPacket(meshcore.PayloadTypeAnonReq, payload))
	if got == nil || !got.IsMarkedDoNotRetransmit() {
		t.Fatal("anon request to us not delivered marked do-not-retransmit")
	}
	if calls := radio.enqueued(); len(calls) != 0 {
		t.Fatalf("%d sends, want 0", len(calls))
	}

	payload[len(payload)-1] ^= 0xFF
	radio.inject(makeFloodPacket(meshcore.PayloadTypeAnonReq, payload))
	if calls := radio.enqueued(); len(calls) != 1 {
		t.Fatalf("anon request failing its MAC: %d sends, want 1 relay", len(calls))
	}
}

// pathFrom builds a PATH from peer to self carrying out as the path and an ACK CRC as extra.
func pathFrom(t *testing.T, peer, self meshcore.LocalIdentity, outLen byte, out []byte, crc uint32) []byte {
	t.Helper()
	extra := binary.LittleEndian.AppendUint32(nil, crc)
	body, err := (&meshcore.PathPayload{PathLength: outLen, Path: out, ExtraType: meshcore.PayloadTypeAck, Extra: extra}).ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	return datagram(t, peer, self, body)
}

func TestNode_PathFromPeer(t *testing.T) {
	self, peer := seedIdentity(0x01), seedIdentity(0x02)
	n, radio := newPeerNode(self, peer)
	defer n.Stop()

	const crc = 0x11223344
	acked := false
	n.ExpectACK(crc, time.Minute, func(time.Duration) { acked = true }, nil)
	delivered := false
	n.OnPacket(meshcore.PayloadTypePath, func(*meshcore.Packet) { delivered = true })

	out := []byte{0xA1, 0xA2, 0xB1, 0xB2}
	payload := pathFrom(t, peer, self, meshcore.MakePathLen(2, 2), out, crc)
	radio.inject(makePacketWithPath(meshcore.RouteTypeFlood, meshcore.PayloadTypePath, 0x01, []byte{0xC1}, payload))

	if !delivered || !acked {
		t.Fatalf("delivered = %v acked = %v, want both", delivered, acked)
	}
	p := n.Peers().Lookup(peer.PublicKey())
	if !bytes.Equal(p.OutPath, out) || p.OutPathHashSize != 2 {
		t.Fatalf("OutPath = %x/%d, want %x/2", p.OutPath, p.OutPathHashSize, out)
	}

	calls := radio.enqueued()
	if len(calls) != 1 {
		t.Fatalf("%d sends, want only the reciprocal path", len(calls))
	}
	if calls[0].priority != 1 || calls[0].delay != reciprocalPathDelay {
		t.Fatalf("reciprocal priority/delay = %d/%v, want 1/%v", calls[0].priority, calls[0].delay, reciprocalPathDelay)
	}
	ret := mustPacketFromBytes(t, calls[0].data)
	if ret.RouteType() != meshcore.RouteTypeDirect || ret.PayloadType() != meshcore.PayloadTypePath ||
		ret.PathLength != meshcore.MakePathLen(2, 2) || !bytes.Equal(ret.Path, out) {
		t.Fatalf("reciprocal = route %d type %d path %x/%02x, want direct PATH along %x", ret.RouteType(), ret.PayloadType(), ret.Path, ret.PathLength, out)
	}
	secret, _ := peer.SharedSecret(self.Identity)
	plain, err := meshcore.MACThenDecrypt(secret, ret.Payload[2:])
	if err != nil {
		t.Fatal(err)
	}
	pp, err := meshcore.ParsePathPayload(plain)
	if err != nil {
		t.Fatal(err)
	}
	if pp.PathLength != 0x01 || !bytes.Equal(pp.Path, []byte{0xC1}) || ret.Payload[0] != peer.Hash()[0] {
		t.Fatalf("reciprocal carries %x/%02x to %02x, want c1/01 to the peer", pp.Path, pp.PathLength, ret.Payload[0])
	}

	radio.inject(makeDirectPacket(meshcore.PayloadTypePath, nil, pathFrom(t, peer, self, 0x01, []byte{0xD1}, 0)))
	if got := len(radio.enqueued()); got != 1 {
		t.Fatalf("direct PATH: %d sends, want no reciprocal", got)
	}
	if p := n.Peers().Lookup(peer.PublicKey()); !bytes.Equal(p.OutPath, []byte{0xD1}) {
		t.Fatalf("direct PATH: OutPath = %x, want d1", p.OutPath)
	}
}

func TestNode_PathBadEncodingRejected(t *testing.T) {
	self, peer := seedIdentity(0x01), seedIdentity(0x02)
	n, radio := newPeerNode(self, peer)
	defer n.Stop()

	secret, _ := peer.SharedSecret(self.Identity)
	enc, _ := meshcore.EncryptThenMAC(secret, []byte{0xC1, 0xAA, 0xBB, 0xCC, 0xDD, 0xFF})
	var got *meshcore.Packet
	n.OnPacket(meshcore.PayloadTypePath, func(p *meshcore.Packet) { got = p })
	radio.inject(makeFloodPacket(meshcore.PayloadTypePath, append([]byte{self.Hash()[0], peer.Hash()[0]}, enc...)))

	if got == nil || got.IsMarkedDoNotRetransmit() {
		t.Fatal("bad path_len: want delivered and not consumed")
	}
	if p := n.Peers().Lookup(peer.PublicKey()); p.OutPath != nil {
		t.Fatalf("OutPath = %x, want unset", p.OutPath)
	}
	calls := radio.enqueued()
	if len(calls) != 1 || mustPacketFromBytes(t, calls[0].data).RouteType() != meshcore.RouteTypeFlood {
		t.Fatalf("%d sends, want one flood relay", len(calls))
	}
}

func TestNode_PathReturnErrorReported(t *testing.T) {
	self, peer := seedIdentity(0x01), seedIdentity(0x02)
	var errs []error
	n, radio := newPeerNode(self, peer, WithErrorHandler(func(err error) { errs = append(errs, err) }))
	defer n.Stop()

	pkt := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypePath, 0),
		PathLength: 0x02,
		Path:       []byte{0xC1},
		Payload:    pathFrom(t, peer, self, 0x01, []byte{0xA1}, 0),
	}
	radio.dataH(pkt)
	if len(errs) != 1 || len(radio.enqueued()) != 0 {
		t.Fatalf("errors %v, %d sends, want the bad return path reported and nothing sent", errs, len(radio.enqueued()))
	}
}

func TestNode_WithoutReciprocalPath(t *testing.T) {
	self, peer := seedIdentity(0x01), seedIdentity(0x02)
	n, radio := newPeerNode(self, peer, WithoutReciprocalPath())
	defer n.Stop()

	radio.inject(makeFloodPacket(meshcore.PayloadTypePath, pathFrom(t, peer, self, 0x01, []byte{0xA1}, 0)))
	if got := len(radio.enqueued()); got != 0 {
		t.Fatalf("%d sends, want 0", got)
	}
	if p := n.Peers().Lookup(peer.PublicKey()); !bytes.Equal(p.OutPath, []byte{0xA1}) {
		t.Fatalf("OutPath = %x, want a1", p.OutPath)
	}
}

func TestNode_FloodFilterHandler(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x01), radio, WithFloodFilterHandler(func(*meshcore.Packet) bool { return true }))
	defer n.Stop()

	count := 0
	n.OnPacket(meshcore.PayloadTypeGrpTxt, func(*meshcore.Packet) { count++ })
	n.OnPacket(meshcore.PayloadTypeRawCustom, func(*meshcore.Packet) { count++ })

	flood := makeFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{1, 2, 3, 4})
	radio.inject(flood)
	radio.inject(makeDirectPacket(meshcore.PayloadTypeRawCustom, nil, []byte{1}))
	if count != 1 {
		t.Fatalf("delivered %d, want only the direct packet", count)
	}

	n.SetFloodFilterHandler(nil)
	radio.inject(flood)
	if count != 2 {
		t.Fatal("filtered flood was recorded as seen")
	}
}
