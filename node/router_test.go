package node

import (
	"bytes"
	"testing"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

func mustPacketFromBytes(t *testing.T, data []byte) *meshcore.Packet {
	t.Helper()
	pkt, err := meshcore.PacketFromBytes(data)
	if err != nil {
		t.Fatalf("PacketFromBytes() error = %v", err)
	}
	return pkt
}

func makeDirectPacket(payloadType byte, path []byte, payload []byte) []byte {
	out := []byte{meshcore.MakeHeader(meshcore.RouteTypeDirect, payloadType, 0), byte(len(path))}
	out = append(out, path...)
	out = append(out, payload...)
	return out
}

func makePacketWithPath(routeType, payloadType, pathLength byte, path []byte, payload []byte) []byte {
	out := []byte{meshcore.MakeHeader(routeType, payloadType, 0), pathLength}
	out = append(out, path...)
	out = append(out, payload...)
	return out
}

type testRouterOpts struct {
	identity     meshcore.LocalIdentity
	allowForward func(*meshcore.Packet) bool
	allowPacket  func(*meshcore.Packet) bool
	send         func([]byte, uint8) error
	sendDirect   func(data []byte, priority uint8) error
	extraAcks    func() uint8
}

// routeThenRelay mirrors Node.onData: route, deliver, then consider a flood relay.
func routeThenRelay(r *router, pkt *meshcore.Packet) RouteAction {
	action := r.route(pkt)
	if action == RouteActionDeliver {
		r.relayFlood(pkt)
	}
	return action
}

func newTestRouter(opts testRouterOpts) *router {
	n := &Node{
		identity: opts.identity,
		regions:  NewRegionMap(),
	}
	n.allowForward = opts.allowForward
	n.allowPacket = opts.allowPacket
	n.extraAcks = opts.extraAcks
	n.router.node = n
	// Direct and flood relays share one hook; tests assert payload and priority.
	n.router.send = func(data []byte, priority uint8, _ time.Duration) error {
		if opts.sendDirect != nil && priority != PriorityFloodRelay {
			return opts.sendDirect(data, priority)
		}
		if opts.send != nil {
			return opts.send(data, priority)
		}
		return nil
	}
	return &n.router
}

func TestRouter_FloodDedup(t *testing.T) {
	r := newTestRouter(testRouterOpts{identity: seedIdentity(0x01)})
	data := makeFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{0x01, 0x02, 0x03, 0x04})

	if got := routeThenRelay(r, mustPacketFromBytes(t, data)); got != RouteActionDeliver {
		t.Fatalf("first route() = %v, want %v", got, RouteActionDeliver)
	}
	if got := routeThenRelay(r, mustPacketFromBytes(t, data)); got != RouteActionDrop {
		t.Fatalf("second route() = %v, want %v", got, RouteActionDrop)
	}
}

func TestRouter_FloodForward(t *testing.T) {
	identity := seedIdentity(0x01)
	var sent [][]byte
	r := newTestRouter(testRouterOpts{
		identity:     identity,
		allowForward: func(*meshcore.Packet) bool { return true },
		send: func(data []byte, _ uint8) error {
			sent = append(sent, append([]byte(nil), data...))
			return nil
		},
	})

	pkt := mustPacketFromBytes(t, makeFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{0xAA, 0xBB, 0xCC, 0xDD}))
	if got := routeThenRelay(r, pkt); got != RouteActionDeliver {
		t.Fatalf("route() = %v, want %v", got, RouteActionDeliver)
	}
	if len(sent) != 1 {
		t.Fatalf("send count = %d, want 1", len(sent))
	}
	if pkt.PathHashCount() != 0 {
		t.Fatalf("original PathHashCount() = %d, want 0", pkt.PathHashCount())
	}

	forwarded := mustPacketFromBytes(t, sent[0])
	if got := forwarded.PathHashCount(); got != 1 {
		t.Fatalf("forwarded PathHashCount() = %d, want 1", got)
	}
	hashes := forwarded.PathHashes()
	if len(hashes) != 1 || !bytes.Equal(hashes[0], identity.Hash()) {
		t.Fatalf("forwarded path hash = %x, want %x", forwarded.Path, identity.Hash())
	}
}

func TestRouter_FloodNoForwardByDefault(t *testing.T) {
	called := false
	r := newTestRouter(testRouterOpts{
		identity: seedIdentity(0x01),
		send: func([]byte, uint8) error {
			called = true
			return nil
		},
	})

	pkt := mustPacketFromBytes(t, makeFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{0x01, 0x02, 0x03, 0x04}))
	if got := routeThenRelay(r, pkt); got != RouteActionDeliver {
		t.Fatalf("route() = %v, want %v", got, RouteActionDeliver)
	}
	if called {
		t.Fatal("send() called, want no forward")
	}
}

