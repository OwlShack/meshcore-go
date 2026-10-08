package meshcore

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

// Request and reply bodies here are the bytes after the 4-byte tag that starts every REQ, RESPONSE and ANON_REQ plaintext.

const maxLoginPassword = 15

// BuildTaggedPlaintext prefixes body with its 4-byte tag, giving a REQ, RESPONSE or ANON_REQ plaintext.
func BuildTaggedPlaintext(tag uint32, body []byte) []byte {
	return append(binary.LittleEndian.AppendUint32(nil, tag), body...)
}

// ParseTaggedPlaintext splits a REQ, RESPONSE or ANON_REQ plaintext into its tag and body.
func ParseTaggedPlaintext(data []byte) (tag uint32, body []byte, err error) {
	if len(data) < 4 {
		return 0, nil, fmt.Errorf("%w: tagged plaintext needs 4 bytes, have %d", ErrTooShort, len(data))
	}
	return binary.LittleEndian.Uint32(data[:4]), data[4:], nil
}

// cString returns b up to its first NUL, dropping the zero padding decryption leaves.
func cString(b []byte) []byte {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return b[:i]
	}
	return b
}

// LoginRequest is the body of an ANON_REQ login.
type LoginRequest struct {
	SyncSince uint32 // room servers only: push posts newer than this
	Password  string // at most 15 bytes; empty logs in by ACL membership alone
}

// BuildLoginRequest builds a login body; set room when logging in to a room server.
func BuildLoginRequest(r LoginRequest, room bool) ([]byte, error) {
	if len(r.Password) > maxLoginPassword {
		return nil, fmt.Errorf("meshcore: login password of %d bytes is longer than %d", len(r.Password), maxLoginPassword)
	}
	if r.Password != "" && r.Password[0] < ' ' {
		return nil, fmt.Errorf("meshcore: login password must not start with control byte 0x%02x", r.Password[0])
	}
	var b []byte
	if room {
		b = binary.LittleEndian.AppendUint32(b, r.SyncSince)
	}
	return append(b, r.Password...), nil
}

// ParseLoginRequest decodes a login body; set room when you are a room server.
func ParseLoginRequest(data []byte, room bool) (LoginRequest, error) {
	var r LoginRequest
	if room {
		if len(data) < 4 {
			return r, fmt.Errorf("%w: room login needs 4 bytes, have %d", ErrTooShort, len(data))
		}
		r.SyncSince = binary.LittleEndian.Uint32(data[:4])
		data = data[4:]
	}
	r.Password = string(cString(data))
	return r, nil
}

// LoginReply is a server's answer to a login, after the 4-byte server clock in its tag position.
type LoginReply struct {
	Legacy        bool // an old repeater's bare "OK"; the other fields are then zero
	KeepAlive     byte // legacy keep-alive interval in units of 16 seconds, now always 0
	Admin         byte // 1 for an admin; room servers send 2 when the client has no permissions
	Permissions   byte // PermACL* role in the low two bits
	FirmwareLevel byte
}

const loginReplySize = 9

// BuildLoginReply builds a login reply body; random keeps its packet hash unique.
func BuildLoginReply(r LoginReply, random [4]byte) []byte {
	b := []byte{0, r.KeepAlive, r.Admin, r.Permissions}
	b = append(b, random[:]...)
	return append(b, r.FirmwareLevel)
}

// ParseLoginReply decodes a login reply body.
func ParseLoginReply(data []byte) (LoginReply, error) {
	if bytes.HasPrefix(data, []byte("OK")) {
		return LoginReply{Legacy: true}, nil
	}
	if len(data) < loginReplySize {
		return LoginReply{}, fmt.Errorf("%w: login reply needs %d bytes, have %d", ErrTooShort, loginReplySize, len(data))
	}
	if data[0] != 0 {
		return LoginReply{}, fmt.Errorf("meshcore: login reply code 0x%02x is not OK", data[0])
	}
	return LoginReply{KeepAlive: data[1], Admin: data[2], Permissions: data[3], FirmwareLevel: data[8]}, nil
}

// BuildRequest builds a REQ body that carries only its type: reqType, four zero bytes, then random.
func BuildRequest(reqType byte, random [4]byte) []byte {
	return append([]byte{reqType, 0, 0, 0, 0}, random[:]...)
}

