package node

import (
	"sync"
	"time"

	meshcore "github.com/OwlShack/meshcore-go"
)

// DefaultMaxPeers is the default maximum number of peers stored in a PeerTable.
const DefaultMaxPeers = 100

// Peer holds information about a known mesh network peer, populated from
// received Advert packets.
type Peer struct {
	Identity meshcore.Identity
	Name     string
	Type     string // "CHAT", "REPEATER", "ROOM", "SENSOR", "NONE"
	Lat      int32
	Lon      int32
	Feat1    uint16
	Feat2    uint16
	// OutPath is the route to this peer in send order: the reverse of the path on a received advert.
	OutPath             []byte
	OutPathHashSize     uint8  // Bytes per hop hash (1, 2, or 4). 0 means default (1).
	LastAdvertTimestamp uint32 // Timestamp from the peer's clock (replay protection).
	LastSeen            time.Time
	SNR                 float32 // Real decibels (converted from wire quarter-dB at ingest).
	RSSI                int8
	HasSignalInfo       bool
}

// PeerTable is a thread-safe table of known peers, keyed by public key.
type PeerTable struct {
	mu       sync.RWMutex
	peers    map[[meshcore.PubKeySize]byte]*Peer
	maxPeers int

	// learnedPathsOnly: adverts don't set OutPath; only SetOutPath does.
	learnedPathsOnly bool
}

// NewPeerTable creates a PeerTable holding up to maxPeers, or DefaultMaxPeers if maxPeers <= 0.
func NewPeerTable(maxPeers int) *PeerTable {
	if maxPeers <= 0 {
		maxPeers = DefaultMaxPeers
	}
	return &PeerTable{
		peers:    make(map[[meshcore.PubKeySize]byte]*Peer),
		maxPeers: maxPeers,
	}
}

// Update inserts or updates a peer from an Advert whose signature the caller has
// already verified, returning false and changing nothing if the advert's timestamp
// is not newer than the stored one.
func (pt *PeerTable) Update(adv *meshcore.Advert, snr float32, rssi int8, hasSignalInfo bool, pathHashes []byte) bool {
	return pt.UpdateWithHashSize(adv, snr, rssi, hasSignalInfo, pathHashes, 1)
}

// UpdateWithHashSize is Update for hashSize-byte hop hashes.
func (pt *PeerTable) UpdateWithHashSize(adv *meshcore.Advert, snr float32, rssi int8, hasSignalInfo bool, pathHashes []byte, hashSize uint8) bool {
	key := adv.PublicKey.PublicKey()

	pt.mu.Lock()
	defer pt.mu.Unlock()

	if existing, ok := pt.peers[key]; ok {
		if adv.Timestamp <= existing.LastAdvertTimestamp {
			return false
		}
		pt.fillPeer(existing, adv, snr, rssi, hasSignalInfo, pathHashes, hashSize)
		return true
	}

	if len(pt.peers) >= pt.maxPeers {
		pt.evictOldestLocked()
	}

	p := &Peer{Identity: adv.PublicKey}
	pt.fillPeer(p, adv, snr, rssi, hasSignalInfo, pathHashes, hashSize)
	pt.peers[key] = p
	return true
}

// Insert adds a peer directly, bypassing advert verification and replay
// protection, for hydrating the table from persistent storage.
func (pt *PeerTable) Insert(p *Peer) {
	key := p.Identity.PublicKey()

	pt.mu.Lock()
	defer pt.mu.Unlock()

	if _, ok := pt.peers[key]; ok {
		cp := *p
		pt.peers[key] = &cp
		return
	}

	if len(pt.peers) >= pt.maxPeers {
		pt.evictOldestLocked()
	}

	cp := *p
	pt.peers[key] = &cp
}

