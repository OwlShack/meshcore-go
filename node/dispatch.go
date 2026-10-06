package node

import (
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

// Bounds taken from firmware Dispatcher::checkRecv.
const (
	minRxDelay = 50 * time.Millisecond
	maxRxDelay = 32 * time.Second

	inboundQueueSize = 64
)

// onData runs inline on the radio's read goroutine unless a receive delay is set,
// which routes every packet through the inbound goroutine to preserve arrival order.
func (n *Node) onData(pkt *meshcore.Packet) {
	if n.inbound == nil {
		n.processPacket(pkt)
		return
	}
	if d := n.inboundDelay(pkt); d > 0 {
		time.AfterFunc(d, func() { n.queueInbound(pkt) })
		return
	}
	n.queueInbound(pkt)
}

func (n *Node) inboundDelay(pkt *meshcore.Packet) time.Duration {
	if !pkt.IsRouteFlood() {
		return 0
	}
	data, err := pkt.ToBytes()
	if err != nil {
		return 0
	}
	d := n.rxDelay(pkt, len(data), n.estAirtime(len(data)))
	if d < minRxDelay {
		return 0
	}
	return min(d, maxRxDelay)
}

func (n *Node) queueInbound(pkt *meshcore.Packet) {
	select {
	case n.inbound <- pkt:
	default:
		n.log.Warn("inbound queue full, dropping packet", "type", pkt.PayloadTypeString())
	}
}

// runInbound drains held packets one at a time so handlers never run concurrently.
func (n *Node) runInbound() {
	for {
		select {
		case <-n.done:
			return
		case pkt := <-n.inbound:
			n.processPacket(pkt)
		}
	}
}

func (n *Node) processPacket(pkt *meshcore.Packet) {
	select {
	case <-n.done:
		return
	default:
	}

	if n.retries.handlePacket(pkt) {
		return
	}

	// Catches ACKs passing through us as a relay, which routing would not deliver locally.
	if pkt.IsRouteDirect() && pkt.PayloadType() == meshcore.PayloadTypeAck && pkt.PathHashCount() > 0 {
		n.acks.handleACK(pkt)
	}

	if n.router.route(pkt) != RouteActionDeliver {
		return
	}

	if pkt.PayloadType() == meshcore.PayloadTypeAdvert {
		n.handleAdvert(pkt)
	}

	if pkt.PayloadType() == meshcore.PayloadTypeAck {
		n.acks.handleACK(pkt)
	}

	if pkt.PayloadType() == meshcore.PayloadTypeMultiPart {
		n.handleMultiPartACK(pkt)
	}

	n.dispatchPacket(pkt)
	n.router.relayFlood(pkt)
}

// handleMultiPartACK delivers the ACK carried by a multipart packet addressed to us.
func (n *Node) handleMultiPartACK(pkt *meshcore.Packet) {
	inner, _, ok := unwrapMultiAck(pkt)
	if !ok || n.router.dedup.HasSeen(inner) {
		return
	}
	n.acks.handleACK(inner)
}

func (n *Node) handleAdvert(pkt *meshcore.Packet) {
	adv, err := meshcore.AdvertFromBytes(pkt.Payload)
	if err != nil {
		n.log.Debug("failed to parse advert", "error", err)
		n.dispatchError(err)
		return
	}

	if n.Identity().Matches(adv.PublicKey) {
		return
	}

	if !adv.Verify() {
		return
	}

	n.peers.UpdateWithHashSize(adv, pkt.SNR, pkt.RSSI, pkt.HasSignalInfo, pkt.Path, pkt.PathHashSize())
}

func (n *Node) dispatchPacket(pkt *meshcore.Packet) {
	n.router.stats.delivered.Add(1)
	n.handlerMu.RLock()
	handlers := n.handlers[pkt.PayloadType()]
	n.handlerMu.RUnlock()

	for _, h := range handlers {
		h(pkt)
	}
}

func (n *Node) dispatchError(err error) {
	n.cbMu.RLock()
	h := n.errH
	n.cbMu.RUnlock()
	if h != nil {
		h(err)
	}
}
