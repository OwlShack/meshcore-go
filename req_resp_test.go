package meshcore

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
	"time"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// sha4 is the firmware's truncated sha256 over parts, as a little-endian uint32.
func sha4(parts ...[]byte) uint32 {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
	}
	return binary.LittleEndian.Uint32(h.Sum(nil))
}

func TestTaggedPlaintext(t *testing.T) {
	got := BuildTaggedPlaintext(0x04030201, []byte{0xAA})
	if want := mustHex(t, "01020304aa"); !bytes.Equal(got, want) {
		t.Fatalf("built %x, want %x", got, want)
	}
	tag, body, err := ParseTaggedPlaintext(got)
	if err != nil || tag != 0x04030201 || !bytes.Equal(body, []byte{0xAA}) {
		t.Fatalf("parsed %x %x %v", tag, body, err)
	}
	if _, _, err := ParseTaggedPlaintext([]byte{1, 2, 3}); !errors.Is(err, ErrTooShort) {
		t.Fatalf("short err = %v", err)
	}
}

func TestLoginRequest(t *testing.T) {
	got, err := BuildLoginRequest(LoginRequest{Password: "hunter2"}, false)
	if err != nil || !bytes.Equal(got, []byte("hunter2")) {
		t.Fatalf("repeater login = %x, %v", got, err)
	}
	room := LoginRequest{SyncSince: 0x11223344, Password: "pw"}
	got, err = BuildLoginRequest(room, true)
	if want := mustHex(t, "44332211"+"7077"); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("room login = %x, %v; want %x", got, err, want)
	}
	back, err := ParseLoginRequest(append(got, 0, 0, 0), true)
	if err != nil || back != room {
		t.Fatalf("parsed room login = %+v, %v", back, err)
	}
	if back, _ := ParseLoginRequest([]byte("abc\x00\x00"), false); back.Password != "abc" {
		t.Fatalf("parsed password = %q", back.Password)
	}
	for _, pw := range []string{"0123456789abcdef", "\x01x"} {
		if _, err := BuildLoginRequest(LoginRequest{Password: pw}, false); err == nil {
			t.Errorf("password %q: expected an error", pw)
		}
	}
	if _, err := ParseLoginRequest([]byte{1, 2, 3}, true); !errors.Is(err, ErrTooShort) {
		t.Fatalf("short room login err = %v", err)
	}
}

func TestLoginReply(t *testing.T) {
	// [0=OK][keep-alive/16][is_admin][perms][rand4][fw_level]
	wire := mustHex(t, "00"+"00"+"01"+"03"+"a1a2a3a4"+"02")
	got, err := ParseLoginReply(wire)
	want := LoginReply{Admin: 1, Permissions: PermACLAdmin, FirmwareLevel: 2}
	if err != nil || got != want {
		t.Fatalf("parsed %+v, %v; want %+v", got, err, want)
	}
	if b := BuildLoginReply(want, [4]byte{0xa1, 0xa2, 0xa3, 0xa4}); !bytes.Equal(b, wire) {
		t.Fatalf("built %x, want %x", b, wire)
	}
	if got, err := ParseLoginReply([]byte("OK\x00\x00")); err != nil || !got.Legacy {
		t.Fatalf("legacy = %+v, %v", got, err)
	}
	if _, err := ParseLoginReply(mustHex(t, "010000000000000000")); err == nil {
		t.Fatal("non-OK code: expected an error")
	}
	if _, err := ParseLoginReply(wire[:8]); !errors.Is(err, ErrTooShort) {
		t.Fatalf("short err = %v", err)
	}
}

