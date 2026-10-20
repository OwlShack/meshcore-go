package meshcore

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestMultiPartFromBytes(t *testing.T) {
	tests := []struct {
		name               string
		hex                string
		wantErr            bool
		wantRemaining      uint8
		wantWrappedType    byte
		wantWrappedPayload string
	}{
		{
			name:               "valid ACK wrapper",
			hex:                "23DEADBEEFCAFEBABE",
			wantRemaining:      2,
			wantWrappedType:    0x03,
			wantWrappedPayload: "DEADBEEFCAFEBABE",
		},
		{
			name:    "no wrapped payload",
			hex:     "15",
			wantErr: true,
		},
		{
			name:    "two bytes",
			hex:     "1500",
			wantErr: true,
		},
		{
			name:    "ACK shorter than its CRC",
			hex:     "23DEADBE",
			wantErr: true,
		},
		{
			name:               "remaining at max, type 0",
			hex:                "F0AABBCCDD",
			wantRemaining:      15,
			wantWrappedType:    0x00,
			wantWrappedPayload: "AABBCCDD",
		},
		{
			name:               "remaining 0, type F",
			hex:                "0FBEEFDEADBABEFACE",
			wantRemaining:      0,
			wantWrappedType:    0x0F,
			wantWrappedPayload: "BEEFDEADBABEFACE",
		},
		{
			name:    "empty input",
			hex:     "",
			wantErr: true,
		},
		{
			name:    "single header byte only",
			hex:     "42",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := hex.DecodeString(tt.hex)
			if err != nil {
				t.Fatalf("bad test hex: %v", err)
			}

			mp, err := MultiPartFromBytes(data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if mp.Remaining != tt.wantRemaining {
				t.Errorf("Remaining = %d, want %d", mp.Remaining, tt.wantRemaining)
			}
			if mp.WrappedType != tt.wantWrappedType {
				t.Errorf("WrappedType = 0x%02x, want 0x%02x", mp.WrappedType, tt.wantWrappedType)
			}
			if tt.wantWrappedPayload != "" {
				if got := hex.EncodeToString(mp.WrappedPayload); !strings.EqualFold(got, tt.wantWrappedPayload) {
					t.Errorf("WrappedPayload = %s, want %s", got, tt.wantWrappedPayload)
				}
			}
		})
	}
}

func TestMultiPartToBytes(t *testing.T) {
	tests := []struct {
		name    string
		mp      MultiPart
		wantHex string
	}{
		{
			name: "ACK wrapper",
			mp: MultiPart{
				Remaining:      2,
				WrappedType:    0x03,
				WrappedPayload: []byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE, 0xBA, 0xBE},
			},
			wantHex: "23DEADBEEFCAFEBABE",
		},
		{
			name: "empty wrapped payload",
			mp: MultiPart{
				Remaining:      1,
				WrappedType:    0x05,
				WrappedPayload: []byte{},
			},
			wantHex: "15",
		},
		{
			name: "remaining at max, type 0",
			mp: MultiPart{
				Remaining:      15,
				WrappedType:    0x00,
				WrappedPayload: []byte{0xAA, 0xBB, 0xCC, 0xDD},
			},
			wantHex: "F0AABBCCDD",
		},
		{
			name: "remaining 0, type F",
			mp: MultiPart{
				Remaining:      0,
				WrappedType:    0x0F,
				WrappedPayload: []byte{0xBE, 0xEF, 0xDE, 0xAD, 0xBA, 0xBE, 0xFA, 0xCE},
			},
			wantHex: "0FBEEFDEADBABEFACE",
		},
		{
			name: "wrapped type masking",
			mp: MultiPart{
				Remaining:      3,
				WrappedType:    0xFF,
				WrappedPayload: []byte{0x11, 0x22},
			},
			wantHex: "3F1122",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.mp.ToBytes()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotHex := hex.EncodeToString(got); !strings.EqualFold(gotHex, tt.wantHex) {
				t.Errorf("ToBytes() = %s, want %s", gotHex, tt.wantHex)
			}
		})
	}
}

func TestMultiPartToBytesRejectsRemainingOver15(t *testing.T) {
	mp := MultiPart{Remaining: 16, WrappedType: PayloadTypeAck, WrappedPayload: []byte{1, 2, 3, 4}}
	if _, err := mp.ToBytes(); err == nil {
		t.Fatal("ToBytes accepted Remaining 16, which wraps to 0 in the 4-bit field")
	}
}

func TestMultiPartRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		mp   MultiPart
	}{
		{
			name: "ACK wrapper",
			mp: MultiPart{
				Remaining:      2,
				WrappedType:    0x03,
				WrappedPayload: []byte{0xDE, 0xAD, 0xBE, 0xEF},
			},
		},
		{
			name: "short payload",
			mp: MultiPart{
				Remaining:      5,
				WrappedType:    0x0A,
				WrappedPayload: []byte{0x01, 0x02},
			},
		},
		{
			name: "large payload",
			mp: MultiPart{
				Remaining:      7,
				WrappedType:    0x02,
				WrappedPayload: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire, err := tt.mp.ToBytes()
			if err != nil {
				t.Fatalf("ToBytes(): %v", err)
			}

			decoded, err := MultiPartFromBytes(wire)
			if err != nil {
				t.Fatalf("MultiPartFromBytes(): %v", err)
			}

			if decoded.Remaining != tt.mp.Remaining {
				t.Errorf("Remaining = %d, want %d", decoded.Remaining, tt.mp.Remaining)
			}
			if decoded.WrappedType != tt.mp.WrappedType {
				t.Errorf("WrappedType = 0x%02x, want 0x%02x", decoded.WrappedType, tt.mp.WrappedType)
			}
			if got := hex.EncodeToString(decoded.WrappedPayload); got != hex.EncodeToString(tt.mp.WrappedPayload) {
				t.Errorf("WrappedPayload = %s, want %s", got, hex.EncodeToString(tt.mp.WrappedPayload))
			}
		})
	}
}