func TestRouter_FloodPathFull(t *testing.T) {
	called := false
	path := bytes.Repeat([]byte{0xAA, 0xBB}, meshcore.MaxPathSize/2)
	pkt := mustPacketFromBytes(t, makePacketWithPath(meshcore.RouteTypeFlood, meshcore.PayloadTypeGrpTxt, 0x60, path, []byte{0x01, 0x02, 0x03, 0x04}))
	r := newTestRouter(testRouterOpts{
		identity:     seedIdentity(0x01),
		allowForward: func(*meshcore.Packet) bool { return true },
		send: func([]byte, uint8) error {
			called = true
			return nil
		},
	})

	if got := routeThenRelay(r, pkt); got != RouteActionDeliver {
		t.Fatalf("route() = %v, want %v", got, RouteActionDeliver)
	}
	if called {
		t.Fatal("send() called for full path")
	}
}

func TestRouter_DirectForward(t *testing.T) {
	identity := seedIdentity(0x01)
	other := seedIdentity(0x02)
	var sent [][]byte
	r := newTestRouter(testRouterOpts{
		identity:     identity,
		allowForward: func(*meshcore.Packet) bool { return true },
		sendDirect: func(data []byte, _ uint8) error {
			sent = append(sent, append([]byte(nil), data...))
			return nil
		},
	})

	path := []byte{identity.Hash()[0], other.Hash()[0]}
	pkt := mustPacketFromBytes(t, makeDirectPacket(meshcore.PayloadTypeAdvert, path, []byte{0x99}))
	if got := routeThenRelay(r, pkt); got != RouteActionForward {
		t.Fatalf("route() = %v, want %v", got, RouteActionForward)
	}
	if len(sent) != 1 {
		t.Fatalf("send count = %d, want 1", len(sent))
	}
	if got := pkt.PathHashCount(); got != 1 {
		t.Fatalf("PathHashCount() = %d, want 1", got)
	}
	if !bytes.Equal(pkt.Path, []byte{other.Hash()[0]}) {
		t.Fatalf("Path = %x, want %x", pkt.Path, []byte{other.Hash()[0]})
	}

	forwarded := mustPacketFromBytes(t, sent[0])
	if got := forwarded.PathHashCount(); got != 1 {
		t.Fatalf("forwarded PathHashCount() = %d, want 1", got)
	}
	if !bytes.Equal(forwarded.Path, []byte{other.Hash()[0]}) {
		t.Fatalf("forwarded Path = %x, want %x", forwarded.Path, []byte{other.Hash()[0]})
	}
}

func TestRouter_DirectNotNextHop(t *testing.T) {
	identity := seedIdentity(0x01)
	next := seedIdentity(0x02)
	called := false
	r := newTestRouter(testRouterOpts{
		identity:     identity,
		allowForward: func(*meshcore.Packet) bool { return true },
		send: func([]byte, uint8) error {
			called = true
			return nil
		},
	})

	pkt := mustPacketFromBytes(t, makeDirectPacket(meshcore.PayloadTypeAdvert, []byte{next.Hash()[0]}, []byte{0x01}))
	if got := routeThenRelay(r, pkt); got != RouteActionDrop {
		t.Fatalf("route() = %v, want %v", got, RouteActionDrop)
	}
	if called {
		t.Fatal("send() called for wrong next hop")
	}
}

func TestRouter_DirectDedup(t *testing.T) {
	identity := seedIdentity(0x01)
	other := seedIdentity(0x02)
	var sends int
	r := newTestRouter(testRouterOpts{
		identity:     identity,
		allowForward: func(*meshcore.Packet) bool { return true },
		sendDirect: func([]byte, uint8) error {
			sends++
			return nil
		},
	})

	data := makeDirectPacket(meshcore.PayloadTypeAdvert, []byte{identity.Hash()[0], other.Hash()[0]}, []byte{0x44})
	if got := routeThenRelay(r, mustPacketFromBytes(t, data)); got != RouteActionForward {
		t.Fatalf("first route() = %v, want %v", got, RouteActionForward)
	}
	if got := routeThenRelay(r, mustPacketFromBytes(t, data)); got != RouteActionDrop {
		t.Fatalf("second route() = %v, want %v", got, RouteActionDrop)
	}
	if sends != 1 {
		t.Fatalf("send count = %d, want 1", sends)
	}
}

