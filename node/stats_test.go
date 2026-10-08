package node

import (
	"testing"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

func TestNode_RouteStats(t *testing.T) {
	identity := seedIdentity(0x01)
	other := seedIdentity(0x02)
	radio := &mockRadio{}
	n := New(identity, radio, WithAllowForwardHandler(func(*meshcore.Packet) bool { return true }))
	defer n.Stop()

	flood := makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0x01, 0x02, 0x03, 0x04, 0x05})
	radio.inject(flood)
	radio.inject(flood)
	radio.inject(makeDirectPacket(meshcore.PayloadTypeTxtMsg, []byte{identity.Hash()[0], other.Hash()[0]}, []byte{0x04}))
	time.Sleep(100 * time.Millisecond)

	got := n.RouteStats()
	want := RouteStats{
		FloodReceived:   2,
		DirectReceived:  1,
		FloodDuplicates: 1,
		FloodRelays:     1,
		DirectRelays:    1,
		Delivered:       1,
	}
	if got != want {
		t.Fatalf("RouteStats() = %+v, want %+v", got, want)
	}
}

func TestNode_RouteStatsRxAirtime(t *testing.T) {
	radio := &mockRadio{}
	n := New(seedIdentity(0x01), radio, WithAirtimeEstimator(func(l int) uint32 { return uint32(l) }))
	defer n.Stop()

	radio.inject(makeFloodPacket(meshcore.PayloadTypeGrpTxt, []byte{1, 2, 3, 4}))
	radio.inject(makeDirectPacket(meshcore.PayloadTypeAck, []byte{0x11}, []byte{1, 2, 3, 4}))
	scoped, _ := makeTransportFloodPacket(meshcore.NewRegion("nz"), []byte{1, 2, 3, 4, 5}).ToBytes()
	radio.inject(scoped)
	if got := n.RouteStats().RxAirtimeMs; got != 6+7+11 {
		t.Fatalf("RxAirtimeMs = %d, want 24", got)
	}
}
