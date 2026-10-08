package meshcore

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestGroupDataFromBytes(t *testing.T) {
	tests := []struct {
		name            string
		hex             string
		wantErr         bool
		wantChannelHash byte
		wantMAC         string
		wantPayload     string
	}{
		{
			name:            "basic group data",
			hex:             "A1B2C3D4E5F6A7B8C9",
			wantChannelHash: 0xA1,
			wantMAC:         "B2C3",
			wantPayload:     "D4E5F6A7B8C9",
		},
		{
			name:            "large encrypted payload",
			hex:             "A0E8883A7C376D8CED0FA0C0EB195D505A23D9FADC78379E1DBC44F7BBF8906D7A3302CB266B751B5683F67ECFAB43E0605DAB858896BC74810DDC37E68E436D5E1FB6",
			wantChannelHash: 0xA0,
			wantMAC:         "E888",
			wantPayload:     "3A7C376D8CED0FA0C0EB195D505A23D9FADC78379E1DBC44F7BBF8906D7A3302CB266B751B5683F67ECFAB43E0605DAB858896BC74810DDC37E68E436D5E1FB6",
		},
		{
			name:    "empty input returns error",
			hex:     "",
			wantErr: true,
		},
		{
			name:    "1 byte only missing MAC",
			hex:     "A1",
			wantErr: true,
		},
		{
			name:    "2 bytes missing payload portion of MAC",
			hex:     "A1B2",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := hex.DecodeString(tt.hex)
			if err != nil {
				t.Fatalf("bad test hex: %v", err)
			}

			gd, err := GroupDataFromBytes(data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if gd.ChannelHash != tt.wantChannelHash {
				t.Errorf("ChannelHash = 0x%02x, want 0x%02x", gd.ChannelHash, tt.wantChannelHash)
			}
			if tt.wantMAC != "" {
				if got := hex.EncodeToString(gd.MAC[:]); !strings.EqualFold(got, tt.wantMAC) {
					t.Errorf("MAC = %s, want %s", got, tt.wantMAC)
				}
			}
			if tt.wantPayload != "" {
				if got := hex.EncodeToString(gd.EncryptedPayload); !strings.EqualFold(got, tt.wantPayload) {
					t.Errorf("EncryptedPayload = %s, want %s", got, tt.wantPayload)
				}
			}
		})
	}
}

func TestGroupDataToBytes(t *testing.T) {
	tests := []struct {
		name    string
		gd      GroupData
		wantHex string
	}{
		{
			name: "minimal payload",
			gd: GroupData{
				ChannelHash:      0xA1,
				MAC:              [2]byte{0xB2, 0xC3},
				EncryptedPayload: []byte{0xD4, 0xE5, 0xF6, 0xA7, 0xB8, 0xC9},
			},
			wantHex: "A1B2C3D4E5F6A7B8C9",
		},
		{
			name: "single block payload",
			gd: GroupData{
				ChannelHash: 0x00,
				MAC:         [2]byte{0x12, 0x34},
				EncryptedPayload: []byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
				},
			},
			wantHex: "0012340102030405060708090A0B0C0D0E0F10",
		},
		{
			name: "empty payload",
			gd: GroupData{
				ChannelHash:      0xDE,
				MAC:              [2]byte{0xAD, 0xBE},
				EncryptedPayload: []byte{},
			},
			wantHex: "DEADBE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.gd.ToBytes()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotHex := hex.EncodeToString(got); !strings.EqualFold(gotHex, tt.wantHex) {
				t.Errorf("ToBytes() = %s, want %s", gotHex, tt.wantHex)
			}
		})
	}
}

func TestGroupDataDecrypt(t *testing.T) {
	channelKey := make([]byte, 32)
	for i := range channelKey {
		channelKey[i] = byte(i)
	}

	t.Run("wrong channel key returns nil", func(t *testing.T) {
		plaintext := []byte("hello group")
		encrypted, err := EncryptThenMAC(channelKey, plaintext)
		if err != nil {
			t.Fatalf("encrypting: %v", err)
		}

		gd := &GroupData{
			ChannelHash:      0xAA,
			MAC:              [2]byte{encrypted[0], encrypted[1]},
			EncryptedPayload: encrypted[2:],
		}

		wrongKey := make([]byte, 32)
		for i := range wrongKey {
			wrongKey[i] = byte(i + 1)
		}

		got := gd.Decrypt(wrongKey)
		if got != nil {
			t.Errorf("Decrypt() with wrong key = %x, want nil", got)
		}
	})

	t.Run("correct channel key recovers plaintext", func(t *testing.T) {
		plaintext := []byte("hello from channel")
		encrypted, err := EncryptThenMAC(channelKey, plaintext)
		if err != nil {
			t.Fatalf("encrypting: %v", err)
		}

		gd := &GroupData{
			ChannelHash:      0xAA,
			MAC:              [2]byte{encrypted[0], encrypted[1]},
			EncryptedPayload: encrypted[2:],
		}

		got := gd.Decrypt(channelKey)
		if got == nil {
			t.Fatal("Decrypt() returned nil, expected plaintext")
		}
		gotTrimmed := strings.TrimRight(string(got), "\x00")
		if gotTrimmed != string(plaintext) {
			t.Errorf("Decrypt() = %q, want %q", gotTrimmed, plaintext)
		}
	})
}

