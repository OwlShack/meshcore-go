package node

import (
	"encoding/binary"
	"sync"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

const (
	ackSweepInterval = 500 * time.Millisecond

	TimeoutBaseMilli       uint32  = 500
	FloodTimeoutFactor     float32 = 16.0
	DirectPerHopFactor     float32 = 6.0
	DirectPerHopExtraMilli uint32  = 250
)

// CalcFloodTimeout returns the estimated ACK timeout for a flood-routed
// packet given its over-the-air time in milliseconds.
func CalcFloodTimeout(airtimeMs uint32) time.Duration {
	ms := TimeoutBaseMilli + uint32(FloodTimeoutFactor*float32(airtimeMs))
	return time.Duration(ms) * time.Millisecond
}

// CalcDirectTimeout returns the estimated ACK timeout for a direct-routed
// packet given its airtime and the number of hops in the path.
func CalcDirectTimeout(airtimeMs uint32, hopCount uint8) time.Duration {
	perHop := uint32(DirectPerHopFactor*float32(airtimeMs)) + DirectPerHopExtraMilli
	ms := TimeoutBaseMilli + perHop*uint32(hopCount+1)
	return time.Duration(ms) * time.Millisecond
}

type pendingACK struct {
	deadline  time.Time
	onACK     func(roundTrip time.Duration)
	onTimeout func()
	sentAt    time.Time
}

type ackTracker struct {
	mu      sync.Mutex
	pending map[uint32]*pendingACK
	done    <-chan struct{}
}

func newACKTracker(done <-chan struct{}) *ackTracker {
	at := &ackTracker{
		pending: make(map[uint32]*pendingACK),
		done:    done,
	}
	go at.sweepLoop()
	return at
}

func (at *ackTracker) expect(crc uint32, timeout time.Duration, onACK func(time.Duration), onTimeout func()) {
	now := time.Now()
	at.mu.Lock()
	at.pending[crc] = &pendingACK{
		deadline:  now.Add(timeout),
		onACK:     onACK,
		onTimeout: onTimeout,
		sentAt:    now,
	}
	at.mu.Unlock()
}

func (at *ackTracker) cancel(crc uint32) {
	at.mu.Lock()
	delete(at.pending, crc)
	at.mu.Unlock()
}

func (at *ackTracker) handleACK(pkt *meshcore.Packet) {
	if len(pkt.Payload) < 4 {
		return
	}
	crc := binary.LittleEndian.Uint32(pkt.Payload[:4])
	at.notifyCRC(crc)
}

func (at *ackTracker) notifyCRC(crc uint32) {
	at.mu.Lock()
	p, ok := at.pending[crc]
	if ok {
		delete(at.pending, crc)
	}
	at.mu.Unlock()

	if ok && p.onACK != nil {
		p.onACK(time.Since(p.sentAt))
	}
}

func (at *ackTracker) sweepLoop() {
	ticker := time.NewTicker(ackSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-at.done:
			return
		case <-ticker.C:
			at.sweep()
		}
	}
}

func (at *ackTracker) sweep() {
	now := time.Now()

	at.mu.Lock()
	var expired []*pendingACK
	for crc, p := range at.pending {
		if now.After(p.deadline) {
			expired = append(expired, p)
			delete(at.pending, crc)
		}
	}
	at.mu.Unlock()

	for _, p := range expired {
		if p.onTimeout != nil {
			p.onTimeout()
		}
	}
}

func (at *ackTracker) pendingCount() int {
	at.mu.Lock()
	defer at.mu.Unlock()
	return len(at.pending)
}