func TestRouter_SendPacketMarksSeen(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x03), radio)
	called := false
	n.OnPacket(meshcore.PayloadTypeGrpTxt, func(*meshcore.Packet) {
		called = true
	})

	pkt := mustPacketFromBytes(t, makeFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{0xDE, 0xAD, 0xBE, 0xEF}))
	if err := n.SendPacket(pkt); err != nil {
		t.Fatalf("SendPacket() error = %v", err)
	}

	data, err := pkt.ToBytes()
	if err != nil {
		t.Fatalf("ToBytes() error = %v", err)
	}
	radio.inject(data)

	if called {
		t.Fatal("handler called for self-originated packet")
	}
}

func TestRouter_NonFloodNonDirect_Dedup(t *testing.T) {
	r := newTestRouter(testRouterOpts{identity: seedIdentity(0x01)})
	data := makeDirectPacket(meshcore.PayloadTypeGrpTxt, nil, []byte{0x33, 0x33, 0x33, 0x33})

	if got := routeThenRelay(r, mustPacketFromBytes(t, data)); got != RouteActionDeliver {
		t.Fatalf("first route() = %v, want %v", got, RouteActionDeliver)
	}
	if got := routeThenRelay(r, mustPacketFromBytes(t, data)); got != RouteActionDrop {
		t.Fatalf("second route() = %v, want %v", got, RouteActionDrop)
	}
}

func TestRouter_DirectNextHopNoForward_Dropped(t *testing.T) {
	identity := seedIdentity(0x01)
	other := seedIdentity(0x02)
	r := newTestRouter(testRouterOpts{identity: identity})

	path := []byte{identity.Hash()[0], other.Hash()[0]}
	pkt := mustPacketFromBytes(t, makeDirectPacket(meshcore.PayloadTypeTxtMsg, path, []byte{0x55}))
	if got := routeThenRelay(r, pkt); got != RouteActionDrop {
		t.Fatalf("route() = %v, want %v (hops remaining, no forward → drop)", got, RouteActionDrop)
	}

	r = newTestRouter(testRouterOpts{identity: identity, allowPacket: func(*meshcore.Packet) bool { return true }})
	if got := routeThenRelay(r, mustPacketFromBytes(t, makeDirectPacket(meshcore.PayloadTypeTxtMsg, path, []byte{0x55}))); got != RouteActionDeliver {
		t.Fatalf("route() = %v, want %v (allowPacket opts in)", got, RouteActionDeliver)
	}
}

func TestRouter_FloodForwardPolicy(t *testing.T) {
	cases := []struct {
		name    string
		typ     byte
		deliver bool
		forward bool
	}{
		{"ACK", meshcore.PayloadTypeAck, true, true},
		{"PATH", meshcore.PayloadTypePath, true, true},
		{"REQ", meshcore.PayloadTypeReq, true, true},
		{"RESPONSE", meshcore.PayloadTypeResponse, true, true},
		{"TXT_MSG", meshcore.PayloadTypeTxtMsg, true, true},
		{"ANON_REQ", meshcore.PayloadTypeAnonReq, true, true},
		{"GRP_TXT", meshcore.PayloadTypeGrpTxt, true, true},
		{"GRP_DATA", meshcore.PayloadTypeGrpData, true, true},
		{"ADVERT_short", meshcore.PayloadTypeAdvert, false, false},
		{"MULTIPART", meshcore.PayloadTypeMultiPart, true, false},
		{"TRACE", meshcore.PayloadTypeTrace, false, false},
		{"CONTROL", meshcore.PayloadTypeControl, false, false},
		{"RAW_CUSTOM", meshcore.PayloadTypeRawCustom, false, false},
		{"unknown_0x0C", 0x0C, false, false},
		{"unknown_0x0E", 0x0E, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sends := 0
			r := newTestRouter(testRouterOpts{
				identity:     seedIdentity(0x01),
				allowForward: func(*meshcore.Packet) bool { return true },
				send:         func([]byte, uint8) error { sends++; return nil },
			})
			pkt := mustPacketFromBytes(t, makeFloodPacket(tc.typ, bytes.Repeat([]byte{0x05}, 48)))
			if got := routeThenRelay(r, pkt); (got == RouteActionDeliver) != tc.deliver {
				t.Fatalf("route() = %v, want deliver=%v", got, tc.deliver)
			}
			if (sends == 1) != tc.forward {
				t.Fatalf("sends = %d, want forward=%v", sends, tc.forward)
			}
		})
	}
}

