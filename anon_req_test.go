package meshcore

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestAnonReqFromBytes(t *testing.T) {
	tests := []struct {
		name                 string
		hex                  string
		wantErr              bool
		wantDestination      byte
		wantEphemeralPubKey  string
		wantMAC              string
		wantEncryptedPayload string
	}{
		{
			name:                 "basic anon req",
			hex:                  "A1" + "0102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F20" + "C3D4" + "E5F6A7B8C9",
			wantDestination:      0xA1,
			wantEphemeralPubKey:  "0102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F20",
			wantMAC:              "C3D4",
			wantEncryptedPayload: "E5F6A7B8C9",
		},
		{
			name:                 "large encrypted payload",
			hex:                  "AA" + "0102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F20" + "ABCD" + "3A7C376D8CED0FA0C0EB195D505A23D9FADC78379E1DBC44F7BBF8906D7A3302CB266B751B5683F67ECFAB43E0605DAB858896BC74810DDC37E68E436D5E1FB6",
			wantDestination:      0xAA,
			wantEphemeralPubKey:  "0102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F20",
			wantMAC:              "ABCD",
			wantEncryptedPayload: "3A7C376D8CED0FA0C0EB195D505A23D9FADC78379E1DBC44F7BBF8906D7A3302CB266B751B5683F67ECFAB43E0605DAB858896BC74810DDC37E68E436D5E1FB6",
		},
		{
			name:    "empty input returns error",
			hex:     "",
			wantErr: true,
		},
		{
			name:    "too short 34 bytes",
			hex:     "A1" + "0102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F" + "C3D4",
			wantErr: true,
		},
		{
			name:                 "exactly 35 bytes valid",
			hex:                  "A1" + "0102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F20" + "C3D4",
			wantDestination:      0xA1,
			wantEphemeralPubKey:  "0102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F20",
			wantMAC:              "C3D4",
			wantEncryptedPayload: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := hex.DecodeString(tt.hex)
			if err != nil {
				t.Fatalf("bad test hex: %v", err)
			}

			msg, err := AnonReqFromBytes(data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if msg.Destination != tt.wantDestination {
				t.Errorf("Destination = 0x%02x, want 0x%02x", msg.Destination, tt.wantDestination)
			}
			if got := hex.EncodeToString(msg.EphemeralPubKey[:]); !strings.EqualFold(got, tt.wantEphemeralPubKey) {
				t.Errorf("EphemeralPubKey = %s, want %s", got, tt.wantEphemeralPubKey)
			}
			if got := hex.EncodeToString(msg.MAC[:]); !strings.EqualFold(got, tt.wantMAC) {
				t.Errorf("MAC = %s, want %s", got, tt.wantMAC)
			}
			if got := hex.EncodeToString(msg.EncryptedPayload); !strings.EqualFold(got, tt.wantEncryptedPayload) {
				t.Errorf("EncryptedPayload = %s, want %s", got, tt.wantEncryptedPayload)
			}
		})
	}
}

func TestAnonReqToBytes(t *testing.T) {
	tests := []struct {
		name    string
		msg     AnonReq
		wantHex string
	}{
		{
			name: "minimal payload",
			msg: AnonReq{
				Destination:      0xA1,
				EphemeralPubKey:  [32]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, 0x20},
				MAC:              [2]byte{0xC3, 0xD4},
				EncryptedPayload: []byte{0xE5, 0xF6, 0xA7, 0xB8, 0xC9},
			},
			wantHex: "A10102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F20C3D4E5F6A7B8C9",
		},
		{
			name: "single block payload",
			msg: AnonReq{
				Destination: 0x00,
				EphemeralPubKey: [32]byte{
					0xFF, 0xFE, 0xFD, 0xFC, 0xFB, 0xFA, 0xF9, 0xF8,
					0xF7, 0xF6, 0xF5, 0xF4, 0xF3, 0xF2, 0xF1, 0xF0,
					0xE0, 0xE1, 0xE2, 0xE3, 0xE4, 0xE5, 0xE6, 0xE7,
					0xE8, 0xE9, 0xEA, 0xEB, 0xEC, 0xED, 0xEE, 0xEF,
				},
				MAC: [2]byte{0x12, 0x34},
				EncryptedPayload: []byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
				},
			},
			wantHex: "00FFFEFDFC FBFAF9F8F7F6F5F4F3F2F1F0E0E1E2E3E4E5E6E7E8E9EAEBECEDEEEF1234010203040506070809 0A0B0C0D0E0F10",
		},
		{
			name: "empty encrypted payload",
			msg: AnonReq{
				Destination:      0xDE,
				EphemeralPubKey:  [32]byte{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB, 0xBB},
				MAC:              [2]byte{0xBE, 0xEF},
				EncryptedPayload: []byte{},
			},
			wantHex: "DEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBEEF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.msg.ToBytes()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			gotHex := hex.EncodeToString(got)
			wantHex := strings.ReplaceAll(tt.wantHex, " ", "")
			if !strings.EqualFold(gotHex, wantHex) {
				t.Errorf("ToBytes() = %s, want %s", gotHex, wantHex)
			}
		})
	}
}

