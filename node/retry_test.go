package node

import (
	"sync/atomic"
	"testing"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

func testGroupPayload(text string) *meshcore.GroupTextPayload {
	return &meshcore.GroupTextPayload{
		Timestamp: 1000,
		Sender:    "test",
		Text:      text,
	}
}

func TestRetryTracker_ConfirmOnEcho(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	var sent atomic.Int32
	sendFn := func(pkt *meshcore.Packet) error {
		sent.Add(1)
		return nil
	}

	rt := newRetryTracker(sendFn, done)

	pkt := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeGrpTxt, 0),
		PathLength: 0x00,
		Path:       []byte{},
		Payload:    []byte{0xAA, 0xBB, 0xCC},
	}

	var confirmed atomic.Int32
	rt.track(pkt, 3, time.Second, func() { confirmed.Add(1) }, nil)

	if rt.pendingCount() != 1 {
		t.Fatalf("pending = %d, want 1", rt.pendingCount())
	}

	echo := &meshcore.Packet{
		Header:     pkt.Header,
		PathLength: pkt.PathLength,
		Path:       []byte{},
		Payload:    append([]byte{}, pkt.Payload...),
	}

	if !rt.handlePacket(echo) {
		t.Fatal("handlePacket should return true for tracked packet")
	}

	if confirmed.Load() != 1 {
		t.Errorf("onConfirm called %d times, want 1", confirmed.Load())
	}
	if rt.pendingCount() != 0 {
		t.Errorf("pending = %d, want 0 after confirm", rt.pendingCount())
	}
}

func TestRetryTracker_RetryOnTimeout(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	var sendCount atomic.Int32
	sendFn := func(pkt *meshcore.Packet) error {
		sendCount.Add(1)
		return nil
	}

	rt := newRetryTracker(sendFn, done)

	pkt := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeGrpTxt, 0),
		PathLength: 0x00,
		Path:       []byte{},
		Payload:    []byte{0x01, 0x02},
	}

	rt.track(pkt, 3, 100*time.Millisecond, nil, nil)

	time.Sleep(500 * time.Millisecond)

	if sendCount.Load() < 1 {
		t.Errorf("expected at least 1 retry send, got %d", sendCount.Load())
	}
}

func TestRetryTracker_FailAfterMaxRetries(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	sendFn := func(pkt *meshcore.Packet) error { return nil }

	rt := newRetryTracker(sendFn, done)

	pkt := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeGrpTxt, 0),
		PathLength: 0x00,
		Path:       []byte{},
		Payload:    []byte{0x01},
	}

	var failed atomic.Int32
	rt.track(pkt, 2, 100*time.Millisecond, nil, func() { failed.Add(1) })

	time.Sleep(800 * time.Millisecond)

	if failed.Load() != 1 {
		t.Errorf("onFail called %d times, want 1", failed.Load())
	}
	if rt.pendingCount() != 0 {
		t.Errorf("pending = %d, want 0 after failure", rt.pendingCount())
	}
}

func TestRetryTracker_UnrelatedPacketIgnored(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	rt := newRetryTracker(
		func(*meshcore.Packet) error { return nil },
		done,
	)

	pkt := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeGrpTxt, 0),
		PathLength: 0x00,
		Path:       []byte{},
		Payload:    []byte{0x01},
	}
	rt.track(pkt, 3, time.Second, nil, nil)

	unrelated := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeGrpTxt, 0),
		PathLength: 0x00,
		Path:       []byte{},
		Payload:    []byte{0xFF},
	}

	if rt.handlePacket(unrelated) {
		t.Fatal("should not match unrelated packet")
	}
	if rt.pendingCount() != 1 {
		t.Errorf("pending = %d, want 1", rt.pendingCount())
	}
}

