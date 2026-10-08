package meshcore

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestDeriveRegionKey_Deterministic(t *testing.T) {
	k1 := DeriveRegionKey("#ch-fr")
	k2 := DeriveRegionKey("#ch-fr")
	if k1 != k2 {
		t.Error("same name should produce same key")
	}
}

func TestDeriveRegionKey_DifferentNames(t *testing.T) {
	k1 := DeriveRegionKey("#ch-fr")
	k2 := DeriveRegionKey("#us-west")
	if k1 == k2 {
		t.Error("different names should produce different keys")
	}
}

func TestRegionKey_IsZero(t *testing.T) {
	var zero RegionKey
	if !zero.IsZero() {
		t.Error("zero key should be zero")
	}
	k := DeriveRegionKey("#test")
	if k.IsZero() {
		t.Error("derived key should not be zero")
	}
}

func TestRegionKey_CalcTransportCode_Deterministic(t *testing.T) {
	key := DeriveRegionKey("#ch-fr")
	c1 := key.CalcTransportCode(PayloadTypeTxtMsg, []byte{0x01, 0x02})
	c2 := key.CalcTransportCode(PayloadTypeTxtMsg, []byte{0x01, 0x02})
	if c1 != c2 {
		t.Errorf("same inputs should produce same code: got %04X and %04X", c1, c2)
	}
}

func TestRegionKey_CalcTransportCode_DifferentPayloads(t *testing.T) {
	key := DeriveRegionKey("#ch-fr")
	c1 := key.CalcTransportCode(PayloadTypeTxtMsg, []byte{0x01, 0x02})
	c2 := key.CalcTransportCode(PayloadTypeTxtMsg, []byte{0x03, 0x04})
	if c1 == c2 {
		t.Error("different payloads should produce different codes")
	}
}

func TestRegionKey_CalcTransportCode_DifferentPayloadTypes(t *testing.T) {
	key := DeriveRegionKey("#ch-fr")
	payload := []byte{0x01, 0x02, 0x03}
	c1 := key.CalcTransportCode(PayloadTypeTxtMsg, payload)
	c2 := key.CalcTransportCode(PayloadTypeAdvert, payload)
	if c1 == c2 {
		t.Error("different payload types should produce different codes")
	}
}

func TestRegionKey_CalcTransportCode_DifferentKeys(t *testing.T) {
	k1 := DeriveRegionKey("#ch-fr")
	k2 := DeriveRegionKey("#us-west")
	payload := []byte{0x01, 0x02, 0x03}
	c1 := k1.CalcTransportCode(PayloadTypeTxtMsg, payload)
	c2 := k2.CalcTransportCode(PayloadTypeTxtMsg, payload)
	if c1 == c2 {
		t.Error("different keys should produce different codes")
	}
}

func TestRegionKey_CalcTransportCode_NotReserved(t *testing.T) {
	key := DeriveRegionKey("#test-reserved")
	for i := range 1000 {
		payload := []byte{byte(i >> 8), byte(i)}
		code := key.CalcTransportCode(PayloadTypeTxtMsg, payload)
		if code == 0x0000 {
			t.Fatalf("code 0x0000 should be reserved (i=%d)", i)
		}
		if code == 0xFFFF {
			t.Fatalf("code 0xFFFF should be reserved (i=%d)", i)
		}
	}
}

func TestNewRegionFromHashtag(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
	}{
		{"#ch-fr", "#ch-fr"},
		{"ch-fr", "#ch-fr"},
		{"$private", "$private"},
	}
	for _, tt := range tests {
		r := NewRegionFromHashtag(tt.input)
		if r.Name != tt.wantName {
			t.Errorf("NewRegionFromHashtag(%q).Name = %q, want %q", tt.input, r.Name, tt.wantName)
		}
		if r.Key.IsZero() {
			t.Errorf("NewRegionFromHashtag(%q).Key should not be zero", tt.input)
		}
	}
}

func TestRegion_MatchesPacket(t *testing.T) {
	r := NewRegionFromHashtag("#ch-fr")
	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	payloadType := PayloadTypeTxtMsg

	code := r.Key.CalcTransportCode(payloadType, payload)

	pkt := &Packet{
		Header:         MakeHeader(RouteTypeTransportFlood, payloadType, 0),
		PathLength:     0,
		Payload:        payload,
		TransportCode1: code,
	}

	if !r.MatchesPacket(pkt) {
		t.Error("region should match packet with correct transport code")
	}

	pkt.TransportCode1 = code + 1
	if r.MatchesPacket(pkt) {
		t.Error("region should not match packet with wrong transport code")
	}
}

func TestRegion_MatchesPacket_NonTransportReturnsFalse(t *testing.T) {
	r := NewRegionFromHashtag("#ch-fr")
	pkt := &Packet{
		Header:     MakeHeader(RouteTypeFlood, PayloadTypeTxtMsg, 0),
		PathLength: 0,
		Payload:    []byte{0x01},
	}
	if r.MatchesPacket(pkt) {
		t.Error("non-transport packet should not match any region")
	}
}

func TestRegion_DenyFlags(t *testing.T) {
	r := &Region{Flags: 0}
	if r.DenyFlood() {
		t.Error("no flags set should not deny flood")
	}
	if r.DenyDirect() {
		t.Error("no flags set should not deny direct")
	}

	r.Flags = RegionDenyFlood
	if !r.DenyFlood() {
		t.Error("DENY_FLOOD set should deny flood")
	}
	if r.DenyDirect() {
		t.Error("only DENY_FLOOD set should not deny direct")
	}

	r.Flags = RegionDenyFlood | RegionDenyDirect
	if !r.DenyFlood() || !r.DenyDirect() {
		t.Error("both flags set should deny both")
	}
}

