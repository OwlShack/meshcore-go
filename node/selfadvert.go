package node

import (
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

func (n *Node) selfAdvert() {
	if n.advertData == nil {
		return
	}

	n.sendAdvert()

	ticker := time.NewTicker(n.advertInterval)
	defer ticker.Stop()

	for {
		select {
		case <-n.done:
			return
		case <-ticker.C:
			n.sendAdvert()
		}
	}
}

func (n *Node) sendAdvert() {
	adv, err := meshcore.NewAdvert(n.Identity(), *n.advertData, uint32(time.Now().Unix()))
	if err != nil {
		n.dispatchError(err)
		return
	}
	payload, err := adv.ToBytes()
	if err != nil {
		n.dispatchError(err)
		return
	}

	pkt := &meshcore.Packet{
		Header:  meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeAdvert, 0),
		Payload: payload,
	}
	if err := n.SendFlood(pkt, n.regions.Default(), n.advertHashSize, 0); err != nil {
		n.dispatchError(err)
	}
}