func TestRequests(t *testing.T) {
	r := [4]byte{9, 8, 7, 6}
	if got, want := BuildRequest(ReqTypeGetStatus, r), mustHex(t, "01"+"00000000"+"09080706"); !bytes.Equal(got, want) {
		t.Fatalf("status = %x, want %x", got, want)
	}
	tel := BuildTelemetryRequest(TelemPermBase, r)
	if want := mustHex(t, "03"+"fe000000"+"09080706"); !bytes.Equal(tel, want) {
		t.Fatalf("telemetry = %x, want %x", tel, want)
	}
	if perms, err := ParseTelemetryRequest(tel); err != nil || perms != TelemPermBase {
		t.Fatalf("telemetry perms = %x, %v", perms, err)
	}
	if _, err := ParseTelemetryRequest([]byte{3}); !errors.Is(err, ErrTooShort) {
		t.Fatalf("short err = %v", err)
	}
}

func TestKeepAlive(t *testing.T) {
	body := BuildKeepAliveRequest(0x0a0b0c0d)
	if want := mustHex(t, "02"+"0d0c0b0a"); !bytes.Equal(body, want) {
		t.Fatalf("keep-alive = %x, want %x", body, want)
	}
	pub := bytes.Repeat([]byte{0x5a}, PubKeySize)
	nine := mustHex(t, "01000000"+"02"+"0d0c0b0a")
	if got, want := KeepAliveAckHash(1, 0x0a0b0c0d, pub), sha4(nine, pub); got != want {
		t.Fatalf("ack hash = %08x, want %08x", got, want)
	}
	ack, err := ParseKeepAliveAck(mustHex(t, "efbeadde07"))
	if err != nil || ack != (KeepAliveAck{CRC: 0xdeadbeef, Unsynced: 7}) {
		t.Fatalf("ack = %+v, %v", ack, err)
	}
	if _, err := ParseKeepAliveAck(mustHex(t, "efbeadde")); !errors.Is(err, ErrTooShort) {
		t.Fatalf("4-byte ack err = %v", err)
	}
}

func TestStats(t *testing.T) {
	common := CommonStats{
		BattMilliVolts: 0x0102, TxQueueLen: 0x0304, NoiseFloor: -110, LastRSSI: -80,
		PacketsRecv: 5, PacketsSent: 6, AirTimeSecs: 7, UpTimeSecs: 8,
		SentFlood: 9, SentDirect: 10, RecvFlood: 11, RecvDirect: 12,
		ErrEvents: 13, LastSNR: -22, DirectDups: 14, FloodDups: 15,
	}
	head := "0201" + "0403" + "92ff" + "b0ff" + "05000000" + "06000000" + "07000000" + "08000000" +
		"09000000" + "0a000000" + "0b000000" + "0c000000" + "0d00" + "eaff" + "0e00" + "0f00"

	rep := RepeaterStats{CommonStats: common, RxAirTimeSecs: 16, RecvErrors: 17}
	wire := mustHex(t, head+"10000000"+"11000000")
	if len(wire) != 56 {
		t.Fatalf("repeater vector is %d bytes", len(wire))
	}
	if got := BuildRepeaterStats(rep); !bytes.Equal(got, wire) {
		t.Fatalf("repeater built %x\nwant %x", got, wire)
	}
	if got, err := ParseRepeaterStats(append(wire, 0, 0)); err != nil || got != rep {
		t.Fatalf("repeater parsed %+v, %v", got, err)
	}
	if _, err := ParseRepeaterStats(wire[:55]); !errors.Is(err, ErrTooShort) {
		t.Fatalf("short repeater err = %v", err)
	}

	room := RoomStats{CommonStats: common, Posted: 0x1234, PostPushes: 0x5678}
	wire = mustHex(t, head+"3412"+"7856")
	if len(wire) != 52 {
		t.Fatalf("room vector is %d bytes", len(wire))
	}
	if got := BuildRoomStats(room); !bytes.Equal(got, wire) {
		t.Fatalf("room built %x\nwant %x", got, wire)
	}
	if got, err := ParseRoomStats(wire); err != nil || got != room {
		t.Fatalf("room parsed %+v, %v", got, err)
	}
}

