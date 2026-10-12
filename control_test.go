package meshcore

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
)

func TestControlFromBytes(t *testing.T) {
	tests := []struct {
		name      string
		hex       string
		wantErr   bool
		wantFlags byte
		wantData  string // hex
	}{
		{
			name:      "flags only no data",
			hex:       "A5",
			wantFlags: 0xA5,
			wantData:  "",
		},
		{
			name:      "flags with data",
			hex:       "8ABC12DEF0",
			wantFlags: 0x8A,
			wantData:  "BC12DEF0",
		},
		{
			name:      "flags with large data",
			hex:       "9A" + "0102030405060708090A0B0C0D0E0F101112131415",
			wantFlags: 0x9A,
			wantData:  "0102030405060708090A0B0C0D0E0F101112131415",
		},
		{
			name:    "empty input returns error",
			hex:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := hex.DecodeString(tt.hex)
			if err != nil {
				t.Fatalf("bad test hex: %v", err)
			}

			ctrl, err := ControlFromBytes(data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ctrl.Flags != tt.wantFlags {
				t.Errorf("Flags = 0x%02x, want 0x%02x", ctrl.Flags, tt.wantFlags)
			}
			if tt.wantData != "" {
				if got := hex.EncodeToString(ctrl.Data); !strings.EqualFold(got, tt.wantData) {
					t.Errorf("Data = %s, want %s", got, tt.wantData)
				}
			} else {
				if len(ctrl.Data) != 0 {
					t.Errorf("Data = %v, want empty", ctrl.Data)
				}
			}
		})
	}
}

func TestControlToBytes(t *testing.T) {
	tests := []struct {
		name    string
		ctrl    Control
		wantHex string
	}{
		{
			name: "flags only",
			ctrl: Control{
				Flags: 0x8A,
				Data:  []byte{},
			},
			wantHex: "8A",
		},
		{
			name: "flags with data",
			ctrl: Control{
				Flags: 0x8A,
				Data:  []byte{0xBC, 0x12, 0xDE, 0xF0},
			},
			wantHex: "8ABC12DEF0",
		},
		{
			name: "flags with large data",
			ctrl: Control{
				Flags: 0x9A,
				Data: []byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15,
				},
			},
			wantHex: "9A0102030405060708090A0B0C0D0E0F101112131415",
		},
		{
			name: "zero flags",
			ctrl: Control{
				Flags: 0x00,
				Data:  []byte{0xAA, 0xBB, 0xCC},
			},
			wantHex: "00AABBCC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.ctrl.ToBytes()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotHex := hex.EncodeToString(got); !strings.EqualFold(gotHex, tt.wantHex) {
				t.Errorf("ToBytes() = %s, want %s", gotHex, tt.wantHex)
			}
		})
	}
}

func TestControlSubType(t *testing.T) {
	tests := []struct {
		name     string
		flags    byte
		wantSubT byte
	}{
		{
			name:     "discover request",
			flags:    0x80,
			wantSubT: 0x08,
		},
		{
			name:     "discover response",
			flags:    0x90,
			wantSubT: 0x09,
		},
		{
			name:     "upper 4 bits extraction",
			flags:    0xB5,
			wantSubT: 0x0B,
		},
		{
			name:     "zero flags",
			flags:    0x00,
			wantSubT: 0x00,
		},
		{
			name:     "max flags",
			flags:    0xFF,
			wantSubT: 0x0F,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := &Control{Flags: tt.flags}
			got := ctrl.SubType()
			if got != tt.wantSubT {
				t.Errorf("SubType() = 0x%02x, want 0x%02x", got, tt.wantSubT)
			}
		})
	}
}

func TestControlRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		ctrl Control
	}{
		{
			name: "flags only",
			ctrl: Control{
				Flags: 0x8A,
				Data:  []byte{},
			},
		},
		{
			name: "flags with data",
			ctrl: Control{
				Flags: 0x9B,
				Data:  []byte{0x01, 0x02, 0x03, 0x04, 0x05},
			},
		},
		{
			name: "large data",
			ctrl: Control{
				Flags: 0xBC,
				Data: []byte{
					0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77,
					0x88, 0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire, err := tt.ctrl.ToBytes()
			if err != nil {
				t.Fatalf("ToBytes(): %v", err)
			}

			decoded, err := ControlFromBytes(wire)
			if err != nil {
				t.Fatalf("ControlFromBytes(): %v", err)
			}

			if decoded.Flags != tt.ctrl.Flags {
				t.Errorf("Flags = 0x%02x, want 0x%02x", decoded.Flags, tt.ctrl.Flags)
			}
			if got := hex.EncodeToString(decoded.Data); got != hex.EncodeToString(tt.ctrl.Data) {
				t.Errorf("Data = %s, want %s", got, hex.EncodeToString(tt.ctrl.Data))
			}
		})
	}
}

