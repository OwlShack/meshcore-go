package node

import (
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

// RouteAction describes what the router decided to do with a received packet.
type RouteAction int

const (
	RouteActionDrop    RouteAction = iota // duplicate or unroutable — discard, no local delivery
	RouteActionDeliver                    // deliver to local handlers (a flood packet may be re-flooded afterwards)
	RouteActionForward                    // relayed to the next hop only — NOT delivered locally
)

// TracePriority is the transmit priority used for relayed TRACE packets.
const TracePriority uint8 = 5

// router requires a non-nil node.
type router struct {
	dedup meshcore.DedupCache
	node  *Node
	stats routeCounters

	send        func(data []byte, priority uint8, delay time.Duration) error
	floodDelay  func(dataLen int) time.Duration
	directDelay func(dataLen int) time.Duration
}

// extraAckSpacing is the fixed gap firmware leaves between copies of a relayed direct ACK.
const extraAckSpacing = 300 * time.Millisecond

// route returns the action for an incoming packet; a direct relay consumes pkt's
// path in place, and a flood packet still needs relayFlood after local dispatch.
func (r *router) route(pkt *meshcore.Packet) RouteAction {
	switch {
	case pkt.IsRouteFlood():
		r.stats.floodReceived.Add(1)
	case pkt.IsRouteDirect():
		r.stats.directReceived.Add(1)
		if pkt.PayloadType() == meshcore.PayloadTypeTrace {
			return r.routeTrace(pkt)
		}
		if pkt.PathHashCount() > 0 {
			return r.routeDirect(pkt)
		}
	}

	if r.seen(pkt) {
		return RouteActionDrop
	}
	return RouteActionDeliver
}

func (r *router) routeDirect(pkt *meshcore.Packet) RouteAction {
	// payload[0]&0x80 marks a zero-hop-only control packet.
	if pkt.PayloadType() == meshcore.PayloadTypeControl && len(pkt.Payload) > 0 && pkt.Payload[0]&0x80 != 0 {
		return RouteActionDrop
	}

	hashes := pkt.PathHashes()
	if len(hashes) == 0 {
		return RouteActionDeliver
	}

	if r.node.Identity().IsHashMatch(hashes[0]) && r.canForward(pkt) {
		if pkt.PayloadType() == meshcore.PayloadTypeMultiPart {
			return r.forwardMultipartDirect(pkt)
		}
		if pkt.IsMarkedDoNotRetransmit() || r.seen(pkt) {
			return RouteActionDrop
		}
		pkt.RemoveFirstPathHash()
		r.stats.directRelays.Add(1)
		if pkt.PayloadType() == meshcore.PayloadTypeAck {
			r.routeDirectRecvAcks(pkt, 0)
		} else {
			r.forward(pkt, PriorityDirectRelay, r.directDelay, 0)
		}
		return RouteActionForward
	}

	if r.node.canAcceptPacket(pkt) {
		if r.seen(pkt) {
			return RouteActionDrop
		}
		return RouteActionDeliver
	}
	return RouteActionDrop
}

// routeTrace relays a direct TRACE to the next hop on its path, appending our SNR.
func (r *router) routeTrace(pkt *meshcore.Packet) RouteAction {
	if int(pkt.PathLength) >= meshcore.MaxPathSize || len(pkt.Payload) < traceHeaderSize {
		return RouteActionDrop
	}

	pathSz := pkt.Payload[8] & 0x03
	hashSize := 1 << pathSz
	hashes := len(pkt.Payload) - traceHeaderSize
	// path_len*hashSize exceeds 255 for a long path, so the offset must be 16-bit.
	offset := int(uint16(pkt.PathLength) << pathSz)

	if offset >= hashes {
		if r.seen(pkt) {
			return RouteActionDrop
		}
		return RouteActionDeliver
	}
	if offset+hashSize > hashes {
		return RouteActionDrop
	}

	next := pkt.Payload[traceHeaderSize+offset : traceHeaderSize+offset+hashSize]
	if !r.node.Identity().IsHashMatch(next) || !r.canForward(pkt) || pkt.IsMarkedDoNotRetransmit() || r.seen(pkt) {
		return RouteActionDrop
	}

	pkt.Path = append(pkt.Path[:len(pkt.Path):len(pkt.Path)], byte(int8(pkt.SNR*4)))
	pkt.PathLength++
	r.stats.directRelays.Add(1)
	r.forward(pkt, TracePriority, r.directDelay, 0)
	return RouteActionForward
}

// traceHeaderSize is the tag(4) + auth(4) + flags(1) prefix of a TRACE payload.
const traceHeaderSize = 9

// multiAckMinPayload is the wrapper byte plus the 4-byte ACK CRC.
const multiAckMinPayload = 5

// unwrapMultiAck returns the inner ACK of a multipart-wrapped ACK, keeping the
// MULTIPART header so its dedup hash stays distinct from the trailing plain ACK.
func unwrapMultiAck(pkt *meshcore.Packet) (inner *meshcore.Packet, remaining uint8, ok bool) {
	if len(pkt.Payload) < multiAckMinPayload {
		return nil, 0, false
	}
	mp, err := meshcore.MultiPartFromBytes(pkt.Payload)
	if err != nil || mp.WrappedType != meshcore.PayloadTypeAck {
		return nil, 0, false
	}
	inner = pkt.Clone()
	inner.Payload = mp.WrappedPayload
	return inner, mp.Remaining, true
}

// forwardMultipartDirect relays the ACK carried by a direct multipart packet.
func (r *router) forwardMultipartDirect(pkt *meshcore.Packet) RouteAction {
	inner, remaining, ok := unwrapMultiAck(pkt)
	if !ok || r.seen(inner) {
		return RouteActionDrop
	}
	inner.RemoveFirstPathHash()
	r.stats.directRelays.Add(1)
	r.routeDirectRecvAcks(inner, time.Duration(remaining+1)*extraAckSpacing)
	return RouteActionForward
}

// routeDirectRecvAcks relays an ACK as extraAckCount spaced multipart copies
// followed by a plain ACK.
func (r *router) routeDirectRecvAcks(pkt *meshcore.Packet, delay time.Duration) {
	if pkt.IsMarkedDoNotRetransmit() {
		return
	}
	ackLen := 2 + len(pkt.Path) + len(pkt.Payload)
	for extra := r.extraAckCount(); extra > 0; extra-- {
		delay += r.relayDelay(r.directDelay, ackLen) + extraAckSpacing
		mp := meshcore.MultiPart{Remaining: extra, WrappedType: meshcore.PayloadTypeAck, WrappedPayload: pkt.Payload}
		payload, err := mp.ToBytes()
		if err != nil {
			r.node.dispatchError(err)
			continue
		}
		r.forwardAck(pkt, meshcore.PayloadTypeMultiPart, payload, delay)
	}
	r.forwardAck(pkt, meshcore.PayloadTypeAck, pkt.Payload, delay)
}

func (r *router) forwardAck(src *meshcore.Packet, payloadType byte, payload []byte, delay time.Duration) {
	out := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeDirect, payloadType, 0),
		PathLength: src.PathLength,
		Path:       src.Path,
		Payload:    payload,
	}
	r.forward(out, PriorityDirectRelay, nil, delay)
}

