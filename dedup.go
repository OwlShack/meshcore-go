package meshcore

import (
	"sync"
)

const (
	MaxPacketHashes = 160 // matches C++ SimpleMeshTables (128+32)
)

// DedupCache is a fixed-size circular buffer of recently seen packet hashes, safe for concurrent use.
type DedupCache struct {
	mu       sync.Mutex
	hashes   [MaxPacketHashes][PacketHashSize]byte
	hashNext int
}

// HasSeen reports whether this packet has been seen before, recording it if not.
func (d *DedupCache) HasSeen(pkt *Packet) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.hasSeenHash(pkt)
}

func (d *DedupCache) hasSeenHash(pkt *Packet) bool {
	h := pkt.PacketHash()
	for i := range d.hashes {
		if d.hashes[i] == h {
			return true
		}
	}
	d.hashes[d.hashNext] = h
	d.hashNext = (d.hashNext + 1) % MaxPacketHashes
	return false
}

// Contains reports whether the packet is recorded, without recording it.
func (d *DedupCache) Contains(pkt *Packet) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := pkt.PacketHash()
	for i := range d.hashes {
		if d.hashes[i] == h {
			return true
		}
	}
	return false
}

// MarkSeen records a packet as seen without checking.
func (d *DedupCache) MarkSeen(pkt *Packet) {
	d.mu.Lock()
	defer d.mu.Unlock()

	h := pkt.PacketHash()
	d.hashes[d.hashNext] = h
	d.hashNext = (d.hashNext + 1) % MaxPacketHashes
}

// Clear forgets a packet so that it is no longer reported as seen.
func (d *DedupCache) Clear(pkt *Packet) {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := pkt.PacketHash()
	for i := range d.hashes {
		if d.hashes[i] == h {
			d.hashes[i] = [PacketHashSize]byte{}
			return
		}
	}
}
