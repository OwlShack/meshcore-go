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

	flood := makeFloodPacket(meshcore.PayloadTypeTxtMsg, []byte{0x01, 0x02, 0x03})
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