// BuildTelemetryRequest builds a GET_TELEMETRY_DATA body asking for the TelemPerm* groups in perms.
func BuildTelemetryRequest(perms byte, random [4]byte) []byte {
	b := BuildRequest(ReqTypeGetTelemetryData, random)
	b[1] = ^perms
	return b
}

// ParseTelemetryRequest returns the TelemPerm* groups a GET_TELEMETRY_DATA body asks for.
func ParseTelemetryRequest(data []byte) (byte, error) {
	if len(data) < 2 {
		return 0, fmt.Errorf("%w: telemetry request needs 2 bytes, have %d", ErrTooShort, len(data))
	}
	return ^data[1], nil
}

// BuildKeepAliveRequest builds a KEEP_ALIVE body for a room server.
func BuildKeepAliveRequest(syncSince uint32) []byte {
	return binary.LittleEndian.AppendUint32([]byte{ReqTypeKeepAlive}, syncSince)
}

// KeepAliveAckHash returns the ACK CRC a room server answers a keep-alive with, keyed by the requester's public key.
func KeepAliveAckHash(tag, syncSince uint32, requesterPubKey []byte) uint32 {
	return CalcAckHash(BuildTaggedPlaintext(tag, BuildKeepAliveRequest(syncSince)), requesterPubKey)
}

// KeepAliveAck is a room server's 5-byte keep-alive ACK.
type KeepAliveAck struct {
	CRC      uint32
	Unsynced byte // posts waiting to be pushed to the client
}

// ParseKeepAliveAck decodes a room server's keep-alive ACK payload.
func ParseKeepAliveAck(data []byte) (KeepAliveAck, error) {
	if len(data) < 5 {
		return KeepAliveAck{}, fmt.Errorf("%w: keep-alive ack needs 5 bytes, have %d", ErrTooShort, len(data))
	}
	return KeepAliveAck{CRC: binary.LittleEndian.Uint32(data[:4]), Unsynced: data[4]}, nil
}

// CommonStats is the part of a GET_STATUS reply repeaters and room servers share.
type CommonStats struct {
	BattMilliVolts uint16
	TxQueueLen     uint16
	NoiseFloor     int16
	LastRSSI       int16
	PacketsRecv    uint32
	PacketsSent    uint32
	AirTimeSecs    uint32
	UpTimeSecs     uint32
	SentFlood      uint32
	SentDirect     uint32
	RecvFlood      uint32
	RecvDirect     uint32
	ErrEvents      uint16
	LastSNR        int16 // quarter dB
	DirectDups     uint16
	FloodDups      uint16
}

// RepeaterStats is a repeater's GET_STATUS reply body.
type RepeaterStats struct {
	CommonStats
	RxAirTimeSecs uint32
	RecvErrors    uint32
}

// RoomStats is a room server's GET_STATUS reply body.
type RoomStats struct {
	CommonStats
	Posted     uint16
	PostPushes uint16
}

// BuildRepeaterStats encodes a repeater's GET_STATUS reply body.
func BuildRepeaterStats(s RepeaterStats) []byte { return appendStats(s) }

// ParseRepeaterStats decodes a repeater's GET_STATUS reply body.
func ParseRepeaterStats(data []byte) (RepeaterStats, error) { return parseStats[RepeaterStats](data) }

// BuildRoomStats encodes a room server's GET_STATUS reply body.
func BuildRoomStats(s RoomStats) []byte { return appendStats(s) }

// ParseRoomStats decodes a room server's GET_STATUS reply body.
func ParseRoomStats(data []byte) (RoomStats, error) { return parseStats[RoomStats](data) }

func appendStats(s any) []byte {
	b, _ := binary.Append(nil, binary.LittleEndian, s)
	return b
}

func parseStats[T any](data []byte) (T, error) {
	var s T
	if n := binary.Size(s); len(data) < n {
		return s, fmt.Errorf("%w: status reply needs %d bytes, have %d", ErrTooShort, n, len(data))
	}
	_, err := binary.Decode(data, binary.LittleEndian, &s)
	return s, err
}

// ACLEntry is one client in a GET_ACCESS_LIST reply.
type ACLEntry struct {
	Prefix      [6]byte // public key prefix
	Permissions byte
}

// BuildAccessListReply encodes a GET_ACCESS_LIST reply body.
func BuildAccessListReply(entries []ACLEntry) []byte {
	b := make([]byte, 0, 7*len(entries))
	for _, e := range entries {
		b = append(append(b, e.Prefix[:]...), e.Permissions)
	}
	return b
}