func (r *router) extraAckCount() uint8 {
	if r.node.extraAcks == nil {
		return 0
	}
	return r.node.extraAcks()
}

// relayFlood re-floods pkt unless it was consumed locally; call it after local dispatch.
func (r *router) relayFlood(pkt *meshcore.Packet) {
	if !pkt.IsRouteFlood() || pkt.IsMarkedDoNotRetransmit() || !r.floodForwardable(pkt) {
		return
	}

	hashSize := int(pkt.PathHashSize())
	newCount := int(pkt.PathHashCount()) + 1
	if newCount*hashSize > meshcore.MaxPathSize {
		return
	}

	if !r.canForward(pkt) {
		return
	}

	clone := pkt.Clone()
	pk := r.node.Identity().PublicKey()
	clone.AppendPathHash(pk[:])
	r.stats.floodRelays.Add(1)
	r.forward(clone, uint8(newCount), r.floodDelay, 0)
}

// floodForwardable reports whether this payload type is flood-relayed.
func (r *router) floodForwardable(pkt *meshcore.Packet) bool {
	switch pkt.PayloadType() {
	case meshcore.PayloadTypeAck, meshcore.PayloadTypePath, meshcore.PayloadTypeReq,
		meshcore.PayloadTypeResponse, meshcore.PayloadTypeTxtMsg, meshcore.PayloadTypeAnonReq,
		meshcore.PayloadTypeGrpTxt, meshcore.PayloadTypeGrpData:
		return true
	case meshcore.PayloadTypeAdvert:
		adv, err := meshcore.AdvertFromBytes(pkt.Payload)
		return err == nil && !r.node.Identity().Matches(adv.PublicKey) && adv.Verify()
	}
	return false
}

// seen reports whether the packet is a duplicate, recording it either way.
func (r *router) seen(pkt *meshcore.Packet) bool {
	if !r.dedup.HasSeen(pkt) {
		return false
	}
	if pkt.IsRouteFlood() {
		r.stats.floodDuplicates.Add(1)
	} else {
		r.stats.directDuplicates.Add(1)
	}
	return true
}

func (r *router) canForward(pkt *meshcore.Packet) bool {
	r.node.cbMu.RLock()
	f := r.node.allowForward
	r.node.cbMu.RUnlock()
	if f == nil {
		return false
	}
	return f(pkt)
}

func (r *router) relayDelay(f func(int) time.Duration, dataLen int) time.Duration {
	if f == nil {
		return 0
	}
	return f(dataLen)
}

// forward transmits pkt at priority, delayed by delayFor(len)+extra.
func (r *router) forward(pkt *meshcore.Packet, priority uint8, delayFor func(int) time.Duration, extra time.Duration) {
	if r.send == nil {
		return
	}
	data, err := pkt.ToBytes()
	if err == nil {
		err = r.send(data, priority, r.relayDelay(delayFor, len(data))+extra)
	}
	if err != nil {
		r.node.dispatchError(err)
	}
}