func TestRouter_ShortAndSelfAdvertsDroppedUnseen(t *testing.T) {
	self := seedIdentity(0x01)
	own, _ := makeSignedAdvert(self, 100, "me").ToBytes()
	for name, payload := range map[string][]byte{"short": make([]byte, meshcore.MinAdvertSize-1), "own": own} {
		r := newTestRouter(testRouterOpts{identity: self})
		pkt := mustPacketFromBytes(t, makeFloodPacket(meshcore.PayloadTypeAdvert, payload))
		if got := r.route(pkt); got != RouteActionDrop || r.dedup.Contains(pkt) {
			t.Errorf("%s advert: route() = %v, seen = %v, want dropped unseen", name, got, r.dedup.Contains(pkt))
		}
	}
	r := newTestRouter(testRouterOpts{identity: self})
	if got := r.route(mustPacketFromBytes(t, makeFloodPacket(meshcore.PayloadTypeAdvert, make([]byte, meshcore.MinAdvertSize)))); got != RouteActionDeliver {
		t.Fatalf("full-size advert: route() = %v, want %v", got, RouteActionDeliver)
	}
}

func TestRouter_FloodAdvertForwardOnlyWhenVerified(t *testing.T) {
	self := seedIdentity(0x01)
	other := seedIdentity(0x02)
	newRouter := func(sends *int) *router {
		return newTestRouter(testRouterOpts{
			identity:     self,
			allowForward: func(*meshcore.Packet) bool { return true },
			send:         func([]byte, uint8) error { *sends++; return nil },
		})
	}
	advertPacket := func(t *testing.T, adv *meshcore.Advert) *meshcore.Packet {
		payload, err := adv.ToBytes()
		if err != nil {
			t.Fatalf("Advert.ToBytes() error = %v", err)
		}
		return mustPacketFromBytes(t, makeFloodPacket(meshcore.PayloadTypeAdvert, payload))
	}

	var sends int
	routeThenRelay(newRouter(&sends), advertPacket(t, makeSignedAdvert(other, 100, "peer")))
	if sends != 1 {
		t.Fatalf("valid advert: sends = %d, want 1", sends)
	}

	sends = 0
	forged := makeSignedAdvert(other, 100, "peer")
	forged.Signature[0] ^= 0xFF
	routeThenRelay(newRouter(&sends), advertPacket(t, forged))
	if sends != 0 {
		t.Fatalf("forged advert: sends = %d, want 0", sends)
	}

	sends = 0
	routeThenRelay(newRouter(&sends), advertPacket(t, makeSignedAdvert(self, 100, "me")))
	if sends != 0 {
		t.Fatalf("own advert: sends = %d, want 0", sends)
	}
}

func TestRouter_DirectControlZeroHopOnly(t *testing.T) {
	identity := seedIdentity(0x01)
	sends := 0
	r := newTestRouter(testRouterOpts{
		identity:     identity,
		allowForward: func(*meshcore.Packet) bool { return true },
		sendDirect:   func([]byte, uint8) error { sends++; return nil },
	})

	withHops := makeDirectPacket(meshcore.PayloadTypeControl, []byte{identity.Hash()[0]}, []byte{0x80, 0x01})
	if got := routeThenRelay(r, mustPacketFromBytes(t, withHops)); got != RouteActionDrop {
		t.Fatalf("route(control, 1 hop) = %v, want %v", got, RouteActionDrop)
	}
	if sends != 0 {
		t.Fatalf("sends = %d, want 0", sends)
	}

	zeroHop := makeDirectPacket(meshcore.PayloadTypeControl, nil, []byte{0x80, 0x01})
	if got := routeThenRelay(r, mustPacketFromBytes(t, zeroHop)); got != RouteActionDeliver {
		t.Fatalf("route(control, 0 hops) = %v, want %v", got, RouteActionDeliver)
	}
}

func TestNode_DirectRelayNotDelivered(t *testing.T) {
	identity := seedIdentity(0x01)
	other := seedIdentity(0x02)
	radio := &mockRadio{}
	n := New(identity, radio, WithAllowForwardHandler(func(*meshcore.Packet) bool { return true }))
	defer n.Stop()

	delivered := false
	n.OnPacket(meshcore.PayloadTypeTxtMsg, func(*meshcore.Packet) { delivered = true })

	radio.inject(makeDirectPacket(meshcore.PayloadTypeTxtMsg, []byte{identity.Hash()[0], other.Hash()[0]}, []byte{0x01, 0x02, 0x03}))
	time.Sleep(100 * time.Millisecond)

	if delivered {
		t.Fatal("relayed direct packet was delivered to local handlers")
	}
	if got := len(radio.sentData()); got != 1 {
		t.Fatalf("relayed sends = %d, want 1", got)
	}

	// Path exhausted: delivered, not relayed.
	radio.inject(makeDirectPacket(meshcore.PayloadTypeTxtMsg, nil, []byte{0x04, 0x05, 0x06, 0x07, 0x08}))
	if !delivered {
		t.Fatal("zero-hop direct packet was not delivered")
	}
}

