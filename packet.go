package meshcore

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

type Packet struct {
	Header         byte
	PathLength     uint8
	Path           []byte
	Payload        []byte
	TransportCode1 uint16 // Little Endian
	TransportCode2 uint16 // Little Endian

	SNR           float32 // real dB; see SNRFromWire
	RSSI          int8
	HasSignalInfo bool

	noRetransmit bool
}

// MarkDoNotRetransmit flags this packet as consumed by this node, suppressing relay.
func (p *Packet) MarkDoNotRetransmit() { p.noRetransmit = true }

// IsMarkedDoNotRetransmit reports whether MarkDoNotRetransmit was called on this packet.
func (p *Packet) IsMarkedDoNotRetransmit() bool { return p.noRetransmit }

// SNRFromWire converts an on-wire SNR byte (quarter-dB units) to real decibels.
func SNRFromWire(b int8) float32 { return float32(b) / 4 }

// SNRToWire converts an SNR in decibels to its on-wire quarter-dB byte, truncating as firmware does and clamping to int8.
func SNRToWire(snr float32) int8 {
	return int8(max(-128, min(127, snr*4)))
}

func snrDBFromWire(b int8) float32 { return SNRFromWire(b) }

// PathSNRdB converts a per-hop SNR byte from a trace path into real decibels.
func PathSNRdB(b byte) float32 { return SNRFromWire(int8(b)) }

func MakeHeader(routeType, payloadType, payloadVer byte) byte {
	return (payloadVer << 6) | (payloadType << 2) | routeType
}

// MakePathLen encodes a path_len byte from bytes per hop (1 to 3) and hop count (0 to 63).
func MakePathLen(hashSize, hashCount uint8) uint8 {
	return (hashSize-1)<<6 | hashCount&63
}

// PathLenFields splits a path_len byte into bytes per hop (bits 6-7, plus 1) and hop count (bits 0-5).
func PathLenFields(pathLen uint8) (hashSize, hashCount uint8) {
	return pathLen>>6 + 1, pathLen & 63
}

// splitPathHashes tolerates a path shorter than hashSize*hashCount, yielding fewer hashes.
func splitPathHashes(path []byte, hashSize, hashCount uint8) [][]byte {
	hashes := make([][]byte, 0, hashCount)
	for i := range int(hashCount) {
		start, end := i*int(hashSize), (i+1)*int(hashSize)
		if end > len(path) {
			break
		}
		hashes = append(hashes, path[start:end])
	}
	return hashes
}

func IsValidPathLen(pathLen uint8) bool {
	hashSize, hashCount := PathLenFields(pathLen)
	if hashSize == 4 {
		return false
	}
	return int(hashCount)*int(hashSize) <= MaxPathSize
}

func PacketFromBytes(data []byte) (*Packet, error) {
	packet := &Packet{}
	buffer := bytes.NewBuffer(data)

	headerByte, err := buffer.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	packet.Header = headerByte

	if v := packet.PayloadVer(); v != 0 {
		return nil, fmt.Errorf("unsupported payload version %d", v)
	}

	routeType := headerByte & PacketRouteMask
	hasTransportCodes := routeType == RouteTypeTransportFlood || routeType == RouteTypeTransportDirect

	if hasTransportCodes {
		if err := binary.Read(buffer, binary.LittleEndian, &packet.TransportCode1); err != nil {
			return nil, fmt.Errorf("reading transport code 1: %w", err)
		}
		if err := binary.Read(buffer, binary.LittleEndian, &packet.TransportCode2); err != nil {
			return nil, fmt.Errorf("reading transport code 2: %w", err)
		}
	}

	pathLenByte, err := buffer.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("reading path length: %w", err)
	}
	packet.PathLength = pathLenByte

	if !IsValidPathLen(pathLenByte) {
		return nil, fmt.Errorf("invalid path length byte: 0x%02x", pathLenByte)
	}

	pathByteLength := int(packet.PathHashCount()) * int(packet.PathHashSize())

	if buffer.Len() < pathByteLength {
		return nil, fmt.Errorf("not enough data for path: need %d bytes, have %d", pathByteLength, buffer.Len())
	}
	packet.Path = buffer.Next(pathByteLength)

	payload := buffer.Bytes()
	if len(payload) > MaxPacketPayload {
		return nil, fmt.Errorf("payload too large: %d bytes, max %d", len(payload), MaxPacketPayload)
	}
	packet.Payload = payload

	return packet, nil
}

func (p *Packet) ToBytes() ([]byte, error) {
	buffer := bytes.NewBuffer(nil)

	if err := binary.Write(buffer, binary.LittleEndian, p.Header); err != nil {
		return nil, fmt.Errorf("writing header: %w", err)
	}

	routeType := p.RouteType()
	hasTransportCodes := routeType == RouteTypeTransportFlood || routeType == RouteTypeTransportDirect

	if hasTransportCodes {
		if err := binary.Write(buffer, binary.LittleEndian, p.TransportCode1); err != nil {
			return nil, fmt.Errorf("writing transport code 1: %w", err)
		}
		if err := binary.Write(buffer, binary.LittleEndian, p.TransportCode2); err != nil {
			return nil, fmt.Errorf("writing transport code 2: %w", err)
		}
	}

	if err := binary.Write(buffer, binary.LittleEndian, p.PathLength); err != nil {
		return nil, fmt.Errorf("writing path length: %w", err)
	}

	if err := binary.Write(buffer, binary.LittleEndian, p.Path); err != nil {
		return nil, fmt.Errorf("writing path: %w", err)
	}

	if err := binary.Write(buffer, binary.LittleEndian, p.Payload); err != nil {
		return nil, fmt.Errorf("writing payload: %w", err)
	}

	return buffer.Bytes(), nil
}

