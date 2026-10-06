package node

import (
	"bytes"
	"crypto/ed25519"
	"sync"
	"testing"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

type mockRadio struct {
	mu       sync.Mutex
	sent     [][]byte
	dataH    func(*meshcore.Packet)
	rawDataH func([]byte, float32, int8, bool)
}

func (m *mockRadio) SendData(data []byte) error {
	m.mu.Lock()
	m.sent = append(m.sent, data)
	m.mu.Unlock()
	return nil
}

func (m *mockRadio) SetDataHandler(h func(*meshcore.Packet))               { m.dataH = h }
func (m *mockRadio) SetRawDataHandler(h func([]byte, float32, int8, bool)) { m.rawDataH = h }
func (m *mockRadio) AddOutboundHandler(h func([]byte))                     {}
func (m *mockRadio) Close() error                                          { return nil }

func (m *mockRadio) inject(data []byte) {
	pkt, err := meshcore.PacketFromBytes(data)
	if err != nil {
		if m.rawDataH != nil {
			m.rawDataH(data, 0, 0, false)
		}
		return
	}
	if m.dataH != nil {
		m.dataH(pkt)
	}
	if m.rawDataH != nil {
		m.rawDataH(data, 0, 0, false)
	}
}

func (m *mockRadio) injectWithSignal(data []byte, snr float32, rssi int8) {
	pkt, err := meshcore.PacketFromBytes(data)
	if err != nil {
		if m.rawDataH != nil {
			m.rawDataH(data, snr, rssi, true)
		}
		return
	}
	pkt.SNR = snr
	pkt.RSSI = rssi
	pkt.HasSignalInfo = true
	if m.dataH != nil {
		m.dataH(pkt)
	}
	if m.rawDataH != nil {
		m.rawDataH(data, snr, rssi, true)
	}
}

func (m *mockRadio) sentData() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, len(m.sent))
	copy(out, m.sent)
	return out
}

var _ Radio = (*mockRadio)(nil)

// pathLen=0x00 means hashCount=0, hashSize=1 → 0 path bytes.
func makeFloodPacket(payloadType byte, payload []byte) []byte {
	header := meshcore.MakeHeader(meshcore.RouteTypeFlood, payloadType, 0)
	out := []byte{header, 0x00}
	out = append(out, payload...)
	return out
}

func seedIdentity(seedByte byte) meshcore.LocalIdentity {
	var seed [ed25519.SeedSize]byte
	seed[0] = seedByte
	return meshcore.NewLocalIdentityFromSeed(seed)
}

func TestNode_HandlerDispatch(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x01), radio)

	var received []*meshcore.Packet
	n.OnPacket(meshcore.PayloadTypeAdvert, func(pkt *meshcore.Packet) {
		received = append(received, pkt)
	})

	radio.inject(makeFloodPacket(meshcore.PayloadTypeAdvert, []byte{0x01, 0x02}))

	if len(received) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(received))
	}
	if received[0].PayloadType() != meshcore.PayloadTypeAdvert {
		t.Errorf("payload type = %d, want ADVERT", received[0].PayloadType())
	}
}

func TestNode_MultipleHandlersSameType(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x02), radio)

	callCount := 0
	n.OnPacket(meshcore.PayloadTypeAdvert, func(_ *meshcore.Packet) { callCount++ })
	n.OnPacket(meshcore.PayloadTypeAdvert, func(_ *meshcore.Packet) { callCount++ })

	radio.inject(makeFloodPacket(meshcore.PayloadTypeAdvert, []byte{0x01}))

	if callCount != 2 {
		t.Errorf("expected 2 handler calls, got %d", callCount)
	}
}

func TestNode_UnregisteredTypeDropped(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x03), radio)

	called := false
	n.OnPacket(meshcore.PayloadTypeAdvert, func(_ *meshcore.Packet) { called = true })

	// TXT_MSG: dest(1) + src(1) + mac(2) + encrypted
	radio.inject(makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0xCC, 0x01, 0x00, 0x00, 0xFF}))

	if called {
		t.Error("advert handler should not fire for TXT_MSG packet")
	}
}