func TestRouter_ForwardErrorReported(t *testing.T) {
	var got error
	r := newTestRouter(testRouterOpts{
		identity:     seedIdentity(0x01),
		allowForward: func(*meshcore.Packet) bool { return true },
		send:         func([]byte, uint8) error { return ErrTxQueueFull },
	})
	r.node.errH = func(err error) { got = err }

	routeThenRelay(r, mustPacketFromBytes(t, makeFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{0x01, 0x02, 0x03, 0x04})))
	if got != ErrTxQueueFull {
		t.Fatalf("error handler got %v, want %v", got, ErrTxQueueFull)
	}
}

func TestRouter_DirectNotNextHop_AllowPacketTrue(t *testing.T) {
	identity := seedIdentity(0x01)
	next := seedIdentity(0x02)
	r := newTestRouter(testRouterOpts{
		identity:    identity,
		allowPacket: func(*meshcore.Packet) bool { return true },
	})

	pkt := mustPacketFromBytes(t, makeDirectPacket(meshcore.PayloadTypeAdvert, []byte{next.Hash()[0]}, []byte{0x01}))
	if got := routeThenRelay(r, pkt); got != RouteActionDeliver {
		t.Fatalf("route() = %v, want %v (allowPacket=true)", got, RouteActionDeliver)
	}
}

func TestRouter_DirectNotNextHop_AllowPacketFalse(t *testing.T) {
	identity := seedIdentity(0x01)
	next := seedIdentity(0x02)
	r := newTestRouter(testRouterOpts{
		identity:    identity,
		allowPacket: func(*meshcore.Packet) bool { return false },
	})

	pkt := mustPacketFromBytes(t, makeDirectPacket(meshcore.PayloadTypeAdvert, []byte{next.Hash()[0]}, []byte{0x01}))
	if got := routeThenRelay(r, pkt); got != RouteActionDrop {
		t.Fatalf("route() = %v, want %v (allowPacket=false)", got, RouteActionDrop)
	}
}

func TestRouter_DirectNotNextHop_AllowPacketDedup(t *testing.T) {
	identity := seedIdentity(0x01)
	next := seedIdentity(0x02)
	r := newTestRouter(testRouterOpts{
		identity:    identity,
		allowPacket: func(*meshcore.Packet) bool { return true },
	})

	data := makeDirectPacket(meshcore.PayloadTypeAdvert, []byte{next.Hash()[0]}, []byte{0x77})
	if got := routeThenRelay(r, mustPacketFromBytes(t, data)); got != RouteActionDeliver {
		t.Fatalf("first route() = %v, want %v", got, RouteActionDeliver)
	}
	if got := routeThenRelay(r, mustPacketFromBytes(t, data)); got != RouteActionDrop {
		t.Fatalf("second route() = %v, want %v (dedup)", got, RouteActionDrop)
	}
}

// makeTracePayload builds tag(4) auth(4) flags(1) followed by the hash list.
func makeTracePayload(flags byte, hashes ...byte) []byte {
	payload := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, flags}
	return append(payload, hashes...)
}

func TestRouter_TraceRelayed(t *testing.T) {
	identity := seedIdentity(0x01)
	other := seedIdentity(0x02)
	var sent [][]byte
	var priority uint8
	r := newTestRouter(testRouterOpts{
		identity:     identity,
		allowForward: func(*meshcore.Packet) bool { return true },
		sendDirect: func(data []byte, p uint8) error {
			sent = append(sent, append([]byte(nil), data...))
			priority = p
			return nil
		},
	})

	payload := makeTracePayload(0x00, identity.Hash()[0], other.Hash()[0])
	pkt := mustPacketFromBytes(t, makeDirectPacket(meshcore.PayloadTypeTrace, nil, payload))
	pkt.SNR = -2.5

	if got := r.route(pkt); got != RouteActionForward {
		t.Fatalf("route() = %v, want %v", got, RouteActionForward)
	}
	if len(sent) != 1 {
		t.Fatalf("send count = %d, want 1", len(sent))
	}
	if priority != TracePriority {
		t.Fatalf("priority = %d, want %d", priority, TracePriority)
	}
	if pkt.PathLength != 1 {
		t.Fatalf("PathLength = %d, want 1", pkt.PathLength)
	}
	if !bytes.Equal(pkt.Path, []byte{0xF6}) {
		t.Fatalf("Path = %x, want f6 (SNR -2.5 dB in quarter-dB units)", pkt.Path)
	}

	relayed := mustPacketFromBytes(t, sent[0])
	if !bytes.Equal(relayed.Path, []byte{0xF6}) || relayed.PathLength != 1 {
		t.Fatalf("relayed path = %x len = %d, want f6 / 1", relayed.Path, relayed.PathLength)
	}
	if !bytes.Equal(relayed.Payload, payload) {
		t.Fatalf("relayed payload = %x, want %x", relayed.Payload, payload)
	}
}