func TestAccessList(t *testing.T) {
	entries := []ACLEntry{{Prefix: [6]byte{1, 2, 3, 4, 5, 6}, Permissions: PermACLAdmin}, {Prefix: [6]byte{7, 8, 9, 10, 11, 12}, Permissions: PermACLReadWrite}}
	wire := mustHex(t, "010203040506"+"03"+"0708090a0b0c"+"02")
	if got := BuildAccessListReply(entries); !bytes.Equal(got, wire) {
		t.Fatalf("built %x, want %x", got, wire)
	}
	got, err := ParseAccessListReply(append(wire, make([]byte, 10)...))
	if err != nil || !reflect.DeepEqual(got, entries) {
		t.Fatalf("parsed %+v, %v", got, err)
	}
}

func TestNeighbours(t *testing.T) {
	req := NeighboursRequest{Count: 10, Offset: 0x0102, OrderBy: NeighboursStrongestFirst, PrefixLen: 4}
	wire := mustHex(t, "06"+"00"+"0a"+"0201"+"02"+"04"+"a0b0c0d0")
	if got := BuildNeighboursRequest(req, [4]byte{0xa0, 0xb0, 0xc0, 0xd0}); !bytes.Equal(got, wire) {
		t.Fatalf("request built %x, want %x", got, wire)
	}
	if got, err := ParseNeighboursRequest(wire); err != nil || got != req {
		t.Fatalf("request parsed %+v, %v", got, err)
	}
	wire[1] = 1
	if _, err := ParseNeighboursRequest(wire); err == nil {
		t.Fatal("version 1: expected an error")
	}

	reply := NeighboursReply{Total: 3, Neighbours: []Neighbour{
		{Prefix: []byte{1, 2}, HeardSecsAgo: 60, SNR: -10},
		{Prefix: []byte{3, 4}, HeardSecsAgo: 0x01020304, SNR: 40},
	}}
	wire = mustHex(t, "0300"+"0200"+"0102"+"3c000000"+"f6"+"0304"+"04030201"+"28")
	if got := BuildNeighboursReply(reply); !bytes.Equal(got, wire) {
		t.Fatalf("reply built %x, want %x", got, wire)
	}
	if got, err := ParseNeighboursReply(append(wire, 0, 0), 2); err != nil || !reflect.DeepEqual(got, reply) {
		t.Fatalf("reply parsed %+v, %v", got, err)
	}
	if _, err := ParseNeighboursReply(wire[:len(wire)-1], 2); !errors.Is(err, ErrTooShort) {
		t.Fatalf("truncated reply err = %v", err)
	}
}

func TestNeighboursPrefixLenClamp(t *testing.T) {
	big := NeighboursRequest{PrefixLen: 200}
	wire := BuildNeighboursRequest(big, [4]byte{})
	if wire[6] != PubKeySize {
		t.Fatalf("built prefix length %d, want %d", wire[6], PubKeySize)
	}
	wire[6] = 200
	if got, err := ParseNeighboursRequest(wire); err != nil || got.PrefixLen != PubKeySize {
		t.Fatalf("parsed %+v, %v", got, err)
	}
	full := bytes.Repeat([]byte{7}, PubKeySize)
	reply := BuildNeighboursReply(NeighboursReply{Total: 1, Neighbours: []Neighbour{{Prefix: full, HeardSecsAgo: 9, SNR: -4}}})
	got, err := ParseNeighboursReply(reply, 200)
	if err != nil || len(got.Neighbours) != 1 || !bytes.Equal(got.Neighbours[0].Prefix, full) || got.Neighbours[0].HeardSecsAgo != 9 {
		t.Fatalf("reply parsed %+v, %v", got, err)
	}
}