func TestNode_SendGroupText_Confirmed(t *testing.T) {
	radio := &mockRadio{}
	ch := testChannel("retry-test")
	n := New(seedIdentity(0xE0), radio, WithChannels(ch))
	defer n.Stop()

	var result GroupSendResult
	var resultCalled atomic.Int32

	err := n.SendGroupText(ch, testGroupPayload("hello"), 1, time.Second, 3, func(r GroupSendResult) {
		result = r
		resultCalled.Add(1)
	})
	if err != nil {
		t.Fatalf("SendGroupText error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	sent := radio.sentData()
	if len(sent) == 0 {
		t.Fatal("expected packet to be sent")
	}

	pkt, err := meshcore.PacketFromBytes(sent[0])
	if err != nil {
		t.Fatalf("parsing sent packet: %v", err)
	}
	radio.inject(sent[0])

	time.Sleep(50 * time.Millisecond)

	if resultCalled.Load() != 1 {
		t.Fatalf("onResult called %d times, want 1", resultCalled.Load())
	}
	if !result.Confirmed {
		t.Error("expected Confirmed=true")
	}

	_ = pkt
}

func TestNode_SendGroupText_Failed(t *testing.T) {
	radio := &mockRadio{}
	ch := testChannel("retry-fail")
	n := New(seedIdentity(0xE1), radio, WithChannels(ch))
	defer n.Stop()

	var result GroupSendResult
	var resultCalled atomic.Int32

	err := n.SendGroupText(ch, testGroupPayload("fail"), 1, 100*time.Millisecond, 2, func(r GroupSendResult) {
		result = r
		resultCalled.Add(1)
	})
	if err != nil {
		t.Fatalf("SendGroupText error: %v", err)
	}

	// No echo — wait for retries to exhaust
	time.Sleep(800 * time.Millisecond)

	if resultCalled.Load() != 1 {
		t.Fatalf("onResult called %d times, want 1", resultCalled.Load())
	}
	if result.Confirmed {
		t.Error("expected Confirmed=false")
	}
}

func TestNode_SendGroupText_EchoNotForwardedOrDelivered(t *testing.T) {
	radio := &mockRadio{}
	ch := testChannel("echo")
	n := New(seedIdentity(0xE2), radio, WithChannels(ch),
		WithAllowForwardHandler(func(*meshcore.Packet) bool { return true }))
	defer n.Stop()

	delivered := 0
	n.OnPacket(meshcore.PayloadTypeGrpTxt, func(*meshcore.Packet) { delivered++ })

	var confirmed atomic.Int32
	err := n.SendGroupText(ch, testGroupPayload("test"), 1, time.Second, 3, func(r GroupSendResult) {
		if r.Confirmed {
			confirmed.Add(1)
		}
	})
	if err != nil {
		t.Fatalf("SendGroupText error: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	sent := radio.sentData()
	if len(sent) != 1 {
		t.Fatalf("sent %d packets, want 1", len(sent))
	}
	pkt, _ := meshcore.PacketFromBytes(sent[0])
	if !n.router.dedup.HasSeen(pkt) {
		t.Error("own group text should be marked seen at send time")
	}

	// Echo: once via the tracker, once more after it has been confirmed.
	radio.inject(sent[0])
	radio.inject(sent[0])
	time.Sleep(200 * time.Millisecond)

	if confirmed.Load() != 1 {
		t.Errorf("confirmed %d times, want 1", confirmed.Load())
	}
	if delivered != 0 {
		t.Errorf("echo delivered to local handlers %d times, want 0", delivered)
	}
	if got := len(radio.sentData()); got != 1 {
		t.Errorf("sent %d packets after echo, want 1 (echo must not be re-flooded)", got)
	}
}

func TestTextAckHashInput_ExcludesAttemptTail(t *testing.T) {
	self := seedIdentity(0xE3)
	ts := time.Unix(1700000000, 0)
	text := []byte("hello")

	p4 := meshcore.BuildTextPlaintextWithAttempt(ts, 0, text, 4)
	if len(p4) != 5+len(text)+2 {
		t.Fatalf("attempt-4 plaintext len = %d, want %d", len(p4), 5+len(text)+2)
	}
	receiverView := meshcore.BuildTextPlaintextWithAttempt(ts, 0, text, 0)

	want := meshcore.CalcAckHash(receiverView, self.PublicKeyBytes())
	got := meshcore.CalcAckHash(textAckHashInput(p4, len(text)), self.PublicKeyBytes())
	if got != want {
		t.Fatalf("attempt-4 ACK CRC = %08x, want %08x", got, want)
	}
	if meshcore.CalcAckHash(p4, self.PublicKeyBytes()) == want {
		t.Fatal("hashing the full tailed plaintext should differ; test is vacuous")
	}
}

func TestNode_SendTextMessage_ACKConfirmed(t *testing.T) {
	radio := &mockRadio{}
	sender := seedIdentity(0xF0)
	peer := seedIdentity(0xF1)
	n := New(sender, radio)
	defer n.Stop()

	var result DMSendResult
	var resultCalled atomic.Int32

	text := []byte("hello DM")
	now := time.Now()
	err := n.SendTextMessage(peer.Identity, text, 0, now, nil, 1, time.Second, func(r DMSendResult) {
		result = r
		resultCalled.Add(1)
	})
	if err != nil {
		t.Fatalf("SendTextMessage error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	sent := radio.sentData()
	if len(sent) == 0 {
		t.Fatal("expected packet to be sent")
	}

	pkt, err := meshcore.PacketFromBytes(sent[0])
	if err != nil {
		t.Fatalf("parsing sent packet: %v", err)
	}
	if pkt.PayloadType() != meshcore.PayloadTypeTxtMsg {
		t.Fatalf("payload type = %d, want TXT_MSG", pkt.PayloadType())
	}
	if pkt.RouteType() != meshcore.RouteTypeFlood {
		t.Fatalf("route type = %d, want FLOOD (no path provided)", pkt.RouteType())
	}

	plaintext := meshcore.BuildTextPlaintext(now, 0, text)
	ackCRC := meshcore.CalcAckHash(plaintext, sender.PublicKeyBytes())

	radio.inject(makeACKPacket(ackCRC))
	time.Sleep(100 * time.Millisecond)

	if resultCalled.Load() != 1 {
		t.Fatalf("onResult called %d times, want 1", resultCalled.Load())
	}
	if !result.Confirmed {
		t.Error("expected Confirmed=true")
	}
	if result.RoundTrip <= 0 {
		t.Error("expected positive RoundTrip")
	}
}

func TestNode_SendTextMessage_FloodRetryExhausted(t *testing.T) {
	radio := &mockRadio{}
	sender := seedIdentity(0xF2)
	peer := seedIdentity(0xF3)
	n := New(sender, radio)
	defer n.Stop()

	var result DMSendResult
	var resultCalled atomic.Int32

	err := n.SendTextMessage(peer.Identity, []byte("fail"), 0, time.Now(), nil, 1, 200*time.Millisecond, func(r DMSendResult) {
		result = r
		resultCalled.Add(1)
	})
	if err != nil {
		t.Fatalf("SendTextMessage error: %v", err)
	}

	// Flood: 3 retries, 100ms each + 500ms ack sweep interval
	time.Sleep(3 * time.Second)

	if resultCalled.Load() != 1 {
		t.Fatalf("onResult called %d times, want 1", resultCalled.Load())
	}
	if result.Confirmed {
		t.Error("expected Confirmed=false")
	}
}

func TestNode_SendTextMessage_DirectEscalatesToFlood(t *testing.T) {
	radio := &mockRadio{}
	sender := seedIdentity(0xF4)
	peer := seedIdentity(0xF5)
	n := New(sender, radio)
	defer n.Stop()

	path := peer.Hash()

	var result DMSendResult
	var resultCalled atomic.Int32

	err := n.SendTextMessage(peer.Identity, []byte("direct"), 0, time.Now(), path, 1, 100*time.Millisecond, func(r DMSendResult) {
		result = r
		resultCalled.Add(1)
	})
	if err != nil {
		t.Fatalf("SendTextMessage error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	sent := radio.sentData()
	if len(sent) == 0 {
		t.Fatal("expected packet to be sent")
	}

	firstPkt, _ := meshcore.PacketFromBytes(sent[0])
	if firstPkt.RouteType() != meshcore.RouteTypeDirect {
		t.Fatalf("first packet route type = %d, want DIRECT", firstPkt.RouteType())
	}

	// Wait for all 5 retries to exhaust (5 * 100ms + sweep overhead)
	time.Sleep(3 * time.Second)

	if resultCalled.Load() != 1 {
		t.Fatalf("onResult called %d times, want 1", resultCalled.Load())
	}
	if result.Confirmed {
		t.Error("expected Confirmed=false after all retries exhausted")
	}

	allSent := radio.sentData()
	if len(allSent) < 2 {
		t.Errorf("expected multiple sends (retries), got %d", len(allSent))
	}

	foundFlood := false
	for i := 1; i < len(allSent); i++ {
		pkt, err := meshcore.PacketFromBytes(allSent[i])
		if err != nil {
			continue
		}
		if pkt.RouteType() == meshcore.RouteTypeFlood {
			foundFlood = true
			break
		}
	}
	if !foundFlood {
		t.Error("expected at least one flood retry after direct retries exhausted")
	}
}

// Distinct payload per attempt, or a relay's dedup drops the retry.
func TestNode_SendTextMessage_RetriesAreUnique(t *testing.T) {
	radio := &mockRadio{}
	sender := seedIdentity(0xF6)
	peer := seedIdentity(0xF7)
	n := New(sender, radio)
	defer n.Stop()

	err := n.SendTextMessage(peer.Identity, []byte("retry me"), 0, time.Now(), nil, 1, 100*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("SendTextMessage error: %v", err)
	}

	// Let the flood retries (3) exhaust.
	time.Sleep(3 * time.Second)

	sent := radio.sentData()
	if len(sent) < 2 {
		t.Fatalf("expected multiple sends, got %d", len(sent))
	}

	hashes := map[[meshcore.PacketHashSize]byte]int{}
	for i, raw := range sent {
		pkt, err := meshcore.PacketFromBytes(raw)
		if err != nil {
			t.Fatalf("parsing sent[%d]: %v", i, err)
		}
		h := pkt.PacketHash()
		if prev, dup := hashes[h]; dup {
			t.Errorf("send %d has the same packet hash as send %d (retries not unique)", i, prev)
		}
		hashes[h] = i
	}
}