func TestNode_SendPacket(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x05), radio)

	pkt := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeAdvert, 0),
		PathLength: 0x00,
		Path:       []byte{},
		Payload:    []byte{0xDE, 0xAD},
	}

	if err := n.SendPacket(pkt); err != nil {
		t.Fatalf("SendPacket error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	sent := radio.sentData()
	if len(sent) != 1 {
		t.Fatalf("expected 1 sent packet, got %d", len(sent))
	}
}

func TestNode_StopPreventsDispatch(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x06), radio)

	called := false
	n.OnPacket(meshcore.PayloadTypeAdvert, func(_ *meshcore.Packet) { called = true })

	n.Stop()
	radio.inject(makeFloodPacket(meshcore.PayloadTypeAdvert, []byte{0x01}))

	if called {
		t.Error("handler should not fire after Stop")
	}
}

func TestNode_StopIdempotent(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x07), radio)
	n.Stop()
	n.Stop()
}

func TestNode_Identity(t *testing.T) {
	id := seedIdentity(0x08)
	radio := &mockRadio{}
	n := New(id, radio)

	if n.Identity().PublicKey() != id.PublicKey() {
		t.Error("identity mismatch")
	}
}

func TestNode_SetIdentity(t *testing.T) {
	idA := seedIdentity(0x08)
	idB := seedIdentity(0x09)
	radio := &mockRadio{}
	n := New(idA, radio)

	if n.Identity().PublicKey() != idA.PublicKey() {
		t.Fatal("initial identity mismatch")
	}

	n.SetIdentity(idB)

	if n.Identity().PublicKey() != idB.PublicKey() {
		t.Error("identity not updated after SetIdentity")
	}
	if n.Identity().PublicKey() == idA.PublicKey() {
		t.Error("identity still matches old identity")
	}
}

func TestNode_DataCopied(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x09), radio)

	var receivedPayload []byte
	n.OnPacket(meshcore.PayloadTypeAdvert, func(pkt *meshcore.Packet) {
		receivedPayload = append([]byte{}, pkt.Payload...)
	})

	data := makeFloodPacket(meshcore.PayloadTypeAdvert, []byte{0xAA, 0xBB})
	radio.inject(data)

	data[len(data)-1] = 0x00

	if len(receivedPayload) < 1 {
		t.Fatal("no payload received")
	}
	if receivedPayload[len(receivedPayload)-1] != 0xBB {
		t.Error("received payload was not copied — mutation leaked through")
	}
}

func TestNode_SignalMetadataOnPacket(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x0B), radio)

	var got *meshcore.Packet
	n.OnPacket(meshcore.PayloadTypeAdvert, func(pkt *meshcore.Packet) {
		got = pkt
	})

	radio.injectWithSignal(makeFloodPacket(meshcore.PayloadTypeAdvert, []byte{0x01, 0x02}), -7, -85)

	if got == nil {
		t.Fatal("expected packet")
	}
	if got.SNR != -7 {
		t.Errorf("SNR = %g, want -7", got.SNR)
	}
	if got.RSSI != -85 {
		t.Errorf("RSSI = %d, want -85", got.RSSI)
	}
}

func TestNode_AllPayloadTypes(t *testing.T) {
	types := []struct {
		name       string
		payloadTyp byte
	}{
		{"REQ", meshcore.PayloadTypeReq},
		{"RESPONSE", meshcore.PayloadTypeResponse},
		{"TXT_MSG", meshcore.PayloadTypeTxtMsg},
		{"ACK", meshcore.PayloadTypeAck},
		{"ADVERT", meshcore.PayloadTypeAdvert},
		{"GRP_TXT", meshcore.PayloadTypeGrpTxt},
		{"GRP_DATA", meshcore.PayloadTypeGrpData},
		{"ANON_REQ", meshcore.PayloadTypeAnonReq},
		{"PATH", meshcore.PayloadTypePath},
		{"TRACE", meshcore.PayloadTypeTrace},
		{"MULTI_PART", meshcore.PayloadTypeMultiPart},
		{"CONTROL", meshcore.PayloadTypeControl},
		{"RAW_CUSTOM", meshcore.PayloadTypeRawCustom},
	}

	for _, tc := range types {
		t.Run(tc.name, func(t *testing.T) {
			radio := &mockRadio{}
			n := New(seedIdentity(0x0A), radio)

			called := false
			n.OnPacket(tc.payloadTyp, func(_ *meshcore.Packet) { called = true })

			radio.inject(makeFloodPacket(tc.payloadTyp, []byte{0x01, 0x02, 0x03, 0x04}))

			if !called {
				t.Errorf("handler not called for payload type %s (0x%02X)", tc.name, tc.payloadTyp)
			}
		})
	}
}