// ParseAccessListReply decodes a GET_ACCESS_LIST reply body.
func ParseAccessListReply(data []byte) ([]ACLEntry, error) {
	var out []ACLEntry
	for ; len(data) >= 7; data = data[7:] {
		if data[6] == 0 {
			continue // servers never list a client without permissions, so this is padding
		}
		out = append(out, ACLEntry{Prefix: [6]byte(data[:6]), Permissions: data[6]})
	}
	return out, nil
}

// NeighboursRequest is a GET_NEIGHBOURS request.
type NeighboursRequest struct {
	Count     byte
	Offset    uint16
	OrderBy   byte // Neighbours* ordering
	PrefixLen byte // public key bytes per neighbour; servers clamp it to 32
}

// BuildNeighboursRequest encodes a GET_NEIGHBOURS body, clamping PrefixLen to PubKeySize.
func BuildNeighboursRequest(r NeighboursRequest, random [4]byte) []byte {
	b := []byte{ReqTypeGetNeighbours, 0, r.Count}
	b = binary.LittleEndian.AppendUint16(b, r.Offset)
	b = append(b, r.OrderBy, min(r.PrefixLen, PubKeySize))
	return append(b, random[:]...)
}

// ParseNeighboursRequest decodes a GET_NEIGHBOURS body, clamping PrefixLen to PubKeySize.
func ParseNeighboursRequest(data []byte) (NeighboursRequest, error) {
	if len(data) < 7 {
		return NeighboursRequest{}, fmt.Errorf("%w: neighbours request needs 7 bytes, have %d", ErrTooShort, len(data))
	}
	if data[1] != 0 {
		return NeighboursRequest{}, fmt.Errorf("meshcore: neighbours request version %d is unknown", data[1])
	}
	return NeighboursRequest{Count: data[2], Offset: binary.LittleEndian.Uint16(data[3:5]), OrderBy: data[5], PrefixLen: min(data[6], PubKeySize)}, nil
}

// Neighbour is one entry in a GET_NEIGHBOURS reply.
type Neighbour struct {
	Prefix       []byte
	HeardSecsAgo uint32
	SNR          int8 // quarter dB
}

// NeighboursReply is a GET_NEIGHBOURS reply.
type NeighboursReply struct {
	Total      uint16 // neighbours the server knows, not just those in this page
	Neighbours []Neighbour
}

// BuildNeighboursReply encodes a GET_NEIGHBOURS reply body; every Prefix should be the requested length.
func BuildNeighboursReply(r NeighboursReply) []byte {
	b := binary.LittleEndian.AppendUint16(nil, r.Total)
	b = binary.LittleEndian.AppendUint16(b, uint16(len(r.Neighbours)))
	for _, n := range r.Neighbours {
		b = append(b, n.Prefix...)
		b = binary.LittleEndian.AppendUint32(b, n.HeardSecsAgo)
		b = append(b, byte(n.SNR))
	}
	return b
}

// ParseNeighboursReply decodes a GET_NEIGHBOURS reply body sent for the request's PrefixLen, clamped as servers do.
func ParseNeighboursReply(data []byte, prefixLen byte) (NeighboursReply, error) {
	if len(data) < 4 {
		return NeighboursReply{}, fmt.Errorf("%w: neighbours reply needs 4 bytes, have %d", ErrTooShort, len(data))
	}
	r := NeighboursReply{Total: binary.LittleEndian.Uint16(data[:2])}
	pl := int(min(prefixLen, PubKeySize))
	n, size := int(binary.LittleEndian.Uint16(data[2:4])), pl+5
	data = data[4:]
	if len(data) < n*size {
		return NeighboursReply{}, fmt.Errorf("%w: %d neighbours need %d bytes, have %d", ErrTooShort, n, n*size, len(data))
	}
	for i := range n {
		e := data[i*size:]
		r.Neighbours = append(r.Neighbours, Neighbour{
			Prefix:       e[:pl:pl],
			HeardSecsAgo: binary.LittleEndian.Uint32(e[pl:]),
			SNR:          int8(e[pl+4]),
		})
	}
	return r, nil
}

// OwnerInfo is a GET_OWNER_INFO reply.
type OwnerInfo struct {
	FirmwareVersion string
	Name            string
	Owner           string // may span several lines
}

// BuildOwnerInfoReply encodes a GET_OWNER_INFO reply body.
func BuildOwnerInfoReply(o OwnerInfo) []byte {
	return []byte(o.FirmwareVersion + "\n" + o.Name + "\n" + o.Owner)
}