// PathHashSize returns the per-hop hash size in bytes (1-3).
func (p *Packet) PathHashSize() uint8 { s, _ := PathLenFields(p.PathLength); return s }

// PathHashCount returns the hop count.
func (p *Packet) PathHashCount() uint8 { _, c := PathLenFields(p.PathLength); return c }

func (p *Packet) PathHashes() [][]byte {
	return splitPathHashes(p.Path, p.PathHashSize(), p.PathHashCount())
}

// Clone returns a deep copy of the packet.
func (p *Packet) Clone() *Packet {
	c := *p
	c.Path = bytes.Clone(p.Path)
	c.Payload = bytes.Clone(p.Payload)
	return &c
}

func (p *Packet) RouteType() byte {
	return p.Header & PacketRouteMask
}

func (p *Packet) RouteTypeString() string {
	switch p.RouteType() {
	case RouteTypeFlood:
		return "FLOOD"
	case RouteTypeDirect:
		return "DIRECT"
	case RouteTypeTransportFlood:
		return "TRANSPORT_FLOOD"
	case RouteTypeTransportDirect:
		return "TRANSPORT_DIRECT"
	default:
		return ""
	}
}

func (p *Packet) PayloadType() byte {
	return p.Header >> PacketTypeShift & PacketTypeMask
}

func (p *Packet) PayloadTypeString() string {
	switch p.PayloadType() {
	case PayloadTypeReq:
		return "REQ"
	case PayloadTypeResponse:
		return "RESPONSE"
	case PayloadTypeTxtMsg:
		return "TXT_MSG"
	case PayloadTypeAck:
		return "ACK"
	case PayloadTypeAdvert:
		return "ADVERT"
	case PayloadTypeGrpTxt:
		return "GRP_TXT"
	case PayloadTypeGrpData:
		return "GRP_DATA"
	case PayloadTypeAnonReq:
		return "ANON_REQ"
	case PayloadTypePath:
		return "PATH"
	case PayloadTypeTrace:
		return "TRACE"
	case PayloadTypeMultiPart:
		return "MULTI_PART"
	case PayloadTypeControl:
		return "CONTROL"
	case PayloadTypeRawCustom:
		return "RAW_CUSTOM"
	default:
		return ""
	}
}

func (p *Packet) PayloadVer() byte {
	return p.Header >> PacketVerShift & PacketVerMask
}

func (p *Packet) Validate() error {
	if !IsValidPathLen(p.PathLength) {
		return fmt.Errorf("invalid path length byte: 0x%02x", p.PathLength)
	}
	if n := int(p.PathHashCount()) * int(p.PathHashSize()); len(p.Path) != n {
		return fmt.Errorf("path length byte 0x%02x needs %d path bytes, have %d", p.PathLength, n, len(p.Path))
	}
	if len(p.Payload) > MaxPacketPayload {
		return fmt.Errorf("payload too large: %d bytes, max %d", len(p.Payload), MaxPacketPayload)
	}
	return nil
}

func (p *Packet) IsRouteFlood() bool {
	rt := p.RouteType()
	return rt == RouteTypeFlood || rt == RouteTypeTransportFlood
}

func (p *Packet) IsRouteDirect() bool {
	rt := p.RouteType()
	return rt == RouteTypeDirect || rt == RouteTypeTransportDirect
}

func (p *Packet) IsTransport() bool {
	rt := p.RouteType()
	return rt == RouteTypeTransportFlood || rt == RouteTypeTransportDirect
}

// AppendPathHash appends a path hash, returning false if the path is already full.
func (p *Packet) AppendPathHash(hash []byte) bool {
	hashSize, count := PathLenFields(p.PathLength)
	if len(hash) < int(hashSize) {
		return false
	}
	newCount := int(count) + 1
	if newCount*int(hashSize) > MaxPathSize {
		return false
	}
	// Path may alias the payload when parsed from a buffer; never grow in place.
	p.Path = append(p.Path[:len(p.Path):len(p.Path)], hash[:hashSize]...)
	p.PathLength = (hashSize-1)<<6 | uint8(newCount)
	return true
}

// RemoveFirstPathHash removes the first hash from the packet's path, returning
// false if the path is empty.
func (p *Packet) RemoveFirstPathHash() bool {
	hashSize, count := PathLenFields(p.PathLength)
	if count == 0 || len(p.Path) < int(hashSize) {
		return false
	}
	p.Path = p.Path[hashSize:]
	p.PathLength = (hashSize-1)<<6 | (count - 1)
	return true
}

const PacketHashSize = 8

// PacketHash computes the dedup fingerprint: SHA256(payloadType ‖ payload)
// truncated to 8 bytes, with PathLength mixed in for TRACE packets.
func (p *Packet) PacketHash() [PacketHashSize]byte {
	h := sha256.New()
	h.Write([]byte{p.PayloadType()})
	if p.PayloadType() == PayloadTypeTrace {
		h.Write([]byte{p.PathLength, 0}) // uint16 LE to match C++ sizeof(uint16_t)
	}
	h.Write(p.Payload)
	sum := h.Sum(nil)
	var out [PacketHashSize]byte
	copy(out[:], sum[:PacketHashSize])
	return out
}
