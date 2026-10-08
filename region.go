package meshcore

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"strings"
)

const (
	RegionKeySize          = 16
	MaxRegionName          = 30
	MaxRegions             = 32
	RegionDenyFlood  uint8 = 0x01
	RegionDenyDirect uint8 = 0x02
)

type RegionKey [RegionKeySize]byte

type Region struct {
	ID     uint16
	Parent uint16
	Flags  uint8
	Name   string
	Key    RegionKey
}

// NewRegionFromHashtag builds a region named "#name".
//
// Deprecated: use NewRegion, which keeps the name as given and gives a "$" private name no key, as firmware does.
func NewRegionFromHashtag(name string) *Region {
	name = normalizeRegionName(name)
	key := DeriveRegionKey(name)
	return &Region{
		Name: name,
		Key:  key,
	}
}

// NewRegion derives the key as firmware does: a bare name hashes as "#name", and a "$" name gets none.
func NewRegion(name string) *Region {
	r := &Region{Name: name}
	if !strings.HasPrefix(name, "$") {
		r.Key = DeriveRegionKey(normalizeRegionName(name))
	}
	return r
}

func NewRegionFromKey(name string, key RegionKey) *Region {
	return &Region{
		Name: name,
		Key:  key,
	}
}

func normalizeRegionName(name string) string {
	if strings.HasPrefix(name, "#") || strings.HasPrefix(name, "$") {
		return name
	}
	return "#" + name
}

// DeriveRegionKey computes the 16-byte region key for a '#'-prefixed name: SHA256(name)[:16].
func DeriveRegionKey(name string) RegionKey {
	h := sha256.Sum256([]byte(name))
	var key RegionKey
	copy(key[:], h[:RegionKeySize])
	return key
}

func (k RegionKey) IsZero() bool {
	for _, b := range k {
		if b != 0 {
			return false
		}
	}
	return true
}

// CalcTransportCode computes the per-packet transport code:
// HMAC-SHA256(key, payloadType ‖ payload) truncated to uint16, with 0x0000 and
// 0xFFFF reserved.
func (k RegionKey) CalcTransportCode(payloadType byte, payload []byte) uint16 {
	mac := hmac.New(sha256.New, k[:])
	mac.Write([]byte{payloadType})
	mac.Write(payload)
	sum := mac.Sum(nil)
	code := binary.LittleEndian.Uint16(sum[:2])
	switch code {
	case 0x0000:
		code = 0x0001
	case 0xFFFF:
		code = 0xFFFE
	}
	return code
}

func (r *Region) CalcTransportCode(pkt *Packet) uint16 {
	return r.Key.CalcTransportCode(pkt.PayloadType(), pkt.Payload)
}

func (r *Region) MatchesPacket(pkt *Packet) bool {
	return pkt.IsTransport() && !r.Key.IsZero() && pkt.TransportCode1 == r.CalcTransportCode(pkt)
}

// SetScope scopes a flood to r, or unscopes it when r is nil or keyless; call it once the payload is final.
func (p *Packet) SetScope(r *Region) {
	if !p.IsRouteFlood() {
		return
	}
	p.TransportCode1, p.TransportCode2 = 0, 0
	if r == nil || r.Key.IsZero() {
		p.Header = MakeHeader(RouteTypeFlood, p.PayloadType(), p.PayloadVer())
		return
	}
	p.Header = MakeHeader(RouteTypeTransportFlood, p.PayloadType(), p.PayloadVer())
	p.TransportCode1 = r.CalcTransportCode(p)
}

func (r *Region) DenyFlood() bool {
	return r.Flags&RegionDenyFlood != 0
}

func (r *Region) DenyDirect() bool {
	return r.Flags&RegionDenyDirect != 0
}

// IsValidRegionNameChar reports whether c is allowed in a region name.
func IsValidRegionNameChar(c byte) bool {
	return c == '-' || c == '$' || c == '#' || (c >= '0' && c <= '9') || c >= 'A'
}