func TestOwnerInfo(t *testing.T) {
	o := OwnerInfo{FirmwareVersion: "v1.17.1", Name: "Hilltop", Owner: "Kiri\nPh 021"}
	wire := []byte("v1.17.1\nHilltop\nKiri\nPh 021")
	if got := BuildOwnerInfoReply(o); !bytes.Equal(got, wire) {
		t.Fatalf("built %q", got)
	}
	if got, err := ParseOwnerInfoReply(append(wire, 0, 0)); err != nil || got != o {
		t.Fatalf("parsed %+v, %v", got, err)
	}
	if got, _ := ParseOwnerInfoReply([]byte("v1")); got != (OwnerInfo{FirmwareVersion: "v1"}) {
		t.Fatalf("partial = %+v", got)
	}
}

func TestAnonOwnerAndBasicReplies(t *testing.T) {
	got, err := ParseAnonOwnerReply(append(mustHex(t, "10203040"), "Hilltop\nKiri\nPh 021\x00\x00"...))
	if err != nil || got != (AnonOwnerReply{Clock: 0x40302010, Name: "Hilltop", Owner: "Kiri\nPh 021"}) {
		t.Fatalf("owner = %+v, %v", got, err)
	}
	basic, err := ParseAnonBasicReply(mustHex(t, "10203040"+"83"))
	if err != nil || basic.Clock != 0x40302010 || basic.Features&AnonFeatureBridgeMask != AnonFeatureBridgeESPNow || basic.Features&AnonFeatureDisabled == 0 {
		t.Fatalf("basic = %+v, %v", basic, err)
	}
	if _, err := ParseAnonBasicReply(mustHex(t, "10203040")); !errors.Is(err, ErrTooShort) {
		t.Fatalf("short basic err = %v", err)
	}
	if _, err := ParseAnonOwnerReply([]byte{1}); !errors.Is(err, ErrTooShort) {
		t.Fatalf("short owner err = %v", err)
	}
}

func TestBuildAnonRequest(t *testing.T) {
	got, err := BuildAnonRequest(AnonReqTypeOwner, []byte{0xA1}, 1)
	if want := []byte{AnonReqTypeOwner, 0x01, 0xA1}; err != nil || !bytes.Equal(got, want) {
		t.Fatalf("owner request = %x, %v; want %x", got, err, want)
	}
}

func TestLPPData(t *testing.T) {
	// MeshCore LPPData: big-endian, voltage unsigned x100, temperature signed x10, humidity 2 bytes x10.
	for _, c := range []struct {
		typ  byte
		v    float64
		wire string
	}{
		{LPPVoltage, 3.75, "0177"},
		{LPPVoltage, -3.75, "0177"},
		{LPPTemperature, -2.5, "ffe7"},
		{LPPRelativeHumidity, 55.5, "022b"},
		{LPPCurrent, 1.25, "04e2"},
		{LPPAltitude, -3, "fffd"},
		{LPPGenericSensor, 70000, "00011170"},
		{LPPPresence, 1, "01"},
	} {
		got := appendLPPData(nil, c.typ, c.v)
		if hex.EncodeToString(got) != c.wire {
			t.Errorf("type %d %v = %x, want %s", c.typ, c.v, got, c.wire)
		}
		want := c.v
		if c.v < 0 && c.typ == LPPVoltage {
			want = -c.v
		}
		if back := lppDataValue(got, c.typ); back != want {
			t.Errorf("type %d %x decodes to %v, want %v", c.typ, got, back, want)
		}
	}
}