func TestNode_NotifyACK(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x10), radio)
	defer n.Stop()

	var ackRTT time.Duration
	var ackFired bool
	crc := uint32(0xDEADBEEF)

	n.ExpectACK(crc, 5*time.Second, func(rtt time.Duration) {
		ackFired = true
		ackRTT = rtt
	}, func() {
		t.Error("timeout should not fire")
	})

	time.Sleep(10 * time.Millisecond)
	n.NotifyACK(crc)
	time.Sleep(10 * time.Millisecond)

	if !ackFired {
		t.Fatal("ACK callback not fired via NotifyACK")
	}
	if ackRTT < 10*time.Millisecond {
		t.Errorf("RTT = %v, expected >= 10ms", ackRTT)
	}
}

func TestNode_NotifyACK_NoMatch(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x11), radio)
	defer n.Stop()

	timedOut := make(chan struct{}, 1)
	n.ExpectACK(0x11111111, 100*time.Millisecond, func(time.Duration) {
		t.Error("onACK should not fire for wrong CRC")
	}, func() {
		timedOut <- struct{}{}
	})

	n.NotifyACK(0x22222222) // wrong CRC

	select {
	case <-timedOut:
		// expected
	case <-time.After(2 * time.Second):
		t.Error("expected timeout since NotifyACK had wrong CRC")
	}
}

func TestNode_SendPacketDelayed(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x12), radio)
	defer n.Stop()

	pkt := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeAck, 0),
		PathLength: 0,
		Payload:    []byte{0x01, 0x02, 0x03, 0x04},
	}

	err := n.SendPacketDelayed(pkt, PrioritySend, 0)
	if err != nil {
		t.Fatalf("SendPacketDelayed error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	sent := radio.sentData()
	if len(sent) != 1 {
		t.Fatalf("expected 1 sent packet, got %d", len(sent))
	}
}

func TestNode_SendPacketDelayed_MarksSeen(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x13), radio)
	defer n.Stop()

	pkt := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeGrpTxt, 0),
		PathLength: 0,
		Payload:    []byte{0x10, 0x20, 0x30, 0x40, 0x50},
	}

	_ = n.SendPacketDelayed(pkt, PrioritySend, 0)

	data, _ := pkt.ToBytes()
	radio.inject(data)
	time.Sleep(100 * time.Millisecond)

	var handlerCalled bool
	n.OnPacket(meshcore.PayloadTypeGrpTxt, func(p *meshcore.Packet) {
		handlerCalled = true
	})

	radio.inject(data)
	time.Sleep(50 * time.Millisecond)

	if handlerCalled {
		t.Error("handler should not fire for deduped packet")
	}
}

func TestNode_TxStats(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x14), radio)
	defer n.Stop()

	pkt := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeAdvert, 0),
		PathLength: 0,
		Payload:    []byte{0x01, 0x02, 0x03, 0x04},
	}
	_ = n.SendPacket(pkt)

	time.Sleep(100 * time.Millisecond)

	stats := n.TxStats()
	if stats.Sent < 1 {
		t.Errorf("TxStats.Sent = %d, want >= 1", stats.Sent)
	}
}

type txCall struct {
	data     []byte
	priority uint8
	delay    time.Duration
}