func TestAnonReqDecrypt(t *testing.T) {
	t.Run("wrong shared secret returns nil", func(t *testing.T) {
		_, aliceSeed, _ := generateTestKeyPair(t)
		_, bobSeed, _ := generateTestKeyPair(t)
		_, _, ephemeralPub := generateTestKeyPair(t)

		sharedAlice, err := DeriveSharedSecret(aliceSeed, ephemeralPub)
		if err != nil {
			t.Fatalf("deriving alice-ephemeral secret: %v", err)
		}

		plaintext := []byte("hello recipient")
		encrypted, err := EncryptThenMAC(sharedAlice, plaintext)
		if err != nil {
			t.Fatalf("encrypting: %v", err)
		}

		msg := &AnonReq{
			Destination:      0xAA,
			EphemeralPubKey:  [32]byte{},
			MAC:              [2]byte{encrypted[0], encrypted[1]},
			EncryptedPayload: encrypted[2:],
		}
		copy(msg.EphemeralPubKey[:], ephemeralPub)

		wrongSecret, err := DeriveSharedSecret(bobSeed, ephemeralPub)
		if err != nil {
			t.Fatalf("deriving wrong secret: %v", err)
		}

		got := msg.Decrypt(wrongSecret)
		if got != nil {
			t.Errorf("Decrypt() with wrong key = %x, want nil", got)
		}
	})

	t.Run("correct shared secret recovers plaintext", func(t *testing.T) {
		_, recipientSeed, recipientPub := generateTestKeyPair(t)
		_, ephemeralSeed, ephemeralPub := generateTestKeyPair(t)

		sharedEphemeral, err := DeriveSharedSecret(ephemeralSeed, recipientPub)
		if err != nil {
			t.Fatalf("deriving ephemeral secret: %v", err)
		}
		sharedRecipient, err := DeriveSharedSecret(recipientSeed, ephemeralPub)
		if err != nil {
			t.Fatalf("deriving recipient secret: %v", err)
		}

		plaintext := []byte("hello from ephemeral")
		encrypted, err := EncryptThenMAC(sharedEphemeral, plaintext)
		if err != nil {
			t.Fatalf("encrypting: %v", err)
		}

		msg := &AnonReq{
			Destination:      0xAA,
			EphemeralPubKey:  [32]byte{},
			MAC:              [2]byte{encrypted[0], encrypted[1]},
			EncryptedPayload: encrypted[2:],
		}
		copy(msg.EphemeralPubKey[:], ephemeralPub)

		got := msg.Decrypt(sharedRecipient)
		if got == nil {
			t.Fatal("Decrypt() returned nil, expected plaintext")
		}
		gotTrimmed := strings.TrimRight(string(got), "\x00")
		if gotTrimmed != string(plaintext) {
			t.Errorf("Decrypt() = %q, want %q", gotTrimmed, plaintext)
		}
	})
}

func TestAnonReqVerifyMAC(t *testing.T) {
	_, ephemeralSeed, ephemeralPub := generateTestKeyPair(t)
	_, recipientSeed, recipientPub := generateTestKeyPair(t)
	_, _, carolPub := generateTestKeyPair(t)

	sharedEphemeral, err := DeriveSharedSecret(ephemeralSeed, recipientPub)
	if err != nil {
		t.Fatalf("deriving ephemeral secret: %v", err)
	}

	plaintext := []byte("verify me")
	encrypted, err := EncryptThenMAC(sharedEphemeral, plaintext)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}

	msg := &AnonReq{
		Destination:      0xAA,
		EphemeralPubKey:  [32]byte{},
		MAC:              [2]byte{encrypted[0], encrypted[1]},
		EncryptedPayload: encrypted[2:],
	}
	copy(msg.EphemeralPubKey[:], ephemeralPub)

	t.Run("valid shared secret", func(t *testing.T) {
		sharedRecipient, err := DeriveSharedSecret(recipientSeed, ephemeralPub)
		if err != nil {
			t.Fatalf("deriving recipient secret: %v", err)
		}
		if !msg.VerifyMAC(sharedRecipient) {
			t.Error("VerifyMAC() = false, want true")
		}
	})

	t.Run("wrong shared secret", func(t *testing.T) {
		wrongSecret, err := DeriveSharedSecret(recipientSeed, carolPub)
		if err != nil {
			t.Fatalf("deriving wrong secret: %v", err)
		}
		if msg.VerifyMAC(wrongSecret) {
			t.Error("VerifyMAC() = true, want false")
		}
	})
}

