package meshcore

import (
	"encoding/binary"
	"testing"
)

func dedupPacket(payload []byte) *Packet {
	return &Packet{
		Header:  MakeHeader(RouteTypeFlood, PayloadTypeAdvert, 0),
		Payload: append([]byte(nil), payload...),
	}
}

// MeshCore 1.16 ACK payloads carry a random salt byte, so each ACK packet hashes uniquely.
func dedupACKPacket(crc uint32, salt byte) *Packet {
	payload := make([]byte, 5)
	binary.LittleEndian.PutUint32(payload, crc)
	payload[4] = salt
	return &Packet{
		Header:  MakeHeader(RouteTypeFlood, PayloadTypeAck, 0),
		Payload: payload,
	}
}

func TestDedup_FirstSeenReturnsFalse(t *testing.T) {
	var d DedupCache
	if got := d.HasSeen(dedupPacket([]byte{0x01})); got {
		t.Fatal("HasSeen() = true, want false")
	}
}

func TestDedup_SecondSeenReturnsTrue(t *testing.T) {
	var d DedupCache
	pkt := dedupPacket([]byte{0x01})
	if got := d.HasSeen(pkt); got {
		t.Fatal("first HasSeen() = true, want false")
	}
	if got := d.HasSeen(pkt); !got {
		t.Fatal("second HasSeen() = false, want true")
	}
}

func TestDedup_DifferentPacketsNotDuplicate(t *testing.T) {
	var d DedupCache
	if got := d.HasSeen(dedupPacket([]byte{0x01})); got {
		t.Fatal("first packet HasSeen() = true, want false")
	}
	if got := d.HasSeen(dedupPacket([]byte{0x02})); got {
		t.Fatal("second packet HasSeen() = true, want false")
	}
}

func TestDedup_ACK_FirstSeenReturnsFalse(t *testing.T) {
	var d DedupCache
	if got := d.HasSeen(dedupACKPacket(0x11223344, 0x01)); got {
		t.Fatal("HasSeen() = true, want false")
	}
}

func TestDedup_ACK_IdenticalPacketDeduped(t *testing.T) {
	var d DedupCache
	pkt := dedupACKPacket(0x11223344, 0x01)
	if got := d.HasSeen(pkt); got {
		t.Fatal("first HasSeen() = true, want false")
	}
	if got := d.HasSeen(pkt); !got {
		t.Fatal("second HasSeen() = false, want true")
	}
}

func TestDedup_ACK_SameCRCDifferentSaltNotDeduped(t *testing.T) {
	var d DedupCache
	if got := d.HasSeen(dedupACKPacket(0x11223344, 0x01)); got {
		t.Fatal("first ACK HasSeen() = true, want false")
	}
	if got := d.HasSeen(dedupACKPacket(0x11223344, 0x02)); got {
		t.Fatal("second ACK (same CRC, different salt) HasSeen() = true, want false")
	}
}

func TestDedup_RingBufferEviction(t *testing.T) {
	var d DedupCache
	first := dedupPacket([]byte{0x00, 0x00})
	if got := d.HasSeen(first); got {
		t.Fatal("first HasSeen() = true, want false")
	}

	for i := 1; i <= MaxPacketHashes; i++ {
		pkt := dedupPacket([]byte{byte(i), byte(i >> 8)})
		if got := d.HasSeen(pkt); got {
			t.Fatalf("HasSeen() = true for unique packet %d", i)
		}
	}

	if got := d.HasSeen(first); got {
		t.Fatal("HasSeen() = true after eviction, want false")
	}
}

func TestDedup_ACK_RingBufferEviction(t *testing.T) {
	var d DedupCache
	first := dedupACKPacket(1, 0x00)
	if got := d.HasSeen(first); got {
		t.Fatal("first HasSeen() = true, want false")
	}

	for i := uint32(2); i <= MaxPacketHashes+1; i++ {
		pkt := dedupACKPacket(i, 0x00)
		if got := d.HasSeen(pkt); got {
			t.Fatalf("HasSeen() = true for unique ACK %d", i)
		}
	}

	if got := d.HasSeen(first); got {
		t.Fatal("HasSeen() = true after ACK eviction, want false")
	}
}

func TestDedup_MarkSeen(t *testing.T) {
	var d DedupCache
	pkt := dedupPacket([]byte{0xAA, 0xBB})
	d.MarkSeen(pkt)
	if got := d.HasSeen(pkt); !got {
		t.Fatal("HasSeen() = false after MarkSeen(), want true")
	}
}

func TestDedup_MarkSeen_ACK(t *testing.T) {
	var d DedupCache
	pkt := dedupACKPacket(0xAABBCCDD, 0x07)
	d.MarkSeen(pkt)
	if got := d.HasSeen(pkt); !got {
		t.Fatal("HasSeen() = false after MarkSeen(), want true")
	}
}

func TestDedup_Clear(t *testing.T) {
	var d DedupCache
	pkt, other := dedupPacket([]byte{0x01}), dedupPacket([]byte{0x02})
	d.MarkSeen(pkt)
	d.MarkSeen(other)
	d.Clear(pkt)
	if d.Contains(pkt) {
		t.Fatal("Contains() = true after Clear, want false")
	}
	if !d.Contains(other) {
		t.Fatal("Clear removed an unrelated packet")
	}
	if d.HasSeen(pkt) {
		t.Fatal("HasSeen() = true after Clear, want false")
	}
}