// ParseOwnerInfoReply decodes a GET_OWNER_INFO reply body.
func ParseOwnerInfoReply(data []byte) (OwnerInfo, error) {
	f := append(strings.SplitN(string(cString(data)), "\n", 3), "", "")
	return OwnerInfo{FirmwareVersion: f[0], Name: f[1], Owner: f[2]}, nil
}

// AnonOwnerReply is a server's answer to an anon OWNER request.
type AnonOwnerReply struct {
	Clock uint32 // the server's clock, in Unix seconds
	Name  string
	Owner string // may span several lines
}

// ParseAnonOwnerReply decodes an OWNER reply from the bytes after its 4-byte tag.
func ParseAnonOwnerReply(data []byte) (AnonOwnerReply, error) {
	if len(data) < 4 {
		return AnonOwnerReply{}, fmt.Errorf("%w: owner reply needs 4 bytes, have %d", ErrTooShort, len(data))
	}
	r := AnonOwnerReply{Clock: binary.LittleEndian.Uint32(data[:4])}
	r.Name, r.Owner, _ = strings.Cut(string(cString(data[4:])), "\n")
	return r, nil
}

// AnonBasicReply is a server's answer to an anon BASIC request.
type AnonBasicReply struct {
	Clock    uint32 // the server's clock, in Unix seconds
	Features byte   // AnonFeature* bits
}

// ParseAnonBasicReply decodes a BASIC reply from the bytes after its 4-byte tag.
func ParseAnonBasicReply(data []byte) (AnonBasicReply, error) {
	if len(data) < 5 {
		return AnonBasicReply{}, fmt.Errorf("%w: basic reply needs 5 bytes, have %d", ErrTooShort, len(data))
	}
	return AnonBasicReply{Clock: binary.LittleEndian.Uint32(data[:4]), Features: data[4]}, nil
}

// lppDataInfo returns a type's size, multiplier and signedness under MeshCore's LPPData rules, which differ from LPPDecode's.
func lppDataInfo(t byte) (size int, mult float32, signed bool) {
	size, mult = 1, 1
	switch t {
	case LPPGPS:
		size = 9
	case LPPPolyline:
		size = 8
	case LPPGyrometer, LPPAccelerometer:
		size = 6
	case LPPGenericSensor, LPPFrequency, LPPDistance, LPPEnergy, LPPUnixTime:
		size = 4
	case LPPColour:
		size = 3
	case LPPAnalogInput, LPPAnalogOutput, LPPLuminosity, LPPTemperature, LPPConcentration, LPPBarometricPressure,
		LPPRelativeHumidity, LPPAltitude, LPPVoltage, LPPCurrent, LPPDirection, LPPPower:
		size = 2
	}
	switch t {
	case LPPCurrent, LPPDistance, LPPEnergy:
		mult = 1000
	case LPPVoltage, LPPAnalogInput, LPPAnalogOutput:
		mult = 100
	case LPPTemperature, LPPBarometricPressure, LPPRelativeHumidity:
		mult = 10
	}
	switch t {
	case LPPAltitude, LPPTemperature, LPPGyrometer, LPPAnalogInput, LPPAnalogOutput, LPPGPS, LPPAccelerometer:
		signed = true
	}
	return size, mult, signed
}

// appendLPPData appends v big-endian; like the firmware, an unsigned type keeps only a negative value's magnitude.
func appendLPPData(b []byte, t byte, v float64) []byte {
	size, mult, signed := lppDataInfo(t)
	f := float32(v)
	neg := f < 0
	if neg {
		f = -f
	}
	u := uint64(uint32(f * mult))
	if signed && neg {
		u = -u
	}
	for i := size - 1; i >= 0; i-- {
		b = append(b, byte(u>>(8*i)))
	}
	return b
}

func lppDataValue(p []byte, t byte) float64 {
	_, mult, signed := lppDataInfo(t)
	var u uint64
	for _, c := range p {
		u = u<<8 | uint64(c)
	}
	if bit := uint64(1) << (8*len(p) - 1); signed && u&bit != 0 {
		return -float64(bit<<1-u) / float64(mult)
	}
	return float64(u) / float64(mult)
}

// AvgMinMaxRequest is a GET_AVG_MIN_MAX request for the window from StartSecsAgo up to EndSecsAgo.
type AvgMinMaxRequest struct {
	StartSecsAgo uint32
	EndSecsAgo   uint32
}