func TestRouter_TraceEndOfPathDelivered(t *testing.T) {
	identity := seedIdentity(0x01)
	sends := 0
	r := newTestRouter(testRouterOpts{
		identity:     identity,
		allowForward: func(*meshcore.Packet) bool { return true },
		send:         func([]byte, uint8) error { sends++; return nil },
	})

	payload := makeTracePayload(0x00, identity.Hash()[0])
	pkt := mustPacketFromBytes(t, makeDirectPacket(meshcore.PayloadTypeTrace, []byte{0x11}, payload))
	if got := r.route(pkt); got != RouteActionDeliver {
		t.Fatalf("route() = %v, want %v", got, RouteActionDeliver)
	}
	if sends != 0 {
		t.Fatalf("sends = %d, want 0", sends)
	}
	if pkt.PathLength != 1 {
		t.Fatalf("PathLength = %d, want 1 (unchanged)", pkt.PathLength)
	}
}

func TestRouter_TraceNotForUs(t *testing.T) {
	other := seedIdentity(0x02)
	sends := 0
	r := newTestRouter(testRouterOpts{
		identity:     seedIdentity(0x01),
		allowForward: func(*meshcore.Packet) bool { return true },
		send:         func([]byte, uint8) error { sends++; return nil },
	})

	pkt := mustPacketFromBytes(t, makeDirectPacket(meshcore.PayloadTypeTrace, nil, makeTracePayload(0x00, other.Hash()[0])))
	if got := r.route(pkt); got != RouteActionDrop {
		t.Fatalf("route() = %v, want %v", got, RouteActionDrop)
	}
	if sends != 0 {
		t.Fatalf("sends = %d, want 0", sends)
	}
}

func TestRouter_TraceDedup(t *testing.T) {
	identity := seedIdentity(0x01)
	sends := 0
	r := newTestRouter(testRouterOpts{
		identity:     identity,
		allowForward: func(*meshcore.Packet) bool { return true },
		sendDirect:   func([]byte, uint8) error { sends++; return nil },
	})

	data := makeDirectPacket(meshcore.PayloadTypeTrace, nil, makeTracePayload(0x00, identity.Hash()[0], 0x99))
	if got := r.route(mustPacketFromBytes(t, data)); got != RouteActionForward {
		t.Fatalf("first route() = %v, want %v", got, RouteActionForward)
	}
	if got := r.route(mustPacketFromBytes(t, data)); got != RouteActionDrop {
		t.Fatalf("second route() = %v, want %v", got, RouteActionDrop)
	}
	if sends != 1 {
		t.Fatalf("sends = %d, want 1", sends)
	}
}

func TestRouter_TraceMultiByteHashes(t *testing.T) {
	identity := seedIdentity(0x01)
	hash := identity.PublicKeyBytes()
	sends := 0
	r := newTestRouter(testRouterOpts{
		identity:     identity,
		allowForward: func(*meshcore.Packet) bool { return true },
		sendDirect:   func([]byte, uint8) error { sends++; return nil },
	})

	// flags=1 → 2-byte hashes; one hop already walked, so our entry is the second.
	payload := makeTracePayload(0x01, 0xAA, 0xBB, hash[0], hash[1])
	pkt := mustPacketFromBytes(t, makeDirectPacket(meshcore.PayloadTypeTrace, []byte{0x7F}, payload))
	if got := r.route(pkt); got != RouteActionForward {
		t.Fatalf("route() = %v, want %v", got, RouteActionForward)
	}
	if sends != 1 || pkt.PathLength != 2 {
		t.Fatalf("sends = %d PathLength = %d, want 1 / 2", sends, pkt.PathLength)
	}
}

