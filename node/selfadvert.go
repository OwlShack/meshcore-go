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
	appBytes, err := n.advertData.ToBytes()
	if err != nil {
		n.dispatchError(err)
		return
	}

	id := n.Identity()
	adv := &meshcore.Advert{
		PublicKey:  id.Identity,
		Timestamp:  uint32(time.Now().Unix()),
		RawAppData: appBytes,
	}
	adv.SignWith(id)

	payload, err := adv.ToBytes()
	if err != nil {
		n.dispatchError(err)
		return
	}

	pkt := &meshcore.Packet{
		Header:     meshcore.MakeHeader(meshcore.RouteTypeFlood, meshcore.PayloadTypeAdvert, 0),
		PathLength: meshcore.PathHashSize - 1,
		Payload:    payload,
	}
	pkt.SetScope(n.regions.Default())

	if err := n.SendPacket(pkt); err != nil {
		n.dispatchError(err)
	}
}