func TestControlDiscoverRequest(t *testing.T) {
	data := []byte{0x81, 0x04}
	tag := make([]byte, 4)
	binary.LittleEndian.PutUint32(tag, 0xAABBCCDD)
	data = append(data, tag...)
	since := make([]byte, 4)
	binary.LittleEndian.PutUint32(since, 0x12345678)
	data = append(data, since...)

	ctrl, err := ControlFromBytes(data)
	if err != nil {
		t.Fatalf("ControlFromBytes: %v", err)
	}

	req, err := ctrl.DiscoverRequest()
	if err != nil {
		t.Fatalf("DiscoverRequest: %v", err)
	}
	if !req.PrefixOnly {
		t.Error("PrefixOnly = false, want true")
	}
	if req.TypeFilter != 0x04 {
		t.Errorf("TypeFilter = 0x%02X, want 0x04", req.TypeFilter)
	}
	if req.Tag != 0xAABBCCDD {
		t.Errorf("Tag = 0x%08X, want 0xAABBCCDD", req.Tag)
	}
	if req.Since != 0x12345678 {
		t.Errorf("Since = 0x%08X, want 0x12345678", req.Since)
	}
}

func TestControlDiscoverRequest_NoPrefixOnly(t *testing.T) {
	data := []byte{0x80, 0x02}
	tag := make([]byte, 4)
	binary.LittleEndian.PutUint32(tag, 0x11111111)
	data = append(data, tag...)

	ctrl, err := ControlFromBytes(data)
	if err != nil {
		t.Fatalf("ControlFromBytes: %v", err)
	}

	req, err := ctrl.DiscoverRequest()
	if err != nil {
		t.Fatalf("DiscoverRequest: %v", err)
	}
	if req.PrefixOnly {
		t.Error("PrefixOnly = true, want false")
	}
	if req.Since != 0 {
		t.Errorf("Since = %d, want 0 (absent)", req.Since)
	}
}

func TestControlDiscoverRequest_WrongSubType(t *testing.T) {
	ctrl := &Control{Flags: 0x90, Data: make([]byte, 10)}
	_, err := ctrl.DiscoverRequest()
	if err == nil {
		t.Error("expected error for wrong subtype")
	}
}

func TestControlDiscoverRequest_TooShort(t *testing.T) {
	ctrl := &Control{Flags: 0x80, Data: []byte{0x01, 0x02}}
	_, err := ctrl.DiscoverRequest()
	if err == nil {
		t.Error("expected error for short data")
	}
}

func TestControlDiscoverResponse(t *testing.T) {
	var pubkey [32]byte
	for i := range pubkey {
		pubkey[i] = byte(i)
	}
	data := []byte{0x92, byte(0xF4)} // 0xF4 = -12 (int8) on wire = -3.0 dB (quarter-dB / 4)
	tag := make([]byte, 4)
	binary.LittleEndian.PutUint32(tag, 0xDEADBEEF)
	data = append(data, tag...)
	data = append(data, pubkey[:]...)

	ctrl, err := ControlFromBytes(data)
	if err != nil {
		t.Fatalf("ControlFromBytes: %v", err)
	}

	resp, err := ctrl.DiscoverResponse()
	if err != nil {
		t.Fatalf("DiscoverResponse: %v", err)
	}
	if resp.NodeType != AdvertTypeRepeater {
		t.Errorf("NodeType = %d, want %d (REPEATER)", resp.NodeType, AdvertTypeRepeater)
	}
	if resp.SNR != -3 {
		t.Errorf("SNR = %g, want -3", resp.SNR)
	}
	if resp.Tag != 0xDEADBEEF {
		t.Errorf("Tag = 0x%08X, want 0xDEADBEEF", resp.Tag)
	}
	if len(resp.PubKey) != 32 {
		t.Fatalf("PubKey len = %d, want 32", len(resp.PubKey))
	}
	for i, b := range resp.PubKey {
		if b != byte(i) {
			t.Errorf("PubKey[%d] = %d, want %d", i, b, i)
			break
		}
	}
}