func TestAvgMinMax(t *testing.T) {
	req := AvgMinMaxRequest{StartSecsAgo: 3600, EndSecsAgo: 60}
	wire := mustHex(t, "04"+"100e0000"+"3c000000"+"0000")
	if got := BuildAvgMinMaxRequest(req); !bytes.Equal(got, wire) {
		t.Fatalf("request built %x, want %x", got, wire)
	}
	if got, err := ParseAvgMinMaxRequest(wire); err != nil || got != req {
		t.Fatalf("request parsed %+v, %v", got, err)
	}
	if _, err := ParseAvgMinMaxRequest(wire[:10]); !errors.Is(err, ErrTooShort) {
		t.Fatalf("10-byte request err = %v", err)
	}
	for _, i := range []int{9, 10} {
		bad := bytes.Clone(wire)
		bad[i] = 1
		if _, err := ParseAvgMinMaxRequest(bad); err == nil {
			t.Fatalf("reserved byte %d set: expected an error", i)
		}
	}

	reply := AvgMinMaxReply{Clock: 0x01020304, Series: []AvgMinMax{
		{Channel: 1, Type: LPPVoltage, Min: 3.5, Max: 4.25, Avg: 3.75},
		{Channel: 2, Type: LPPTemperature, Min: -2.5, Max: 20, Avg: 8.5},
	}}
	wire = mustHex(t, "04030201"+"0174"+"015e"+"01a9"+"0177"+"0267"+"ffe7"+"00c8"+"0055")
	if got := BuildAvgMinMaxReply(reply); !bytes.Equal(got, wire) {
		t.Fatalf("reply built %x\nwant %x", got, wire)
	}
	if got, err := ParseAvgMinMaxReply(append(wire, 0, 0, 0)); err != nil || !reflect.DeepEqual(got, reply) {
		t.Fatalf("reply parsed %+v, %v", got, err)
	}
	if _, err := ParseAvgMinMaxReply(wire[:len(wire)-1]); !errors.Is(err, ErrTooShort) {
		t.Fatalf("truncated reply err = %v", err)
	}
}

func TestSubscribe(t *testing.T) {
	req := SubscribeRequest{PushTag: 0xcafef00d, TimeoutSecs: 600, MinDeltas: []LPPDataValue{
		{Channel: 1, Type: LPPVoltage, Value: 0.1},
		{Channel: 2, Type: LPPTemperature, Value: 0.5},
	}}
	wire := mustHex(t, "08"+"0df0feca"+"5802"+"00"+"08"+"0174000a"+"02670005")
	got, err := BuildSubscribeRequest(req)
	if err != nil || !bytes.Equal(got, wire) {
		t.Fatalf("request built %x, %v\nwant %x", got, err, wire)
	}
	if back, err := ParseSubscribeRequest(append(wire, 0, 0)); err != nil || !reflect.DeepEqual(back, req) {
		t.Fatalf("request parsed %+v, %v", back, err)
	}
	if _, err := ParseSubscribeRequest(wire[:len(wire)-1]); !errors.Is(err, ErrTooShort) {
		t.Fatalf("truncated request err = %v", err)
	}
	if _, err := BuildSubscribeRequest(SubscribeRequest{}); err == nil {
		t.Fatal("no min deltas: expected an error")
	}

	reply := SubscribeReply{ExpirySecs: 600, Scope: "nz"}
	wire = mustHex(t, "5802"+"0000"+"6e7a")
	if got := BuildSubscribeReply(reply); !bytes.Equal(got, wire) {
		t.Fatalf("reply built %x, want %x", got, wire)
	}
	if back, err := ParseSubscribeReply(append(wire, 0)); err != nil || back != reply {
		t.Fatalf("reply parsed %+v, %v", back, err)
	}

	unsub := BuildUnsubscribeReply([2]byte{0xee, 0xff})
	if want := mustHex(t, "0000000000000000"+"eeff"); !bytes.Equal(unsub, want) {
		t.Fatalf("unsubscribe reply = %x", unsub)
	}
	if err := ParseUnsubscribeReply(unsub); err != nil {
		t.Fatal(err)
	}
	unsub[0] = 1
	if err := ParseUnsubscribeReply(unsub); err == nil {
		t.Fatal("non-zero unsubscribe reply: expected an error")
	}

	push := TelemetryPush{Timestamp: 0x01020304, LPP: mustHex(t, "017401a9")}
	wire = mustHex(t, "04030201"+"017401a9")
	if got := BuildTelemetryPush(push); !bytes.Equal(got, wire) {
		t.Fatalf("push built %x, want %x", got, wire)
	}
	if back, err := ParseTelemetryPush(wire); err != nil || !reflect.DeepEqual(back, push) {
		t.Fatalf("push parsed %+v, %v", back, err)
	}
}