func TestAnonReqRoundTrip(t *testing.T) {
	_, ephemeralSeed, ephemeralPub := generateTestKeyPair(t)
	_, recipientSeed, recipientPub := generateTestKeyPair(t)

	sharedEphemeral, err := DeriveSharedSecret(ephemeralSeed, recipientPub)
	if err != nil {
		t.Fatalf("deriving ephemeral secret: %v", err)
	}
	sharedRecipient, err := DeriveSharedSecret(recipientSeed, ephemeralPub)
	if err != nil {
		t.Fatalf("deriving recipient secret: %v", err)
	}

	plaintext := []byte("full round trip test")
	encrypted, err := EncryptThenMAC(sharedEphemeral, plaintext)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}

	msg := &AnonReq{
		Destination:      0xAA,
		EphemeralPubKey:  [32]byte{},
		MAC:              [2]byte{encrypted[0], encrypted[1]},
		EncryptedPayload: encrypted[2:],
	}
	copy(msg.EphemeralPubKey[:], ephemeralPub)

	wire, err := msg.ToBytes()
	if err != nil {
		t.Fatalf("ToBytes(): %v", err)
	}

	decoded, err := AnonReqFromBytes(wire)
	if err != nil {
		t.Fatalf("AnonReqFromBytes(): %v", err)
	}

	if decoded.Destination != msg.Destination {
		t.Errorf("Destination = 0x%02x, want 0x%02x", decoded.Destination, msg.Destination)
	}
	if decoded.EphemeralPubKey != msg.EphemeralPubKey {
		t.Errorf("EphemeralPubKey = %x, want %x", decoded.EphemeralPubKey, msg.EphemeralPubKey)
	}

	if !decoded.VerifyMAC(sharedRecipient) {
		t.Fatal("VerifyMAC() failed after round trip")
	}

	got := decoded.Decrypt(sharedRecipient)
	if got == nil {
		t.Fatal("Decrypt() returned nil after round trip")
	}
	gotTrimmed := strings.TrimRight(string(got), "\x00")
	if gotTrimmed != string(plaintext) {
		t.Errorf("Decrypt() = %q, want %q", gotTrimmed, plaintext)
	}
}

func TestAnonReqFromBytes_ShortIsErrTooShort(t *testing.T) {
	if _, err := AnonReqFromBytes(make([]byte, 34)); !errors.Is(err, ErrTooShort) {
		t.Fatalf("error = %v, want ErrTooShort", err)
	}
}

func TestBuildAnonRegionsRequest(t *testing.T) {
	got, err := BuildAnonRegionsRequest([]byte{0xA1, 0xA2, 0xB1, 0xB2}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{AnonReqTypeRegions, 0x42, 0xA1, 0xA2, 0xB1, 0xB2}; !bytes.Equal(got, want) {
		t.Fatalf("request = %x, want %x", got, want)
	}

	if got, err := BuildAnonRegionsRequest(nil, 1); err != nil || !bytes.Equal(got, []byte{AnonReqTypeRegions, 0x00}) {
		t.Fatalf("zero-hop request = %x, %v", got, err)
	}

	for name, c := range map[string]struct {
		path []byte
		size uint8
	}{
		"partial hop":    {[]byte{1, 2, 3}, 2},
		"hash size 0":    {nil, 0},
		"hash size 4":    {make([]byte, 4), 4},
		"too many bytes": {make([]byte, 66), 3},
		"too many hops":  {make([]byte, 64+1), 1},
	} {
		if _, err := BuildAnonRegionsRequest(c.path, c.size); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestParseAnonRegionsReply(t *testing.T) {
	data := append([]byte{0x10, 0x20, 0x30, 0x40}, "*,nz,au"...)
	data = append(data, 0, 0, 0)
	got, err := ParseAnonRegionsReply(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Clock != 0x40302010 || strings.Join(got.Regions, "|") != "*|nz|au" {
		t.Fatalf("reply = %+v", got)
	}

	if got, err := ParseAnonRegionsReply([]byte{1, 0, 0, 0, 0, 0}); err != nil || got.Regions != nil {
		t.Fatalf("empty list = %+v, %v; want no regions", got, err)
	}
	if _, err := ParseAnonRegionsReply([]byte{1, 2, 3}); !errors.Is(err, ErrTooShort) {
		t.Fatalf("short reply err = %v, want ErrTooShort", err)
	}
}

func TestNewAnonReq(t *testing.T) {
	sender, server, shared := testPeers(t)
	plain := []byte("\x01\x02\x03\x04password")
	a, err := NewAnonReq(sender, server.Identity, plain, shared)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := a.ToBytes()
	if wire[0] != server.PublicKey()[0] || !bytes.Equal(wire[1:33], sender.PublicKeyBytes()) {
		t.Fatalf("header = %x, want dest hash then sender pubkey", wire[:33])
	}
	back, err := AnonReqFromBytes(wire)
	if err != nil {
		t.Fatal(err)
	}
	serverShared, _ := server.SharedSecret(NewIdentity(back.EphemeralPubKey))
	if got := back.Decrypt(serverShared); !bytes.HasPrefix(got, plain) {
		t.Fatalf("Decrypt = %x, want prefix %x", got, plain)
	}
}
