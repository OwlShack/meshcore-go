package node

import (
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

	radio.inject(makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0x01, 0x02, 0x03}))
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

	radio.inject(makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0x01, 0x02, 0x03}))
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

	radio.inject(makeFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{0x0A}))
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

	radio.inject(makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0x01}))
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
		{"below threshold", 10 * time.Millisecond, makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0x01})},
		{"direct not held", time.Hour, makeDirectPacket(meshcore.PayloadTypeTxtMsg, nil, []byte{0x02})},
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

	radio.inject(makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0x01}))
	for i := range 5 {
		radio.inject(makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0x02, byte(i)}))
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
