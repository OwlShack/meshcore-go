package client

import (
	"context"
	"sync"

	"github.com/OwlShack/meshcore-go/companion"
)

// DefaultModemPriority is the default TX-queue priority for outgoing packets.
const DefaultModemPriority uint8 = 1

// CompanionModem adapts a companion Client to the node.Modem interface.
type CompanionModem struct {
	client *Client
	ctx    context.Context
	cancel context.CancelFunc

	// Priority is the TX-queue priority for outgoing packets.
	Priority uint8

	mu    sync.Mutex
	dataH func(data []byte, snr float32, rssi int8, hasSignalInfo bool)

	outboundMu sync.RWMutex
	outboundH  []func([]byte)

	unsubscribe func()
}

// NewCompanionModem creates a CompanionModem backed by the given Client, with ctx bounding outbound sends.
func NewCompanionModem(ctx context.Context, c *Client) *CompanionModem {
	ctx, cancel := context.WithCancel(ctx)
	m := &CompanionModem{
		client:   c,
		ctx:      ctx,
		cancel:   cancel,
		Priority: DefaultModemPriority,
	}
	m.unsubscribe = c.OnPush(companion.PushLogRxData, m.onLogRxData)
	return m
}

func (m *CompanionModem) AddOutboundHandler(h func([]byte)) {
	m.outboundMu.Lock()
	m.outboundH = append(m.outboundH, h)
	m.outboundMu.Unlock()
}

// SendData transmits a fully formed mesh packet through the companion device.
func (m *CompanionModem) SendData(data []byte) error {
	m.outboundMu.RLock()
	handlers := m.outboundH
	m.outboundMu.RUnlock()
	for _, h := range handlers {
		h(data)
	}
	return m.client.SendRawPacket(m.ctx, m.Priority, data)
}

// SetDataHandler registers the callback invoked for each incoming raw mesh packet.
func (m *CompanionModem) SetDataHandler(h func(data []byte, snr float32, rssi int8, hasSignalInfo bool)) {
	m.mu.Lock()
	m.dataH = h
	m.mu.Unlock()
}

// Close unblocks pending sends and detaches from the Client's pushes; it does not close the Client.
func (m *CompanionModem) Close() {
	m.cancel()
	m.unsubscribe()
}

func (m *CompanionModem) onLogRxData(resp companion.Response) {
	rx, ok := resp.Data.(companion.PushLogRxDataResponse)
	if !ok {
		return
	}
	m.mu.Lock()
	h := m.dataH
	m.mu.Unlock()
	if h != nil {
		h(rx.Raw, rx.LastSNR, rx.LastRSSI, true)
	}
}