// mockTxRadio records enqueued transmissions without sending them.
type mockTxRadio struct {
	mockRadio
	mu    sync.Mutex
	calls []txCall
}

func (m *mockTxRadio) Enqueue(data []byte, priority uint8, delay time.Duration) bool {
	m.mu.Lock()
	m.calls = append(m.calls, txCall{data: data, priority: priority, delay: delay})
	m.mu.Unlock()
	return true
}

func (m *mockTxRadio) TxQueueLen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockTxRadio) enqueued() []txCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]txCall, len(m.calls))
	copy(out, m.calls)
	return out
}

var _ TxRadio = (*mockTxRadio)(nil)

func ackRelayBytes() []byte {
	header := meshcore.MakeHeader(meshcore.RouteTypeDirect, meshcore.PayloadTypeAck, 0)
	return append([]byte{header, 0x00}, 1, 2, 3, 4)
}

func ackRelayPacket() *meshcore.Packet {
	return &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeDirect, meshcore.PayloadTypeAck, 0),
		PathLength: 0,
		Payload:    []byte{1, 2, 3, 4},
	}
}

func floodRelayPacket() *meshcore.Packet {
	return &meshcore.Packet{
		Header:  meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeTxtMsg, 0),
		Payload: []byte{0xaa},
	}
}

func TestNode_RelayDelayDefaults(t *testing.T) {
	radio := &mockTxRadio{}
	n := New(seedIdentity(1), radio)
	defer n.Stop()

	n.router.forward(floodRelayPacket(), PriorityFloodRelay, n.router.floodDelay, 0)
	n.router.routeDirectRecvAcks(ackRelayPacket(), 0)

	calls := radio.enqueued()
	if len(calls) != 2 {
		t.Fatalf("got %d enqueues, want 2", len(calls))
	}
	for i, c := range calls {
		if c.delay != 0 {
			t.Errorf("call %d delay = %v, want 0 without an airtime estimator", i, c.delay)
		}
	}
}

func TestNode_FloodRelayDelayUsesAirtimeEstimator(t *testing.T) {
	radio := &mockTxRadio{}
	n := New(seedIdentity(1), radio, WithAirtimeEstimator(func(int) uint32 { return 100 }))
	defer n.Stop()

	maxDelay := 5 * time.Duration(100*52/50/2) * time.Millisecond
	for range 20 {
		n.router.forward(floodRelayPacket(), PriorityFloodRelay, n.router.floodDelay, 0)
	}
	for i, c := range radio.enqueued() {
		if c.delay < 0 || c.delay > maxDelay {
			t.Errorf("call %d delay = %v, want within [0, %v]", i, c.delay, maxDelay)
		}
	}
}

func TestNode_RelayDelayOverrides(t *testing.T) {
	radio := &mockTxRadio{}
	var gotLen int
	var gotAirtime uint32
	n := New(seedIdentity(1), radio,
		WithAirtimeEstimator(func(int) uint32 { return 40 }),
		WithFloodRetransmitDelay(func(packetLen int, estAirtimeMs uint32) time.Duration {
			gotLen, gotAirtime = packetLen, estAirtimeMs
			return 700 * time.Millisecond
		}),
		WithDirectRetransmitDelay(func(int, uint32) time.Duration { return 250 * time.Millisecond }),
	)
	defer n.Stop()

	n.router.forward(floodRelayPacket(), PriorityFloodRelay, n.router.floodDelay, 0)
	n.router.forward(ackRelayPacket(), PriorityDirectRelay, n.router.directDelay, 0)

	calls := radio.enqueued()
	if len(calls) != 2 {
		t.Fatalf("got %d enqueues, want 2", len(calls))
	}
	if calls[0].delay != 700*time.Millisecond {
		t.Errorf("flood delay = %v, want 700ms", calls[0].delay)
	}
	if calls[1].delay != 250*time.Millisecond {
		t.Errorf("direct delay = %v, want 250ms", calls[1].delay)
	}
	if gotLen != 3 || gotAirtime != 40 {
		t.Errorf("flood hook got (len=%d, airtime=%d), want (3, 40)", gotLen, gotAirtime)
	}
}