// BuildAvgMinMaxRequest encodes a GET_AVG_MIN_MAX body.
func BuildAvgMinMaxRequest(r AvgMinMaxRequest) []byte {
	b := binary.LittleEndian.AppendUint32([]byte{ReqTypeGetAvgMinMax}, r.StartSecsAgo)
	b = binary.LittleEndian.AppendUint32(b, r.EndSecsAgo)
	return append(b, 0, 0)
}

// ParseAvgMinMaxRequest decodes a GET_AVG_MIN_MAX body, rejecting the non-zero reserved bytes sensors refuse.
func ParseAvgMinMaxRequest(data []byte) (AvgMinMaxRequest, error) {
	if len(data) < 11 {
		return AvgMinMaxRequest{}, fmt.Errorf("%w: avg-min-max request needs 11 bytes, have %d", ErrTooShort, len(data))
	}
	if data[9] != 0 || data[10] != 0 {
		return AvgMinMaxRequest{}, fmt.Errorf("meshcore: avg-min-max request reserved bytes %x are not zero", data[9:11])
	}
	return AvgMinMaxRequest{StartSecsAgo: binary.LittleEndian.Uint32(data[1:5]), EndSecsAgo: binary.LittleEndian.Uint32(data[5:9])}, nil
}

// AvgMinMax is one channel's summary in a GET_AVG_MIN_MAX reply.
type AvgMinMax struct {
	Channel byte
	Type    byte // LPP* type
	Min     float64
	Max     float64
	Avg     float64
}

// AvgMinMaxReply is a GET_AVG_MIN_MAX reply.
type AvgMinMaxReply struct {
	Clock  uint32 // the sensor's clock, in Unix seconds
	Series []AvgMinMax
}

// BuildAvgMinMaxReply encodes a GET_AVG_MIN_MAX reply body.
func BuildAvgMinMaxReply(r AvgMinMaxReply) []byte {
	b := binary.LittleEndian.AppendUint32(nil, r.Clock)
	for _, s := range r.Series {
		b = append(b, s.Channel, s.Type)
		b = appendLPPData(b, s.Type, s.Min)
		b = appendLPPData(b, s.Type, s.Max)
		b = appendLPPData(b, s.Type, s.Avg)
	}
	return b
}

// ParseAvgMinMaxReply decodes a GET_AVG_MIN_MAX reply body.
func ParseAvgMinMaxReply(data []byte) (AvgMinMaxReply, error) {
	if len(data) < 4 {
		return AvgMinMaxReply{}, fmt.Errorf("%w: avg-min-max reply needs 4 bytes, have %d", ErrTooShort, len(data))
	}
	r := AvgMinMaxReply{Clock: binary.LittleEndian.Uint32(data[:4])}
	for data = data[4:]; len(data) >= 2 && data[0] != 0; {
		ch, t := data[0], data[1]
		size, _, _ := lppDataInfo(t)
		if len(data) < 2+3*size {
			return AvgMinMaxReply{}, fmt.Errorf("%w: channel %d type %d needs %d bytes, have %d", ErrTooShort, ch, t, 2+3*size, len(data))
		}
		v := data[2:]
		r.Series = append(r.Series, AvgMinMax{Channel: ch, Type: t,
			Min: lppDataValue(v[:size], t), Max: lppDataValue(v[size:2*size], t), Avg: lppDataValue(v[2*size:3*size], t)})
		data = data[2+3*size:]
	}
	return r, nil
}

// LPPDataValue is one channel's value in MeshCore's own LPPData encoding.
type LPPDataValue struct {
	Channel byte
	Type    byte // LPP* type
	Value   float64
}

// SubscribeRequest is a SUBSCRIBE request for telemetry pushes.
type SubscribeRequest struct {
	PushTag     uint32 // tag the sensor puts on each push
	TimeoutSecs uint16
	MinDeltas   []LPPDataValue // the change on each channel that triggers a push
}

const minDeltasMin, minDeltasMax = 3, 14

// BuildSubscribeRequest encodes a SUBSCRIBE body.
func BuildSubscribeRequest(r SubscribeRequest) ([]byte, error) {
	var deltas []byte
	for _, d := range r.MinDeltas {
		deltas = appendLPPData(append(deltas, d.Channel, d.Type), d.Type, d.Value)
	}
	if len(deltas) < minDeltasMin || len(deltas) > minDeltasMax {
		return nil, fmt.Errorf("meshcore: min deltas encode to %d bytes; sensors accept %d to %d", len(deltas), minDeltasMin, minDeltasMax)
	}
	b := binary.LittleEndian.AppendUint32([]byte{ReqTypeSubscribe}, r.PushTag)
	b = binary.LittleEndian.AppendUint16(b, r.TimeoutSecs)
	b = append(b, 0, byte(len(deltas)))
	return append(b, deltas...), nil
}