func TestTextFlags(t *testing.T) {
	if got := TextFlags(TxtTypeCLIData); got != 0x04 {
		t.Fatalf("TextFlags(CLIData) = %#x", got)
	}
}

func TestSignedText(t *testing.T) {
	author := [4]byte{0xab, 0xab, 0xab, 0xab}
	got := BuildSignedTextPlaintext(time.Unix(0x01020304, 0), author, []byte("hi"), 6)
	// [ts4][(2<<2)|attempt&3][author prefix4][text]
	wire := mustHex(t, "04030201"+"0a"+"abababab"+"6869")
	if !bytes.Equal(got, wire) {
		t.Fatalf("built %x, want %x", got, wire)
	}
	m, err := ParseTextPlaintext(append(wire, 0, 0, 0))
	if err != nil || m.TxtType != TxtTypeSignedPlain || m.Attempt != 2 || !bytes.Equal(m.Author, author[:]) || string(m.Text) != "hi" {
		t.Fatalf("parsed %+v, %v", m, err)
	}
	receiver := bytes.Repeat([]byte{0x11}, PubKeySize)
	if got, want := SignedTextAckHash(append(wire, 0, 0), receiver), sha4(wire, receiver); got != want {
		t.Fatalf("signed ack = %08x, want %08x", got, want)
	}
	if _, err := ParseTextPlaintext(wire[:8]); !errors.Is(err, ErrTooShort) {
		t.Fatalf("short signed err = %v", err)
	}
}

func TestParseTextPlaintextAndAck(t *testing.T) {
	sender := bytes.Repeat([]byte{0x22}, PubKeySize)
	ts := time.Unix(0x01020304, 0)

	plain := append(BuildTextPlaintextWithAttempt(ts, TextFlags(TxtTypePlain), []byte("kia ora"), 1), 0, 0, 0)
	m, err := ParseTextPlaintext(plain)
	if err != nil || m.Timestamp != 0x01020304 || m.TxtType != TxtTypePlain || m.Attempt != 1 || m.Author != nil || string(m.Text) != "kia ora" {
		t.Fatalf("parsed %+v, %v", m, err)
	}
	body := mustHex(t, "04030201"+"01"+hex.EncodeToString([]byte("kia ora")))
	ack := BuildTextAck(plain, sender, 0x99)
	if want := append(binary.LittleEndian.AppendUint32(nil, sha4(body, sender)), 0, 0x99); !bytes.Equal(ack, want) {
		t.Fatalf("ack = %x, want %x", ack, want)
	}

	// Past attempt 3 the [0x00][attempt] tail is outside the hash but lands in ack[4].
	retry := append(BuildTextPlaintextWithAttempt(ts, 0, []byte("kia ora"), 5), 0)
	if m, _ := ParseTextPlaintext(retry); m.Attempt != 5 || string(m.Text) != "kia ora" {
		t.Fatalf("retry parsed %+v", m)
	}
	body[4] = 5 & 3
	ack = BuildTextAck(retry, sender, 0x99)
	if want := append(binary.LittleEndian.AppendUint32(nil, sha4(body, sender)), 5, 0x99); !bytes.Equal(ack, want) {
		t.Fatalf("retry ack = %x, want %x", ack, want)
	}
	if TextAckHash(retry, sender) != CalcAckHash(body, sender) {
		t.Fatal("TextAckHash disagrees with CalcAckHash over the text")
	}
	if _, err := ParseTextPlaintext([]byte{1, 2, 3, 4}); !errors.Is(err, ErrTooShort) {
		t.Fatalf("short err = %v", err)
	}
}