func TestControlDiscoverResponse_PrefixPubKey(t *testing.T) {
	data := []byte{0x91, 0x00}
	tag := make([]byte, 4)
	binary.LittleEndian.PutUint32(tag, 0x12345678)
	data = append(data, tag...)
	data = append(data, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22)

	ctrl, err := ControlFromBytes(data)
	if err != nil {
		t.Fatalf("ControlFromBytes: %v", err)
	}

	resp, err := ctrl.DiscoverResponse()
	if err != nil {
		t.Fatalf("DiscoverResponse: %v", err)
	}
	if resp.NodeType != AdvertTypeChat {
		t.Errorf("NodeType = %d, want %d (CHAT)", resp.NodeType, AdvertTypeChat)
	}
	if len(resp.PubKey) != 8 {
		t.Errorf("PubKey len = %d, want 8 (prefix)", len(resp.PubKey))
	}
}

func TestControlDiscoverResponse_NoPubKey(t *testing.T) {
	data := []byte{0x93, 0x05}
	tag := make([]byte, 4)
	binary.LittleEndian.PutUint32(tag, 0x00000001)
	data = append(data, tag...)

	ctrl, err := ControlFromBytes(data)
	if err != nil {
		t.Fatalf("ControlFromBytes: %v", err)
	}

	resp, err := ctrl.DiscoverResponse()
	if err != nil {
		t.Fatalf("DiscoverResponse: %v", err)
	}
	if len(resp.PubKey) != 0 {
		t.Errorf("PubKey len = %d, want 0", len(resp.PubKey))
	}
}

func TestControlDiscoverResponse_WrongSubType(t *testing.T) {
	ctrl := &Control{Flags: 0x80, Data: make([]byte, 10)}
	_, err := ctrl.DiscoverResponse()
	if err == nil {
		t.Error("expected error for wrong subtype")
	}
}

func TestControlDiscoverResponse_TooShort(t *testing.T) {
	ctrl := &Control{Flags: 0x90, Data: []byte{0x01}}
	_, err := ctrl.DiscoverResponse()
	if err == nil {
		t.Error("expected error for short data")
	}
}

func TestDiscoverBuilders(t *testing.T) {
	req := DiscoverRequest{TypeFilter: 1 << AdvertTypeRepeater, Tag: 0x04030201}
	b, _ := NewDiscoverRequest(req).ToBytes()
	if got := hex.EncodeToString(b); got != "80"+"04"+"01020304"+"00000000" {
		t.Fatalf("request = %s", got)
	}
	req.PrefixOnly, req.Since = true, 7
	c, _ := ControlFromBytes(mustBytes(NewDiscoverRequest(req).ToBytes()))
	if got, err := c.DiscoverRequest(); err != nil || *got != req {
		t.Fatalf("request round trip = %+v, %v", got, err)
	}

	pub := make([]byte, PubKeySize)
	for i := range pub {
		pub[i] = byte(i)
	}
	resp := DiscoverResponse{NodeType: AdvertTypeRepeater, SNR: -5.25, Tag: 0x04030201, PubKey: pub}
	b, _ = NewDiscoverResponse(resp, false).ToBytes()
	if len(b) != 38 || hex.EncodeToString(b[:6]) != "92"+"eb"+"01020304" {
		t.Fatalf("response = %x", b)
	}
	c, _ = ControlFromBytes(b)
	got, err := c.DiscoverResponse()
	if err != nil || got.NodeType != resp.NodeType || got.SNR != resp.SNR || got.Tag != resp.Tag || !bytes.Equal(got.PubKey, pub) {
		t.Fatalf("response round trip = %+v, %v", got, err)
	}
	b, _ = NewDiscoverResponse(resp, true).ToBytes()
	if len(b) != 14 || !bytes.Equal(b[6:], pub[:8]) {
		t.Fatalf("prefix-only response = %x", b)
	}
}

func mustBytes(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}