func TestIsValidRegionNameChar(t *testing.T) {
	valid := []byte{'-', '$', '#', '0', '9', 'A', 'Z', 'a', 'z'}
	for _, c := range valid {
		if !IsValidRegionNameChar(c) {
			t.Errorf("expected %q to be valid", c)
		}
	}

	invalid := []byte{' ', '!', '@', '.', ',', '/', '(', ')'}
	for _, c := range invalid {
		if IsValidRegionNameChar(c) {
			t.Errorf("expected %q to be invalid", c)
		}
	}
}

func TestRegionKey_CalcTransportCode_Vectors(t *testing.T) {
	key := DeriveRegionKey("#nz")
	if got := hex.EncodeToString(key[:]); got != "eb87ee8817ba71315ac7be9c733b523a" {
		t.Fatalf("DeriveRegionKey(#nz) = %s", got)
	}
	cases := []struct {
		payload string
		want    uint16
	}{
		{"010203", 0x38ab},
		{"00c5ec", 0x0001},
		{"01b654", 0xfffe},
	}
	for _, c := range cases {
		p, _ := hex.DecodeString(c.payload)
		if got := key.CalcTransportCode(PayloadTypeTxtMsg, p); got != c.want {
			t.Errorf("CalcTransportCode(%s) = 0x%04x, want 0x%04x", c.payload, got, c.want)
		}
	}
}

func TestNewRegionFromKey(t *testing.T) {
	var key RegionKey
	copy(key[:], bytes.Repeat([]byte{0xA5}, RegionKeySize))
	r := NewRegionFromKey("$private", key)
	if r.Name != "$private" || r.Key != key || r.ID != 0 || r.Parent != 0 || r.Flags != 0 {
		t.Fatalf("NewRegionFromKey() = %+v", r)
	}
	if r.Key == DeriveRegionKey("$private") {
		t.Fatal("NewRegionFromKey re-derived the key from the name")
	}
}

func TestNewRegion_KeyDerivation(t *testing.T) {
	cases := []struct {
		name    string
		wantKey RegionKey
	}{
		{"nz", DeriveRegionKey("#nz")},
		{"#nz", DeriveRegionKey("#nz")},
		{"$private", RegionKey{}},
	}
	for _, c := range cases {
		r := NewRegion(c.name)
		if r.Name != c.name {
			t.Errorf("NewRegion(%q).Name = %q, want it unchanged", c.name, r.Name)
		}
		if r.Key != c.wantKey {
			t.Errorf("NewRegion(%q).Key = %x, want %x", c.name, r.Key, c.wantKey)
		}
	}
}

func TestRegion_MatchesPacket_ZeroKeyNeverMatches(t *testing.T) {
	r := NewRegion("$private")
	pkt := &Packet{
		Header:  MakeHeader(RouteTypeTransportFlood, PayloadTypeTxtMsg, 0),
		Payload: []byte{0x01, 0x02},
	}
	pkt.TransportCode1 = r.CalcTransportCode(pkt)
	if r.MatchesPacket(pkt) {
		t.Error("a region without a key matched a packet")
	}
}

func TestPacket_SetScope(t *testing.T) {
	r := NewRegion("nz")
	pkt := &Packet{
		Header:  MakeHeader(RouteTypeFlood, PayloadTypeGrpTxt, 0),
		Payload: []byte{0x01, 0x02, 0x03},
	}

	v1 := &Packet{Header: MakeHeader(RouteTypeFlood, PayloadTypeGrpTxt, 1)}
	if v1.SetScope(r); v1.PayloadVer() != 1 {
		t.Fatalf("SetScope changed payload version to %d", v1.PayloadVer())
	}

	pkt.SetScope(r)
	data, err := pkt.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	got, err := PacketFromBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.RouteType() != RouteTypeTransportFlood || got.PayloadType() != PayloadTypeGrpTxt {
		t.Fatalf("header = %s/%s, want TRANSPORT_FLOOD/GRP_TXT", got.RouteTypeString(), got.PayloadTypeString())
	}
	if !r.MatchesPacket(got) || got.TransportCode2 != 0 {
		t.Fatalf("codes = %04x/%04x, want region code/0", got.TransportCode1, got.TransportCode2)
	}

	pkt.SetScope(nil)
	if pkt.RouteType() != RouteTypeFlood || pkt.TransportCode1 != 0 {
		t.Fatalf("after SetScope(nil): route %s code %04x, want FLOOD/0", pkt.RouteTypeString(), pkt.TransportCode1)
	}

	pkt.SetScope(NewRegion("$private"))
	if pkt.RouteType() != RouteTypeFlood {
		t.Fatalf("keyless scope gave route %s, want FLOOD", pkt.RouteTypeString())
	}

	direct := &Packet{Header: MakeHeader(RouteTypeDirect, PayloadTypeTxtMsg, 0), Payload: []byte{0x01}}
	direct.SetScope(r)
	if direct.RouteType() != RouteTypeDirect || direct.TransportCode1 != 0 {
		t.Fatal("SetScope changed a direct packet")
	}
}
