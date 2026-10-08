package node

import (
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
	"github.com/OwlShack/meshcore-go/hardware"
)

const queuedRadioTickInterval = 50 * time.Millisecond

const txBusyBackoff = 200 * time.Millisecond

// TxStats holds runtime counters reported by txEngine.Stats.
type TxStats struct {
	Sent          uint64
	BusyRequeued  uint64
	BusyDropped   uint64
	Failed        uint64
	QueueRejected uint64
	SentFlood     uint64 // flood packets transmitted, relays included
	SentDirect    uint64 // direct packets transmitted, relays included
	AirtimeMs     uint64 // estimated transmit airtime; zero without an airtime estimator

	// FailedInARow counts send attempts since the last success, busy retries included.
	FailedInARow uint64
	// FailingSince is when that streak began, zero when FailedInARow is.
	FailingSince time.Time
}

type txEngine struct {
	mu         sync.Mutex
	queue      *txQueue
	budget     *airtimeBudget
	nextTxTime time.Time
	sendFn     func([]byte) error
	retryable  func(error) bool
	log        *slog.Logger
	errH       func(error)
	done       chan struct{}

	statSent          atomic.Uint64
	statBusyRequeued  atomic.Uint64
	statBusyDropped   atomic.Uint64
	statFailed        atomic.Uint64
	statQueueRejected atomic.Uint64
	statSentFlood     atomic.Uint64
	statSentDirect    atomic.Uint64
	failedInARow      atomic.Uint64
	failingSince      atomic.Int64
}

type txEngineConfig struct {
	maxQueue  int
	log       *slog.Logger
	errH      func(error)
	budget    *airtimeBudget
	retryable func(error) bool
}

type txEngineOption func(*txEngineConfig)

func withTxMaxQueue(size int) txEngineOption {
	return func(c *txEngineConfig) {
		c.maxQueue = size
	}
}

func withTxLogger(l *slog.Logger) txEngineOption {
	return func(c *txEngineConfig) {
		c.log = l
	}
}

func withTxErrorHandler(h func(error)) txEngineOption {
	return func(c *txEngineConfig) {
		c.errH = h
	}
}

func withTxAirtimeBudget(budget *airtimeBudget) txEngineOption {
	return func(c *txEngineConfig) {
		c.budget = budget
	}
}

func withTxRetryable(fn func(error) bool) txEngineOption {
	return func(c *txEngineConfig) {
		c.retryable = fn
	}
}

func newTxEngine(sendFn func([]byte) error, done chan struct{}, opts ...txEngineOption) *txEngine {
	cfg := txEngineConfig{
		maxQueue: DefaultMaxTxQueue,
		log:      slog.Default(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.retryable == nil {
		// The modem still awaits the previous TX_DONE; the packet was never written.
		cfg.retryable = func(err error) bool { return errors.Is(err, hardware.ErrTxPending) }
	}
	e := &txEngine{
		queue:     newTxQueue(cfg.maxQueue),
		budget:    cfg.budget,
		sendFn:    sendFn,
		retryable: cfg.retryable,
		log:       cfg.log,
		errH:      cfg.errH,
		done:      done,
	}
	go e.loop()
	return e
}

func (e *txEngine) enqueue(data []byte, priority uint8, delay time.Duration) bool {
	e.mu.Lock()
	ok := e.queue.add(data, priority, time.Now().Add(delay))
	e.mu.Unlock()
	if !ok {
		e.statQueueRejected.Add(1)
	}
	return ok
}

func (e *txEngine) stats() TxStats {
	st := TxStats{
		Sent:          e.statSent.Load(),
		BusyRequeued:  e.statBusyRequeued.Load(),
		BusyDropped:   e.statBusyDropped.Load(),
		Failed:        e.statFailed.Load(),
		QueueRejected: e.statQueueRejected.Load(),
		SentFlood:     e.statSentFlood.Load(),
		SentDirect:    e.statSentDirect.Load(),
	}
	e.mu.Lock()
	if e.budget != nil {
		st.AirtimeMs = e.budget.totalAirtimeMs
	}
	e.mu.Unlock()
	if n := e.failedInARow.Load(); n > 0 {
		st.FailedInARow = n
		st.FailingSince = time.Unix(0, e.failingSince.Load())
	}
	return st
}

func (e *txEngine) queueLen() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.queue.len()
}

func (e *txEngine) loop() {
	ticker := time.NewTicker(queuedRadioTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.done:
			return
		case <-ticker.C:
			e.drain()
		}
	}
}

func (e *txEngine) drain() {
	for {
		e.mu.Lock()
		now := time.Now()

		if e.budget != nil {
			e.budget.refill(now)
		}

		entry := e.queue.peek(now)
		if entry == nil {
			e.mu.Unlock()
			return
		}

		if e.budget != nil {
			ok, waitMs := e.budget.canSend(e.budget.estimator(meshcore.MaxTransUnit))
			if !ok {
				e.nextTxTime = now.Add(time.Duration(waitMs * float64(time.Millisecond)))
				e.mu.Unlock()
				return
			}
			if now.Before(e.nextTxTime) {
				e.mu.Unlock()
				return
			}
		}

		popped := e.queue.pop(now)
		e.mu.Unlock()

		if popped == nil {
			return
		}

		sendStart := time.Now()
		err := e.sendFn(popped.data)

		if err != nil {
			// Store the start before the count, which stats reads first; drain is the only writer.
			if e.failedInARow.Load() == 0 {
				e.failingSince.Store(sendStart.UnixNano())
			}
			e.failedInARow.Add(1)
			if e.retryable != nil && e.retryable(err) {
				e.mu.Lock()
				ok := e.queue.add(popped.data, popped.priority, time.Now().Add(txBusyBackoff))
				e.mu.Unlock()
				if ok {
					e.statBusyRequeued.Add(1)
					e.log.Debug("tx busy, re-enqueued packet", "error", err, "data_len", len(popped.data))
				} else {
					e.statBusyDropped.Add(1)
					e.log.Warn("tx busy, queue full, dropping packet", "error", err, "data_len", len(popped.data))
				}
			} else {
				e.statFailed.Add(1)
				e.log.Warn("tx failed, dropping packet", "error", err, "data_len", len(popped.data))
			}
			if e.errH != nil {
				e.errH(err)
			}
			return
		}

		e.statSent.Add(1)
		if len(popped.data) > 0 {
			if rt := popped.data[0] & meshcore.PacketRouteMask; rt == meshcore.RouteTypeFlood || rt == meshcore.RouteTypeTransportFlood {
				e.statSentFlood.Add(1)
			} else {
				e.statSentDirect.Add(1)
			}
		}
		e.failedInARow.Store(0)

		if e.budget != nil {
			// Wall-clock SendData time includes KISS TXDELAY, CSMA and serial; firmware counts radio time only.
			e.mu.Lock()
			e.budget.deduct(uint64(e.budget.estimator(len(popped.data))))
			delay := e.budget.nextTxDelay()
			if delay > 0 {
				e.nextTxTime = time.Now().Add(delay)
			}
			e.mu.Unlock()
		}
	}
}