func TestNode_ExtraAckTransmitCount(t *testing.T) {
	radio := &mockTxRadio{}
	n := New(seedIdentity(1), radio,
		WithDirectRetransmitDelay(func(int, uint32) time.Duration { return 100 * time.Millisecond }),
		WithExtraAckTransmitCount(func() uint8 { return 2 }),
	)
	defer n.Stop()

	n.router.routeDirectRecvAcks(ackRelayPacket(), 0)

	calls := radio.enqueued()
	want := []struct {
		delay     time.Duration
		remaining uint8
		payload   byte
	}{
		{400 * time.Millisecond, 2, meshcore.PayloadTypeMultiPart},
		{800 * time.Millisecond, 1, meshcore.PayloadTypeMultiPart},
		{800 * time.Millisecond, 0, meshcore.PayloadTypeAck},
	}
	if len(calls) != len(want) {
		t.Fatalf("got %d enqueues, want %d", len(calls), len(want))
	}
	for i, w := range want {
		pkt, err := meshcore.PacketFromBytes(calls[i].data)
		if err != nil {
			t.Fatalf("copy %d: %v", i, err)
		}
		if calls[i].delay != w.delay {
			t.Errorf("copy %d delay = %v, want %v", i, calls[i].delay, w.delay)
		}
		if calls[i].priority != PriorityDirectRelay {
			t.Errorf("copy %d priority = %d, want %d", i, calls[i].priority, PriorityDirectRelay)
		}
		if pkt.PayloadType() != w.payload {
			t.Fatalf("copy %d payload type = %d, want %d", i, pkt.PayloadType(), w.payload)
		}
		if w.payload == meshcore.PayloadTypeAck {
			if !bytes.Equal(pkt.Payload, []byte{1, 2, 3, 4}) {
				t.Errorf("plain ACK payload = %x, want 01020304", pkt.Payload)
			}
			continue
		}
		mp, err := meshcore.MultiPartFromBytes(pkt.Payload)
		if err != nil {
			t.Fatalf("copy %d multipart: %v", i, err)
		}
		if mp.Remaining != w.remaining || mp.WrappedType != meshcore.PayloadTypeAck {
			t.Errorf("copy %d = remaining %d type %d, want %d / %d", i, mp.Remaining, mp.WrappedType, w.remaining, meshcore.PayloadTypeAck)
		}
		if !bytes.Equal(mp.WrappedPayload, []byte{1, 2, 3, 4}) {
			t.Errorf("copy %d wrapped payload = %x, want 01020304", i, mp.WrappedPayload)
		}
	}
}

// Each copy must hash differently or a relay's dedup collapses them.
func TestNode_ExtraAckCopiesHashDistinctly(t *testing.T) {
	radio := &mockTxRadio{}
	n := New(seedIdentity(1), radio, WithExtraAckTransmitCount(func() uint8 { return 2 }))
	defer n.Stop()

	n.router.routeDirectRecvAcks(ackRelayPacket(), 0)

	seen := map[[meshcore.PacketHashSize]byte]bool{}
	for i, c := range radio.enqueued() {
		pkt, err := meshcore.PacketFromBytes(c.data)
		if err != nil {
			t.Fatalf("copy %d: %v", i, err)
		}
		h := pkt.PacketHash()
		if seen[h] {
			t.Fatalf("copy %d repeats packet hash %x", i, h)
		}
		seen[h] = true
	}
	if len(seen) != 3 {
		t.Fatalf("got %d distinct hashes, want 3", len(seen))
	}
}

func TestNode_ExtraAckTransmitCountUnset(t *testing.T) {
	radio := &mockTxRadio{}
	n := New(seedIdentity(1), radio)
	defer n.Stop()

	n.router.routeDirectRecvAcks(ackRelayPacket(), 0)
	if got := len(radio.enqueued()); got != 1 {
		t.Fatalf("got %d enqueues, want 1", got)
	}
}