func TestGroupDataVerifyMAC(t *testing.T) {
	channelKey := make([]byte, 32)
	for i := range channelKey {
		channelKey[i] = byte(i)
	}

	plaintext := []byte("verify me group")
	encrypted, err := EncryptThenMAC(channelKey, plaintext)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}

	gd := &GroupData{
		ChannelHash:      0xAA,
		MAC:              [2]byte{encrypted[0], encrypted[1]},
		EncryptedPayload: encrypted[2:],
	}

	t.Run("valid channel key", func(t *testing.T) {
		if !gd.VerifyMAC(channelKey) {
			t.Error("VerifyMAC() = false, want true")
		}
	})

	t.Run("wrong channel key", func(t *testing.T) {
		wrongKey := make([]byte, 32)
		for i := range wrongKey {
			wrongKey[i] = byte(i + 1)
		}
		if gd.VerifyMAC(wrongKey) {
			t.Error("VerifyMAC() = true, want false")
		}
	})
}

func TestGroupDataRoundTrip(t *testing.T) {
	channelKey := make([]byte, 32)
	for i := range channelKey {
		channelKey[i] = byte(i)
	}

	plaintext := []byte("full round trip test group message")
	encrypted, err := EncryptThenMAC(channelKey, plaintext)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}

	gd := &GroupData{
		ChannelHash:      0xAA,
		MAC:              [2]byte{encrypted[0], encrypted[1]},
		EncryptedPayload: encrypted[2:],
	}

	wire, err := gd.ToBytes()
	if err != nil {
		t.Fatalf("ToBytes(): %v", err)
	}

	decoded, err := GroupDataFromBytes(wire)
	if err != nil {
		t.Fatalf("GroupDataFromBytes(): %v", err)
	}

	if decoded.ChannelHash != gd.ChannelHash {
		t.Errorf("ChannelHash = 0x%02x, want 0x%02x", decoded.ChannelHash, gd.ChannelHash)
	}
	if decoded.MAC != gd.MAC {
		t.Errorf("MAC = %x, want %x", decoded.MAC, gd.MAC)
	}
	if got := hex.EncodeToString(decoded.EncryptedPayload); !strings.EqualFold(got, hex.EncodeToString(gd.EncryptedPayload)) {
		t.Errorf("EncryptedPayload mismatch")
	}

	if !decoded.VerifyMAC(channelKey) {
		t.Error("VerifyMAC() = false, want true")
	}

	decrypted := decoded.Decrypt(channelKey)
	if decrypted == nil {
		t.Fatal("Decrypt() returned nil")
	}
	decryptedTrimmed := strings.TrimRight(string(decrypted), "\x00")
	if decryptedTrimmed != string(plaintext) {
		t.Errorf("Decrypt() = %q, want %q", decryptedTrimmed, plaintext)
	}
}

func TestGroupDataPayloadRoundTrip(t *testing.T) {
	ch := NewChannelFromHashtag("data")
	p := GroupDataPayload{DataType: 0x1234, Data: []byte("hi")}
	plain, err := BuildGroupDataPayload(p)
	if err != nil || !bytes.Equal(plain, []byte{0x34, 0x12, 0x02, 'h', 'i'}) {
		t.Fatalf("BuildGroupDataPayload = %x, err %v", plain, err)
	}
	gd, err := NewGroupData(ch.Hash, ch.PSK, plain)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := gd.ToBytes()
	back, err := GroupDataFromBytes(wire)
	if err != nil || back.ChannelHash != ch.Hash {
		t.Fatalf("GroupDataFromBytes = %+v, err %v", back, err)
	}
	got, err := back.DecryptStruct(ch.PSK)
	if err != nil || got.DataType != p.DataType || !bytes.Equal(got.Data, p.Data) {
		t.Fatalf("DecryptStruct = %+v, err %v", got, err)
	}
}

func TestGroupDataPayloadLimits(t *testing.T) {
	if _, err := BuildGroupDataPayload(GroupDataPayload{DataType: GroupDataTypeDev, Data: make([]byte, MaxGroupDataLength)}); err != nil {
		t.Fatalf("max length rejected: %v", err)
	}
	if _, err := BuildGroupDataPayload(GroupDataPayload{Data: make([]byte, MaxGroupDataLength+1)}); err == nil || !strings.HasPrefix(err.Error(), "meshcore: ") {
		t.Fatalf("over-long data err = %v", err)
	}
	if _, err := ParseGroupDataPayload([]byte{0xFF, 0xFF, 3, 'a', 'b'}); !errors.Is(err, ErrTooShort) {
		t.Fatalf("length past the payload err = %v", err)
	}
	if _, err := ParseGroupDataPayload([]byte{0, 0}); err == nil {
		t.Fatal("2-byte payload parsed")
	}
}
