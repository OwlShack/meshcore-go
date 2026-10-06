package node

import (
	"errors"
	"sync"

	meshcore "github.com/OwlShack/meshcore-go"
)

var (
	ErrNoMatchingChannel = errors.New("no matching channel found")
	ErrInvalidPayload    = errors.New("invalid payload for packet type")
)

// DefaultMaxChannels is the default upper bound on channel slots.
const DefaultMaxChannels = 50

// channelTable stores entries by pointer and never mutates them; treat an entry as immutable once set.
type channelTable struct {
	mu       sync.RWMutex
	channels []*meshcore.ChannelEntry
}

func newChannelTable(maxChannels int) *channelTable {
	return &channelTable{
		channels: make([]*meshcore.ChannelEntry, maxChannels),
	}
}

func (ct *channelTable) maxChannels() int {
	return len(ct.channels)
}

func (ct *channelTable) set(idx int, ch *meshcore.ChannelEntry) bool {
	if idx < 0 || idx >= ct.maxChannels() {
		return false
	}
	ct.mu.Lock()
	ct.channels[idx] = ch
	ct.mu.Unlock()
	return true
}

func (ct *channelTable) remove(idx int) bool {
	if idx < 0 || idx >= ct.maxChannels() {
		return false
	}
	ct.mu.Lock()
	existed := ct.channels[idx] != nil
	ct.channels[idx] = nil
	ct.mu.Unlock()
	return existed
}

func (ct *channelTable) get(idx int) *meshcore.ChannelEntry {
	if idx < 0 || idx >= ct.maxChannels() {
		return nil
	}
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.channels[idx]
}

func (ct *channelTable) findByHash(hash byte) []*meshcore.ChannelEntry {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	var matches []*meshcore.ChannelEntry
	for _, ch := range ct.channels {
		if ch != nil && ch.Hash == hash {
			matches = append(matches, ch)
		}
	}
	return matches
}

func (ct *channelTable) all() []*meshcore.ChannelEntry {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	var result []*meshcore.ChannelEntry
	for _, ch := range ct.channels {
		if ch != nil {
			result = append(result, ch)
		}
	}
	return result
}

func (ct *channelTable) decryptGroupText(pkt *meshcore.Packet) (*meshcore.GroupTextPayload, *meshcore.ChannelEntry, error) {
	grp, err := meshcore.GroupTextFromBytes(pkt.Payload)
	if err != nil {
		return nil, nil, ErrInvalidPayload
	}

	ct.mu.RLock()
	defer ct.mu.RUnlock()

	for _, ch := range ct.channels {
		if ch == nil || ch.Hash != grp.ChannelHash {
			continue
		}
		msg, decErr := grp.DecryptStruct(ch.PSK[:])
		if decErr != nil {
			continue
		}
		return msg, ch, nil
	}
	return nil, nil, ErrNoMatchingChannel
}

func (ct *channelTable) decryptGroupData(pkt *meshcore.Packet) ([]byte, *meshcore.ChannelEntry, error) {
	grp, err := meshcore.GroupDataFromBytes(pkt.Payload)
	if err != nil {
		return nil, nil, ErrInvalidPayload
	}

	ct.mu.RLock()
	defer ct.mu.RUnlock()

	for _, ch := range ct.channels {
		if ch == nil || ch.Hash != grp.ChannelHash {
			continue
		}
		plaintext := grp.Decrypt(ch.PSK[:])
		if plaintext == nil {
			continue
		}
		return plaintext, ch, nil
	}
	return nil, nil, ErrNoMatchingChannel
}