func TestRouter_AllowForwardConsultedLast(t *testing.T) {
	identity := seedIdentity(0x01)
	newRouter := func(asked *bool) *router {
		return newTestRouter(testRouterOpts{
			identity:     identity,
			allowForward: func(*meshcore.Packet) bool { *asked = true; return true },
			send:         func([]byte, uint8) error { return nil },
		})
	}

	var asked bool
	r := newRouter(&asked)
	routeThenRelay(r, mustPacketFromBytes(t, makeFloodPacket(meshcore.PayloadTypeControl, []byte{0x01})))
	if asked {
		t.Fatal("allowForward consulted for a non-forwardable payload type")
	}

	asked = false
	r = newRouter(&asked)
	path := bytes.Repeat([]byte{0xAA, 0xBB}, meshcore.MaxPathSize/2)
	full := makePacketWithPath(meshcore.RouteTypeFlood, meshcore.PayloadTypeGrpTxt, 0x60, path, []byte{0x01, 0x02, 0x03, 0x04})
	routeThenRelay(r, mustPacketFromBytes(t, full))
	if asked {
		t.Fatal("allowForward consulted for a full path")
	}

	asked = false
	r = newRouter(&asked)
	routeThenRelay(r, mustPacketFromBytes(t, makeFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{0x01, 0x02, 0x03, 0x04})))
	if !asked {
		t.Fatal("allowForward not consulted for a forwardable flood packet")
	}
}

func TestRouter_FloodMarkedDoNotRetransmit(t *testing.T) {
	sends := 0
	r := newTestRouter(testRouterOpts{
		identity:     seedIdentity(0x01),
		allowForward: func(*meshcore.Packet) bool { return true },
		send:         func([]byte, uint8) error { sends++; return nil },
	})

	pkt := mustPacketFromBytes(t, makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0x01, 0x02, 0x03, 0x04, 0x05}))
	pkt.MarkDoNotRetransmit()
	if got := routeThenRelay(r, pkt); got != RouteActionDeliver {
		t.Fatalf("route() = %v, want %v", got, RouteActionDeliver)
	}
	if sends != 0 {
		t.Fatalf("sends = %d, want 0", sends)
	}
}

func multiAckDirect(path []byte, crc []byte, remaining uint8) []byte {
	mp := meshcore.MultiPart{Remaining: remaining, WrappedType: meshcore.PayloadTypeAck, WrappedPayload: crc}
	payload, err := mp.ToBytes()
	if err != nil {
		panic(err)
	}
	return makeDirectPacket(meshcore.PayloadTypeMultiPart, path, payload)
}

func TestRouter_ForwardMultipartDirect(t *testing.T) {
	identity := seedIdentity(0x01)
	other := seedIdentity(0x02)
	var sent [][]byte
	r := newTestRouter(testRouterOpts{
		identity:     identity,
		allowForward: func(*meshcore.Packet) bool { return true },
		extraAcks:    func() uint8 { return 1 },
		sendDirect: func(data []byte, _ uint8) error {
			sent = append(sent, append([]byte(nil), data...))
			return nil
		},
	})

	crc := []byte{1, 2, 3, 4}
	path := []byte{identity.Hash()[0], other.Hash()[0]}
	pkt := mustPacketFromBytes(t, multiAckDirect(path, crc, 2))
	if got := r.route(pkt); got != RouteActionForward {
		t.Fatalf("route() = %v, want %v", got, RouteActionForward)
	}
	if len(sent) != 2 {
		t.Fatalf("sent %d packets, want 2 (one multipart copy, one plain ACK)", len(sent))
	}

	for i, want := range []byte{meshcore.PayloadTypeMultiPart, meshcore.PayloadTypeAck} {
		out := mustPacketFromBytes(t, sent[i])
		if out.PayloadType() != want {
			t.Errorf("packet %d payload type = %d, want %d", i, out.PayloadType(), want)
		}
		if out.RouteType() != meshcore.RouteTypeDirect {
			t.Errorf("packet %d route type = %d, want direct", i, out.RouteType())
		}
		if !bytes.Equal(out.Path, []byte{other.Hash()[0]}) {
			t.Errorf("packet %d path = %x, want our hash removed", i, out.Path)
		}
	}
	if inner := mustPacketFromBytes(t, sent[1]); !bytes.Equal(inner.Payload, crc) {
		t.Errorf("plain ACK payload = %x, want %x", inner.Payload, crc)
	}
}

