package node

import (
	"encoding/binary"
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
	n.router.stats.rxAirtimeMs.Add(uint64(n.estAirtime(rawLen(pkt))))
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

// rawLen is the serialised length of pkt.
func rawLen(pkt *meshcore.Packet) int {
	l := 2 + len(pkt.Path) + len(pkt.Payload)
	if pkt.IsTransport() {
		l += 4
	}
	return l
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

	n.receiveDatagram(pkt)
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

	if !adv.Verify() || adv.AppData().Name == "" {
		return
	}

	n.peers.UpdateWithHashSize(adv, pkt.SNR, pkt.RSSI, pkt.HasSignalInfo, pkt.Path, pkt.PathHashSize())
}

// receiveDatagram decrypts a flood or zero-hop datagram addressed to us, marking it do-not-retransmit on success.
func (n *Node) receiveDatagram(pkt *meshcore.Packet) {
	if !pkt.IsRouteFlood() && pkt.PathHashCount() > 0 {
		return
	}
	p := pkt.Payload
	switch pkt.PayloadType() {
	case meshcore.PayloadTypeTxtMsg, meshcore.PayloadTypeReq, meshcore.PayloadTypeResponse, meshcore.PayloadTypePath:
		if !n.Identity().IsHashMatch(p[:1]) {
			return
		}
		for _, peer := range n.peers.LookupByHash(p[1:2]) {
			secret, err := n.secrets.get(peer.Identity)
			if err != nil {
				continue
			}
			plain, err := meshcore.MACThenDecrypt(secret, p[2:])
			if err != nil {
				continue
			}
			if pkt.PayloadType() == meshcore.PayloadTypePath && !n.handlePeerPath(pkt, peer, secret, plain) {
				return
			}
			pkt.MarkDoNotRetransmit()
			return
		}
	case meshcore.PayloadTypeAnonReq:
		if !n.Identity().IsHashMatch(p[:1]) {
			return
		}
		sender, err := meshcore.NewIdentityFromBytes(p[1 : 1+meshcore.PubKeySize])
		if err != nil {
			return
		}
		secret, err := n.secrets.get(sender)
		if err != nil {
			return
		}
		if _, err := meshcore.MACThenDecrypt(secret, p[1+meshcore.PubKeySize:]); err == nil {
			pkt.MarkDoNotRetransmit()
		}
	}
}

// handlePeerPath learns a peer's out path from a decrypted PATH, returning false for a bad encoding.
func (n *Node) handlePeerPath(pkt *meshcore.Packet, peer *Peer, secret, plain []byte) bool {
	pp, err := meshcore.ParsePathPayload(plain)
	if err != nil {
		return false
	}
	n.peers.SetOutPath(peer.Identity.PublicKey(), pp.Path, pp.PathHashSize())
	if pp.ExtraType == meshcore.PayloadTypeAck && len(pp.Extra) >= 4 {
		n.acks.notifyCRC(binary.LittleEndian.Uint32(pp.Extra))
	}
	if pkt.IsRouteFlood() && !n.noReciprocalPath {
		n.sendPathReturn(pkt, peer, secret, pp)
	}
	return true
}

// reciprocalPathDelay is how long a reciprocal path return waits before it is sent.
const reciprocalPathDelay = 500 * time.Millisecond

// sendPathReturn answers a flood PATH with the route it took to us, sent direct along the path it carried.
func (n *Node) sendPathReturn(pkt *meshcore.Packet, peer *Peer, secret []byte, pp *meshcore.PathPayload) {
	body, err := (&meshcore.PathPayload{PathLength: pkt.PathLength, Path: pkt.Path}).ToBytes()
	if err != nil {
		n.dispatchError(err)
		return
	}
	ret, err := meshcore.NewPath(n.Identity(), peer.Identity, body, secret)
	if err != nil {
		n.dispatchError(err)
		return
	}
	payload, err := ret.ToBytes()
	if err != nil {
		n.dispatchError(err)
		return
	}
	out := &meshcore.Packet{Header: meshcore.MakeHeader(meshcore.RouteTypeDirect, meshcore.PayloadTypePath, 0), Payload: payload}
	if err := n.SendDirect(out, pp.Path, pp.PathHashSize(), reciprocalPathDelay); err != nil {
		n.dispatchError(err)
	}
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
