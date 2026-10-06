package node

import (
	"testing"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

func TestSelfAdvert_SendsOnStart(t *testing.T) {
	radio := &mockRadio{}
	id := seedIdentity(0x40)
	appData := meshcore.AdvertAppData{Type: "CHAT", Name: "test-node"}

	_ = New(id, radio,
		WithAdvertData(appData),
		WithAdvertInterval(time.Hour),
	)

	time.Sleep(200 * time.Millisecond)

	sent := radio.sentData()
	if len(sent) == 0 {
		t.Fatal("expected at least 1 advert packet sent on start")
	}

	pkt, err := meshcore.PacketFromBytes(sent[0])
	if err != nil {
		t.Fatalf("parsing sent packet: %v", err)
	}
	if pkt.PayloadType() != meshcore.PayloadTypeAdvert {
		t.Errorf("payload type = %d, want ADVERT", pkt.PayloadType())
	}
	if !pkt.IsRouteFlood() {
		t.Errorf("route type = %s, want FLOOD", pkt.RouteTypeString())
	}

	adv, err := meshcore.AdvertFromBytes(pkt.Payload)
	if err != nil {
		t.Fatalf("parsing advert: %v", err)
	}
	if !adv.PublicKey.Matches(id.Identity) {
		t.Error("advert public key does not match node identity")
	}
	if !adv.Verify() {
		t.Error("advert signature verification failed")
	}
	ad := adv.AppData()
	if ad.Name != "test-node" {
		t.Errorf("advert name = %q, want %q", ad.Name, "test-node")
	}
	if ad.Type != "CHAT" {
		t.Errorf("advert type = %q, want %q", ad.Type, "CHAT")
	}
}

func TestSelfAdvert_NoAdvertWithoutData(t *testing.T) {
	radio := &mockRadio{}
	_ = New(seedIdentity(0x41), radio)

	time.Sleep(50 * time.Millisecond)

	if len(radio.sentData()) != 0 {
		t.Error("should not send adverts without WithAdvertData")
	}
}

func TestSelfAdvert_StopsOnNodeStop(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x42), radio,
		WithAdvertData(meshcore.AdvertAppData{Type: "REPEATER", Name: "rpt"}),
		WithAdvertInterval(10*time.Millisecond),
	)

	time.Sleep(50 * time.Millisecond)
	n.Stop()
	time.Sleep(20 * time.Millisecond)

	countAfterStop := len(radio.sentData())
	time.Sleep(50 * time.Millisecond)

	if len(radio.sentData()) != countAfterStop {
		t.Error("adverts continued after Stop()")
	}
}

func TestSelfAdvert_PeriodicSending(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x43), radio,
		WithAdvertData(meshcore.AdvertAppData{Type: "CHAT", Name: "periodic"}),
		WithAdvertInterval(20*time.Millisecond),
	)
	defer n.Stop()

	time.Sleep(80 * time.Millisecond)

	sent := radio.sentData()
	if len(sent) < 3 {
		t.Errorf("expected at least 3 adverts (initial + 2 periodic), got %d", len(sent))
	}
}

func TestNode_AdvertUpdatesLocalPeerTable(t *testing.T) {
	radio := &mockRadio{}
	localID := seedIdentity(0x50)
	n := New(localID, radio)

	peerID := peerIdentity(0x51)
	adv := makeSignedAdvert(peerID, 100, "remote-peer")
	payload, err := adv.ToBytes()
	if err != nil {
		t.Fatalf("advert ToBytes: %v", err)
	}

	raw := makeFloodPacket(meshcore.PayloadTypeAdvert, payload)
	radio.injectWithSignal(raw, -4, -75)

	p := n.Peers().Lookup(peerID.PublicKey())
	if p == nil {
		t.Fatal("peer not added to table")
	}
	if p.Name != "remote-peer" {
		t.Errorf("Name = %q, want %q", p.Name, "remote-peer")
	}
	if p.SNR != -4 {
		t.Errorf("SNR = %g, want -4", p.SNR)
	}
	if p.RSSI != -75 {
		t.Errorf("RSSI = %d, want -75", p.RSSI)
	}
}

func TestNode_AdvertRejectsSelfAdvert(t *testing.T) {
	radio := &mockRadio{}
	localID := seedIdentity(0x52)
	n := New(localID, radio)

	adv := makeSignedAdvert(localID, 100, "self")
	payload, _ := adv.ToBytes()

	radio.inject(makeFloodPacket(meshcore.PayloadTypeAdvert, payload))

	if n.Peers().Count() != 0 {
		t.Error("self-advert should not be added to peer table")
	}
}

func TestNode_AdvertRejectsForgedSignature(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x53), radio)

	peerID := peerIdentity(0x54)
	adv := makeSignedAdvert(peerID, 100, "forged")
	adv.Signature[0] ^= 0xFF

	payload, _ := adv.ToBytes()
	radio.inject(makeFloodPacket(meshcore.PayloadTypeAdvert, payload))

	if n.Peers().Count() != 0 {
		t.Error("forged advert should not be added to peer table")
	}
}

func TestNode_AdvertReplayProtection(t *testing.T) {
	radio := &mockRadio{}
	localID := seedIdentity(0x55)
	n := New(localID, radio)

	peerID := peerIdentity(0x56)

	adv1 := makeSignedAdvert(peerID, 100, "first")
	payload1, _ := adv1.ToBytes()
	radio.inject(makeFloodPacket(meshcore.PayloadTypeAdvert, payload1))

	adv2 := makeSignedAdvert(peerID, 50, "replay")
	payload2, _ := adv2.ToBytes()
	radio.inject(makeFloodPacket(meshcore.PayloadTypeAdvert, payload2))

	p := n.Peers().Lookup(peerID.PublicKey())
	if p == nil {
		t.Fatal("peer should exist")
	}
	if p.Name != "first" {
		t.Errorf("Name = %q, want %q (replay should be rejected)", p.Name, "first")
	}
}

func TestNode_AdvertStillDispatchesToUserHandlers(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x57), radio)

	var userGot *meshcore.Packet
	n.OnPacket(meshcore.PayloadTypeAdvert, func(pkt *meshcore.Packet) {
		userGot = pkt
	})

	peerID := peerIdentity(0x58)
	adv := makeSignedAdvert(peerID, 100, "dispatch")
	payload, _ := adv.ToBytes()
	radio.inject(makeFloodPacket(meshcore.PayloadTypeAdvert, payload))

	if userGot == nil {
		t.Fatal("user handler should still be called after internal advert processing")
	}
}

func TestNode_PeersAccessor(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x59), radio)

	if n.Peers() == nil {
		t.Fatal("Peers() returned nil")
	}
	if n.Peers().Count() != 0 {
		t.Errorf("Peers().Count() = %d, want 0", n.Peers().Count())
	}
}

func TestNode_WithMaxPeers(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x5A), radio, WithMaxPeers(5))

	if n.Peers().maxPeers != 5 {
		t.Errorf("maxPeers = %d, want 5", n.Peers().maxPeers)
	}
}
