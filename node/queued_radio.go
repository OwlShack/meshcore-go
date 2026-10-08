package node

import (
	"log/slog"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

// QueuedRadio wraps a plain Radio with a serializing transmit queue.
type QueuedRadio struct {
	inner Radio
	tx    *txEngine
}

type queuedRadioConfig struct {
	txOpts []txEngineOption
}

type QueuedRadioOption func(*queuedRadioConfig)

func WithQueuedRadioLogger(l *slog.Logger) QueuedRadioOption {
	return func(c *queuedRadioConfig) {
		c.txOpts = append(c.txOpts, withTxLogger(l))
	}
}

func WithQueuedRadioErrorHandler(h func(error)) QueuedRadioOption {
	return func(c *queuedRadioConfig) {
		c.txOpts = append(c.txOpts, withTxErrorHandler(h))
	}
}

func WithQueuedRadioMaxQueue(size int) QueuedRadioOption {
	return func(c *queuedRadioConfig) {
		c.txOpts = append(c.txOpts, withTxMaxQueue(size))
	}
}

func WithQueuedRadioAirtimeBudget(budget *airtimeBudget) QueuedRadioOption {
	return func(c *queuedRadioConfig) {
		c.txOpts = append(c.txOpts, withTxAirtimeBudget(budget))
	}
}

func WithQueuedRadioRetryable(fn func(error) bool) QueuedRadioOption {
	return func(c *queuedRadioConfig) {
		c.txOpts = append(c.txOpts, withTxRetryable(fn))
	}
}

func NewQueuedRadio(inner Radio, done chan struct{}, opts ...QueuedRadioOption) *QueuedRadio {
	cfg := queuedRadioConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	q := &QueuedRadio{inner: inner}
	q.tx = newTxEngine(inner.SendData, done, cfg.txOpts...)
	return q
}

func (q *QueuedRadio) Enqueue(data []byte, priority uint8, delay time.Duration) bool {
	return q.tx.enqueue(data, priority, delay)
}

func (q *QueuedRadio) SendData(data []byte) error {
	if !q.Enqueue(data, PrioritySend, 0) {
		return ErrTxQueueFull
	}
	return nil
}

func (q *QueuedRadio) TxQueueLen() int {
	return q.tx.queueLen()
}

func (q *QueuedRadio) TxStats() TxStats {
	return q.tx.stats()
}

func (q *QueuedRadio) SetDataHandler(h func(*meshcore.Packet)) { q.inner.SetDataHandler(h) }
func (q *QueuedRadio) SetRawDataHandler(h func([]byte, float32, int8, bool)) {
	q.inner.SetRawDataHandler(h)
}
func (q *QueuedRadio) AddOutboundHandler(h func([]byte)) { q.inner.AddOutboundHandler(h) }
func (q *QueuedRadio) Close() error                      { return q.inner.Close() }

var _ TxRadio = (*QueuedRadio)(nil)