// ParseSubscribeRequest decodes a SUBSCRIBE body.
func ParseSubscribeRequest(data []byte) (SubscribeRequest, error) {
	if len(data) < 9 {
		return SubscribeRequest{}, fmt.Errorf("%w: subscribe request needs 9 bytes, have %d", ErrTooShort, len(data))
	}
	r := SubscribeRequest{PushTag: binary.LittleEndian.Uint32(data[1:5]), TimeoutSecs: binary.LittleEndian.Uint16(data[5:7])}
	n := int(data[8])
	if len(data) < 9+n {
		return SubscribeRequest{}, fmt.Errorf("%w: %d bytes of min deltas, have %d", ErrTooShort, n, len(data)-9)
	}
	for d := data[9 : 9+n]; len(d) > 0; {
		if len(d) < 2 {
			return SubscribeRequest{}, fmt.Errorf("%w: min delta header needs 2 bytes, have %d", ErrTooShort, len(d))
		}
		size, _, _ := lppDataInfo(d[1])
		if len(d) < 2+size {
			return SubscribeRequest{}, fmt.Errorf("%w: min delta type %d needs %d bytes, have %d", ErrTooShort, d[1], 2+size, len(d))
		}
		r.MinDeltas = append(r.MinDeltas, LPPDataValue{Channel: d[0], Type: d[1], Value: lppDataValue(d[2:2+size], d[1])})
		d = d[2+size:]
	}
	return r, nil
}

// SubscribeReply is a sensor's answer to a SUBSCRIBE.
type SubscribeReply struct {
	ExpirySecs uint16 // 0 when the sensor refused
	Scope      string // region the pushes are sent in
}

// BuildSubscribeReply encodes a SUBSCRIBE reply body.
func BuildSubscribeReply(r SubscribeReply) []byte {
	return append(binary.LittleEndian.AppendUint16(nil, r.ExpirySecs), append([]byte{0, 0}, r.Scope...)...)
}

// ParseSubscribeReply decodes a SUBSCRIBE reply body.
func ParseSubscribeReply(data []byte) (SubscribeReply, error) {
	if len(data) < 4 {
		return SubscribeReply{}, fmt.Errorf("%w: subscribe reply needs 4 bytes, have %d", ErrTooShort, len(data))
	}
	return SubscribeReply{ExpirySecs: binary.LittleEndian.Uint16(data[:2]), Scope: string(cString(data[4:]))}, nil
}

// BuildUnsubscribeReply encodes an UNSUBSCRIBE reply body: eight zero bytes, then random.
func BuildUnsubscribeReply(random [2]byte) []byte {
	return append(make([]byte, 8), random[:]...)
}

// ParseUnsubscribeReply checks an UNSUBSCRIBE reply body reports success.
func ParseUnsubscribeReply(data []byte) error {
	if len(data) < 10 {
		return fmt.Errorf("%w: unsubscribe reply needs 10 bytes, have %d", ErrTooShort, len(data))
	}
	if !bytes.Equal(data[:8], make([]byte, 8)) {
		return fmt.Errorf("meshcore: unsubscribe reply %x does not report success", data[:8])
	}
	return nil
}

// TelemetryPush is a sensor's telemetry push, after the subscription's push tag.
type TelemetryPush struct {
	Timestamp uint32
	LPP       []byte // Cayenne LPP readings, for LPPDecode
}

// BuildTelemetryPush encodes a telemetry push body.
func BuildTelemetryPush(p TelemetryPush) []byte {
	return append(binary.LittleEndian.AppendUint32(nil, p.Timestamp), p.LPP...)
}

// ParseTelemetryPush decodes a telemetry push body.
func ParseTelemetryPush(data []byte) (TelemetryPush, error) {
	if len(data) < 4 {
		return TelemetryPush{}, fmt.Errorf("%w: telemetry push needs 4 bytes, have %d", ErrTooShort, len(data))
	}
	return TelemetryPush{Timestamp: binary.LittleEndian.Uint32(data[:4]), LPP: data[4:]}, nil
}
