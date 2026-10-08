package node

import (
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

type Radio interface {
	SendData(data []byte) error
	SetDataHandler(func(pkt *meshcore.Packet))
	SetRawDataHandler(func(data []byte, snr float32, rssi int8, hasSignalInfo bool))
	AddOutboundHandler(h func([]byte))
	Close() error
}

// TxRadio is a Radio that supports prioritized enqueuing and serialized
// transmission.
type TxRadio interface {
	Radio
	// Enqueue adds data to the transmit queue at the given priority (lower is
	// sent first), postponed by delay, returning false if the queue is full.
	Enqueue(data []byte, priority uint8, delay time.Duration) bool
	// TxQueueLen returns the number of entries currently in the queue.
	TxQueueLen() int
}
