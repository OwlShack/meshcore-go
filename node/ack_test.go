package node

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

func makeACKPacket(crc uint32) []byte {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, crc)
	return makeFloodPacket(meshcore.PayloadTypeAck, payload)
}

func TestACKTracker_ExpectAndReceive(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xB0), radio)
	defer n.Stop()

	var gotTrip time.Duration
	var called atomic.Bool

	n.ExpectACK(0xAABBCCDD, 5*time.Second, func(trip time.Duration) {
		gotTrip = trip
		called.Store(true)
	}, nil)

	time.Sleep(10 * time.Millisecond)
	radio.inject(makeACKPacket(0xAABBCCDD))
	time.Sleep(10 * time.Millisecond)

	if !called.Load() {
		t.Fatal("onACK callback not called")
	}
	if gotTrip < 10*time.Millisecond {
		t.Errorf("round trip = %v, want >= 10ms", gotTrip)
	}
	if n.acks.pendingCount() != 0 {
		t.Errorf("pending count = %d, want 0", n.acks.pendingCount())
	}
}

func TestACKTracker_Timeout(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xB1), radio)
	defer n.Stop()

	var timedOut atomic.Bool
	n.ExpectACK(0x11223344, 200*time.Millisecond, nil, func() {
		timedOut.Store(true)
	})

	time.Sleep(800 * time.Millisecond)

	if !timedOut.Load() {
		t.Fatal("onTimeout callback not called")
	}
	if n.acks.pendingCount() != 0 {
		t.Errorf("pending count = %d, want 0", n.acks.pendingCount())
	}
}

func TestACKTracker_Cancel(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xB2), radio)
	defer n.Stop()

	var called atomic.Bool
	n.ExpectACK(0x55667788, 200*time.Millisecond, func(_ time.Duration) {
		called.Store(true)
	}, func() {
		called.Store(true)
	})

	n.CancelACK(0x55667788)
	time.Sleep(10 * time.Millisecond)
	radio.inject(makeACKPacket(0x55667788))
	time.Sleep(500 * time.Millisecond)

	if called.Load() {
		t.Error("callback should not fire after cancel")
	}
}

func TestACKTracker_UnmatchedACKIgnored(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xB3), radio)
	defer n.Stop()

	var called atomic.Bool
	n.ExpectACK(0x11111111, 5*time.Second, func(_ time.Duration) {
		called.Store(true)
	}, nil)

	radio.inject(makeACKPacket(0x99999999))
	time.Sleep(10 * time.Millisecond)

	if called.Load() {
		t.Error("callback should not fire for unmatched CRC")
	}
	if n.acks.pendingCount() != 1 {
		t.Errorf("pending count = %d, want 1", n.acks.pendingCount())
	}
}

func TestACKTracker_DuplicateACKOnlyFiresOnce(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xB4), radio)
	defer n.Stop()

	var count atomic.Int32
	n.ExpectACK(0xDEADBEEF, 5*time.Second, func(_ time.Duration) {
		count.Add(1)
	}, nil)

	radio.inject(makeACKPacket(0xDEADBEEF))
	radio.inject(makeACKPacket(0xDEADBEEF))
	time.Sleep(20 * time.Millisecond)

	if count.Load() != 1 {
		t.Errorf("onACK called %d times, want 1", count.Load())
	}
}

func TestACKTracker_UserHandlerStillFires(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xB5), radio)
	defer n.Stop()

	var userGot *meshcore.Packet
	n.OnPacket(meshcore.PayloadTypeAck, func(pkt *meshcore.Packet) {
		userGot = pkt
	})

	n.ExpectACK(0xCAFEBABE, 5*time.Second, nil, nil)
	radio.inject(makeACKPacket(0xCAFEBABE))
	time.Sleep(10 * time.Millisecond)

	if userGot == nil {
		t.Fatal("user ACK handler should still fire")
	}
}

func TestACKTracker_Concurrent(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0xB6), radio)
	defer n.Stop()

	var wg sync.WaitGroup
	var received atomic.Int32

	for i := range uint32(50) {
		crc := 0xA0000000 + i
		wg.Add(1)
		n.ExpectACK(crc, 5*time.Second, func(_ time.Duration) {
			received.Add(1)
			wg.Done()
		}, nil)
	}

	for i := range uint32(50) {
		crc := 0xA0000000 + i
		radio.inject(makeACKPacket(crc))
	}

	wg.Wait()
	if received.Load() != 50 {
		t.Errorf("received %d ACKs, want 50", received.Load())
	}
}

func TestCalcFloodTimeout(t *testing.T) {
	got := CalcFloodTimeout(100)
	want := time.Duration(500+1600) * time.Millisecond
	if got != want {
		t.Errorf("CalcFloodTimeout(100) = %v, want %v", got, want)
	}
}

func TestCalcFloodTimeout_ZeroAirtime(t *testing.T) {
	got := CalcFloodTimeout(0)
	want := 500 * time.Millisecond
	if got != want {
		t.Errorf("CalcFloodTimeout(0) = %v, want %v", got, want)
	}
}

func TestCalcDirectTimeout(t *testing.T) {
	got := CalcDirectTimeout(100, 3)
	perHop := uint32(600 + 250)
	wantMs := 500 + perHop*4
	want := time.Duration(wantMs) * time.Millisecond
	if got != want {
		t.Errorf("CalcDirectTimeout(100, 3) = %v, want %v", got, want)
	}
}

func TestCalcDirectTimeout_ZeroHops(t *testing.T) {
	got := CalcDirectTimeout(50, 0)
	perHop := uint32(300 + 250)
	wantMs := 500 + perHop*1
	want := time.Duration(wantMs) * time.Millisecond
	if got != want {
		t.Errorf("CalcDirectTimeout(50, 0) = %v, want %v", got, want)
	}
}
