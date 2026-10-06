package node

import (
	"sync"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

const retrySweepInterval = 200 * time.Millisecond

type pendingRetry struct {
	pkt        *meshcore.Packet
	hash       [meshcore.PacketHashSize]byte
	retries    int
	maxRetries int
	timeout    time.Duration
	deadline   time.Time
	onConfirm  func()
	onFail     func()
}

type retryTracker struct {
	mu      sync.Mutex
	pending map[[meshcore.PacketHashSize]byte]*pendingRetry
	sendFn  func(*meshcore.Packet) error
	done    <-chan struct{}
}

func newRetryTracker(sendFn func(*meshcore.Packet) error, done <-chan struct{}) *retryTracker {
	rt := &retryTracker{
		pending: make(map[[meshcore.PacketHashSize]byte]*pendingRetry),
		sendFn:  sendFn,
		done:    done,
	}
	go rt.sweepLoop()
	return rt
}

func (rt *retryTracker) track(pkt *meshcore.Packet, maxRetries int, timeout time.Duration, onConfirm, onFail func()) {
	hash := pkt.PacketHash()
	rt.mu.Lock()
	rt.pending[hash] = &pendingRetry{
		pkt:        pkt,
		hash:       hash,
		maxRetries: maxRetries,
		timeout:    timeout,
		deadline:   time.Now().Add(timeout),
		onConfirm:  onConfirm,
		onFail:     onFail,
	}
	rt.mu.Unlock()
}

func (rt *retryTracker) handlePacket(pkt *meshcore.Packet) bool {
	hash := pkt.PacketHash()
	rt.mu.Lock()
	p, ok := rt.pending[hash]
	if ok {
		delete(rt.pending, hash)
	}
	rt.mu.Unlock()

	if !ok {
		return false
	}

	if p.onConfirm != nil {
		p.onConfirm()
	}
	return true
}

func (rt *retryTracker) sweepLoop() {
	ticker := time.NewTicker(retrySweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-rt.done:
			return
		case <-ticker.C:
			rt.sweep()
		}
	}
}

func (rt *retryTracker) sweep() {
	now := time.Now()

	rt.mu.Lock()
	var expired []*pendingRetry
	var resend []*meshcore.Packet
	for hash, p := range rt.pending {
		if now.After(p.deadline) {
			p.retries++
			if p.retries >= p.maxRetries {
				expired = append(expired, p)
				delete(rt.pending, hash)
			} else {
				p.deadline = now.Add(p.timeout)
				resend = append(resend, p.pkt)
			}
		}
	}
	rt.mu.Unlock()

	for _, pkt := range resend {
		_ = rt.sendFn(pkt)
	}
	for _, p := range expired {
		if p.onFail != nil {
			p.onFail()
		}
	}
}

func (rt *retryTracker) pendingCount() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return len(rt.pending)
}