func (pt *PeerTable) fillPeer(p *Peer, adv *meshcore.Advert, snr float32, rssi int8, hasSignalInfo bool, pathHashes []byte, hashSize uint8) {
	appData := adv.AppData()
	p.Name = appData.Name
	p.Type = appData.Type
	p.Lat = appData.Lat
	p.Lon = appData.Lon
	p.Feat1 = appData.Feat1
	p.Feat2 = appData.Feat2
	p.LastAdvertTimestamp = adv.Timestamp
	p.LastSeen = time.Now()
	p.SNR = snr
	p.RSSI = rssi
	p.HasSignalInfo = hasSignalInfo
	if !pt.learnedPathsOnly && len(pathHashes) > 0 {
		if hashSize == 0 {
			hashSize = 1
		}
		p.OutPath = ReverseHops(pathHashes, int(hashSize))
		p.OutPathHashSize = hashSize
	}
}

// ReverseHops returns path with its hashSize-byte hops reversed, dropping any trailing partial hop.
func ReverseHops(path []byte, hashSize int) []byte {
	out := make([]byte, 0, len(path))
	for i := len(path) - len(path)%hashSize - hashSize; i >= 0; i -= hashSize {
		out = append(out, path[i:i+hashSize]...)
	}
	return out
}

// Caller must hold pt.mu.
func (pt *PeerTable) evictOldestLocked() {
	var oldestKey [meshcore.PubKeySize]byte
	var oldestTime time.Time
	first := true

	for key, p := range pt.peers {
		if first || p.LastSeen.Before(oldestTime) {
			oldestKey = key
			oldestTime = p.LastSeen
			first = false
		}
	}
	if !first {
		delete(pt.peers, oldestKey)
	}
}

// Lookup returns a copy of the Peer with the given public key, or nil
// if not found.
func (pt *PeerTable) Lookup(pubKey [meshcore.PubKeySize]byte) *Peer {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	p, ok := pt.peers[pubKey]
	if !ok {
		return nil
	}
	cp := *p
	return &cp
}

// LookupByHash returns copies of all peers whose public key matches the given
// hash prefix, which with a 1-byte hash may be more than one.
func (pt *PeerTable) LookupByHash(hash []byte) []*Peer {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	var result []*Peer
	for _, p := range pt.peers {
		if p.Identity.IsHashMatch(hash) {
			cp := *p
			result = append(result, &cp)
		}
	}
	return result
}

// Remove deletes a peer by public key, returning true if it existed.
func (pt *PeerTable) Remove(pubKey [meshcore.PubKeySize]byte) bool {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	_, ok := pt.peers[pubKey]
	if ok {
		delete(pt.peers, pubKey)
	}
	return ok
}

// Peers returns a snapshot of all peers as a slice of copies.
func (pt *PeerTable) Peers() []Peer {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	result := make([]Peer, 0, len(pt.peers))
	for _, p := range pt.peers {
		result = append(result, *p)
	}
	return result
}

// SetOutPath sets a peer's outbound path in send order — nil clears it, a non-nil
// zero-length slice marks a direct neighbor, hashSize 0 leaves bytes-per-hop
// unchanged — and returns false if the peer is not found.
func (pt *PeerTable) SetOutPath(pubKey [meshcore.PubKeySize]byte, path []byte, hashSize ...uint8) bool {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	p, ok := pt.peers[pubKey]
	if !ok {
		return false
	}
	if path == nil {
		p.OutPath = nil
		p.OutPathHashSize = 0
	} else if len(path) == 0 {
		p.OutPath = []byte{}
	} else {
		out := make([]byte, len(path))
		copy(out, path)
		p.OutPath = out
	}
	if len(hashSize) > 0 && hashSize[0] > 0 {
		p.OutPathHashSize = hashSize[0]
	}
	return true
}

// ResetOutPath clears the outbound path for a peer, returning false if it is not found.
func (pt *PeerTable) ResetOutPath(pubKey [meshcore.PubKeySize]byte) bool {
	return pt.SetOutPath(pubKey, nil)
}

// Count returns the number of peers in the table.
func (pt *PeerTable) Count() int {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return len(pt.peers)
}
