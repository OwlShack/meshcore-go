package meshcore

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// ChannelEntry represents a group channel with its name, pre-shared key, and derived hash.
type ChannelEntry struct {
	Name string
	PSK  []byte // 16 or 32 bytes
	Hash byte   // SHA256(PSK)[0]
}

// NewChannelFromPSK creates a ChannelEntry from a name and raw 16- or 32-byte PSK.
func NewChannelFromPSK(name string, psk []byte) (*ChannelEntry, error) {
	if len(psk) != 16 && len(psk) != 32 {
		return nil, fmt.Errorf("PSK must be 16 or 32 bytes, got %d", len(psk))
	}
	h := sha256.Sum256(psk)
	return &ChannelEntry{Name: name, PSK: append([]byte(nil), psk...), Hash: h[0]}, nil
}

// NewChannelFromBase64 creates a ChannelEntry from a name and base64-encoded PSK.
func NewChannelFromBase64(name string, pskBase64 string) (*ChannelEntry, error) {
	psk, err := base64.StdEncoding.DecodeString(pskBase64)
	if err != nil {
		return nil, fmt.Errorf("decoding base64 PSK: %w", err)
	}
	return NewChannelFromPSK(name, psk)
}

// PublicChannelPSK is the base64 key of the Public channel MeshCore companions start with.
const PublicChannelPSK = "izOH6cXN6mrJ5e26oRXNcg=="

// PublicChannel returns a new entry for the Public channel.
func PublicChannel() *ChannelEntry {
	ch, err := NewChannelFromBase64("Public", PublicChannelPSK)
	if err != nil {
		panic(err)
	}
	return ch
}

// NewChannelFromHashtag creates a ChannelEntry for a hashtag channel, with PSK
// SHA256("#name")[:16].
func NewChannelFromHashtag(name string) *ChannelEntry {
	name = NormalizeHashtag(name)
	psk := DeriveHashtagPSK(name)
	return &ChannelEntry{Name: name, PSK: psk[:], Hash: DeriveChannelHash(psk)}
}

// NormalizeHashtag ensures the name has a leading '#'.
func NormalizeHashtag(name string) string {
	if !strings.HasPrefix(name, "#") {
		return "#" + name
	}
	return name
}

// DeriveHashtagPSK returns the 16-byte PSK for a hashtag channel name.
func DeriveHashtagPSK(name string) [16]byte {
	name = NormalizeHashtag(name)
	h := sha256.Sum256([]byte(name))
	var psk [16]byte
	copy(psk[:], h[:16])
	return psk
}

// DeriveChannelHash returns the single-byte channel hash for a 16-byte PSK.
func DeriveChannelHash(psk [16]byte) byte {
	h := sha256.Sum256(psk[:])
	return h[0]
}