func TestRouter_MultipartACKDedup(t *testing.T) {
	identity := seedIdentity(0x01)
	other := seedIdentity(0x02)
	sends := 0
	r := newTestRouter(testRouterOpts{
		identity:     identity,
		allowForward: func(*meshcore.Packet) bool { return true },
		sendDirect:   func([]byte, uint8) error { sends++; return nil },
	})

	crc := []byte{1, 2, 3, 4}
	path := []byte{identity.Hash()[0], other.Hash()[0]}

	if got := r.route(mustPacketFromBytes(t, multiAckDirect(path, crc, 2))); got != RouteActionForward {
		t.Fatalf("first copy = %v, want %v", got, RouteActionForward)
	}
	if got := r.route(mustPacketFromBytes(t, multiAckDirect(path, crc, 1))); got != RouteActionDrop {
		t.Fatalf("second copy = %v, want %v", got, RouteActionDrop)
	}
	if got := r.route(mustPacketFromBytes(t, makeDirectPacket(meshcore.PayloadTypeAck, path, crc))); got != RouteActionForward {
		t.Fatalf("plain ACK = %v, want %v", got, RouteActionForward)
	}
	if sends != 2 {
		t.Fatalf("sends = %d, want 2 (one per distinct hash)", sends)
	}
}

func TestRouter_TooShortDropped(t *testing.T) {
	cases := []struct {
		name string
		typ  byte
		min  int
	}{
		{"ACK", meshcore.PayloadTypeAck, 4},
		{"PATH", meshcore.PayloadTypePath, 5},
		{"REQ", meshcore.PayloadTypeReq, 5},
		{"RESPONSE", meshcore.PayloadTypeResponse, 5},
		{"TXT_MSG", meshcore.PayloadTypeTxtMsg, 5},
		{"ANON_REQ", meshcore.PayloadTypeAnonReq, 36},
		{"GRP_TXT", meshcore.PayloadTypeGrpTxt, 4},
		{"GRP_DATA", meshcore.PayloadTypeGrpData, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sends := 0
			r := newTestRouter(testRouterOpts{
				identity:     seedIdentity(0x01),
				allowForward: func(*meshcore.Packet) bool { return true },
				send:         func([]byte, uint8) error { sends++; return nil },
			})
			short := mustPacketFromBytes(t, makeFloodPacket(tc.typ, bytes.Repeat([]byte{0x05}, tc.min-1)))
			if got := routeThenRelay(r, short); got != RouteActionDrop {
				t.Fatalf("%d bytes: route() = %v, want %v", tc.min-1, got, RouteActionDrop)
			}
			if sends != 0 || r.dedup.Contains(short) {
				t.Fatalf("%d bytes: sends = %d, seen = %v, want neither", tc.min-1, sends, r.dedup.Contains(short))
			}
			if got := routeThenRelay(r, mustPacketFromBytes(t, makeFloodPacket(tc.typ, bytes.Repeat([]byte{0x05}, tc.min)))); got != RouteActionDeliver {
				t.Fatalf("%d bytes: route() = %v, want %v", tc.min, got, RouteActionDeliver)
			}
		})
	}
}

func TestRouter_DirectOnlyTypes(t *testing.T) {
	r := newTestRouter(testRouterOpts{identity: seedIdentity(0x01)})
	cases := []struct {
		name string
		data []byte
		want RouteAction
	}{
		{"zero-hop RAW_CUSTOM", makeDirectPacket(meshcore.PayloadTypeRawCustom, nil, []byte{0x01}), RouteActionDeliver},
		{"flood RAW_CUSTOM", makeFloodPacket(meshcore.PayloadTypeRawCustom, []byte{0x02}), RouteActionDrop},
		{"zero-hop CONTROL without 0x80", makeDirectPacket(meshcore.PayloadTypeControl, nil, []byte{0x01}), RouteActionDrop},
		{"flood CONTROL", makeFloodPacket(meshcore.PayloadTypeControl, []byte{0x80}), RouteActionDrop},
		{"flood TRACE", makeFloodPacket(meshcore.PayloadTypeTrace, makeTracePayload(0x00)), RouteActionDrop},
		{"zero-hop unknown type", makeDirectPacket(0x0C, nil, []byte{0x01}), RouteActionDrop},
	}
	for _, tc := range cases {
		if got := r.route(mustPacketFromBytes(t, tc.data)); got != tc.want {
			t.Errorf("%s: route() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRouter_TraceEndAndZeroHopControlNotDeduped(t *testing.T) {
	r := newTestRouter(testRouterOpts{identity: seedIdentity(0x01)})
	for _, data := range [][]byte{
		makeDirectPacket(meshcore.PayloadTypeTrace, []byte{0x11}, makeTracePayload(0x00, 0x22)),
		makeDirectPacket(meshcore.PayloadTypeControl, nil, []byte{0x80, 0x01}),
	} {
		for i := range 2 {
			if got := r.route(mustPacketFromBytes(t, data)); got != RouteActionDeliver {
				t.Fatalf("copy %d of %x: route() = %v, want %v", i, data, got, RouteActionDeliver)
			}
		}
	}
}
