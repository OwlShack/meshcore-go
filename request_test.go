package meshcore

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestRequestFromBytes(t *testing.T) {
	tests := []struct {
		name                 string
		hex                  string
		wantErr              bool
		wantDestination      byte
		wantSource           byte
		wantMAC              string
		wantEncryptedPayload string
	}{
		{
			name:                 "basic request",
			hex:                  "A1B2C3D4E5F6A7B8C9",
			wantDestination:      0xA1,
			wantSource:           0xB2,
			wantMAC:              "C3D4",
			wantEncryptedPayload: "E5F6A7B8C9",
		},
		{
			name:                 "large encrypted payload",
			hex:                  "A3D0E8883A7C376D8CED0FA0C0EB195D505A23D9FADC78379E1DBC44F7BBF8906D7A3302CB266B751B5683F67ECFAB43E0605DAB858896BC74810DDC37E68E436D5E1FB6",
			wantDestination:      0xA3,
			wantSource:           0xD0,
			wantMAC:              "E888",
			wantEncryptedPayload: "3A7C376D8CED0FA0C0EB195D505A23D9FADC78379E1DBC44F7BBF8906D7A3302CB266B751B5683F67ECFAB43E0605DAB858896BC74810DDC37E68E436D5E1FB6",
		},
		{
			name:    "empty input returns error",
			hex:     "",
			wantErr: true,
		},
		{
			name:    "1 byte only missing source",
			hex:     "A1",
			wantErr: true,
		},
		{
			name:    "2 bytes missing MAC",
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

			req, err := RequestFromBytes(data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if req.Destination != tt.wantDestination {
				t.Errorf("Destination = 0x%02x, want 0x%02x", req.Destination, tt.wantDestination)
			}
			if req.Source != tt.wantSource {
				t.Errorf("Source = 0x%02x, want 0x%02x", req.Source, tt.wantSource)
			}
			if tt.wantMAC != "" {
				if got := hex.EncodeToString(req.MAC[:]); !strings.EqualFold(got, tt.wantMAC) {
					t.Errorf("MAC = %s, want %s", got, tt.wantMAC)
				}
			}
			if tt.wantEncryptedPayload != "" {
				if got := hex.EncodeToString(req.EncryptedPayload); !strings.EqualFold(got, tt.wantEncryptedPayload) {
					t.Errorf("EncryptedPayload = %s, want %s", got, tt.wantEncryptedPayload)
				}
			}
		})
	}
}

func TestRequestToBytes(t *testing.T) {
	tests := []struct {
		name    string
		req     Request
		wantHex string
	}{
		{
			name: "minimal payload",
			req: Request{
				Destination:      0xA1,
				Source:           0xB2,
				MAC:              [2]byte{0xC3, 0xD4},
				EncryptedPayload: []byte{0xE5, 0xF6, 0xA7, 0xB8, 0xC9},
			},
			wantHex: "A1B2C3D4E5F6A7B8C9",
		},
		{
			name: "single block payload",
			req: Request{
				Destination: 0x00,
				Source:      0xFF,
				MAC:         [2]byte{0x12, 0x34},
				EncryptedPayload: []byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
				},
			},
			wantHex: "00FF12340102030405060708090A0B0C0D0E0F10",
		},
		{
			name: "empty payload",
			req: Request{
				Destination:      0xDE,
				Source:           0xAD,
				MAC:              [2]byte{0xBE, 0xEF},
				EncryptedPayload: []byte{},
			},
			wantHex: "DEADBEEF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.req.ToBytes()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotHex := hex.EncodeToString(got); !strings.EqualFold(gotHex, tt.wantHex) {
				t.Errorf("ToBytes() = %s, want %s", gotHex, tt.wantHex)
			}
		})
	}
}

func TestRequestDecrypt(t *testing.T) {
	t.Run("wrong shared secret returns nil", func(t *testing.T) {
		_, aliceSeed, _ := generateTestKeyPair(t)
		_, bobSeed, _ := generateTestKeyPair(t)
		_, _, carolPub := generateTestKeyPair(t)

		sharedAliceBob, err := DeriveSharedSecret(aliceSeed, carolPub)
		if err != nil {
			t.Fatalf("deriving alice-carol secret: %v", err)
		}

		plaintext := []byte("hello request")
		encrypted, err := EncryptThenMAC(sharedAliceBob, plaintext)
		if err != nil {
			t.Fatalf("encrypting: %v", err)
		}

		req := &Request{
			Destination:      0xAA,
			Source:           0xBB,
			MAC:              [2]byte{encrypted[0], encrypted[1]},
			EncryptedPayload: encrypted[2:],
		}

		wrongSecret, err := DeriveSharedSecret(bobSeed, carolPub)
		if err != nil {
			t.Fatalf("deriving wrong secret: %v", err)
		}

		got := req.Decrypt(wrongSecret)
		if got != nil {
			t.Errorf("Decrypt() with wrong key = %x, want nil", got)
		}
	})

	t.Run("correct shared secret recovers plaintext", func(t *testing.T) {
		_, aliceSeed, alicePub := generateTestKeyPair(t)
		_, bobSeed, bobPub := generateTestKeyPair(t)

		sharedAlice, err := DeriveSharedSecret(aliceSeed, bobPub)
		if err != nil {
			t.Fatalf("deriving alice secret: %v", err)
		}
		sharedBob, err := DeriveSharedSecret(bobSeed, alicePub)
		if err != nil {
			t.Fatalf("deriving bob secret: %v", err)
		}

		plaintext := []byte("hello from request")
		encrypted, err := EncryptThenMAC(sharedAlice, plaintext)
		if err != nil {
			t.Fatalf("encrypting: %v", err)
		}

		req := &Request{
			Destination:      0xAA,
			Source:           0xBB,
			MAC:              [2]byte{encrypted[0], encrypted[1]},
			EncryptedPayload: encrypted[2:],
		}

		got := req.Decrypt(sharedBob)
		if got == nil {
			t.Fatal("Decrypt() returned nil, expected plaintext")
		}
		gotTrimmed := strings.TrimRight(string(got), "\x00")
		if gotTrimmed != string(plaintext) {
			t.Errorf("Decrypt() = %q, want %q", gotTrimmed, plaintext)
		}
	})
}

func TestRequestVerifyMAC(t *testing.T) {
	_, aliceSeed, alicePub := generateTestKeyPair(t)
	_, bobSeed, bobPub := generateTestKeyPair(t)
	_, _, carolPub := generateTestKeyPair(t)

	sharedAlice, err := DeriveSharedSecret(aliceSeed, bobPub)
	if err != nil {
		t.Fatalf("deriving alice secret: %v", err)
	}

	plaintext := []byte("verify request")
	encrypted, err := EncryptThenMAC(sharedAlice, plaintext)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}

	req := &Request{
		Destination:      0xAA,
		Source:           0xBB,
		MAC:              [2]byte{encrypted[0], encrypted[1]},
		EncryptedPayload: encrypted[2:],
	}

	t.Run("valid shared secret", func(t *testing.T) {
		sharedBob, err := DeriveSharedSecret(bobSeed, alicePub)
		if err != nil {
			t.Fatalf("deriving bob secret: %v", err)
		}
		if !req.VerifyMAC(sharedBob) {
			t.Error("VerifyMAC() = false, want true")
		}
	})

	t.Run("wrong shared secret", func(t *testing.T) {
		wrongSecret, err := DeriveSharedSecret(bobSeed, carolPub)
		if err != nil {
			t.Fatalf("deriving wrong secret: %v", err)
		}
		if req.VerifyMAC(wrongSecret) {
			t.Error("VerifyMAC() = true, want false")
		}
	})
}

func TestRequestRoundTrip(t *testing.T) {
	_, aliceSeed, alicePub := generateTestKeyPair(t)
	_, bobSeed, bobPub := generateTestKeyPair(t)

	sharedAlice, err := DeriveSharedSecret(aliceSeed, bobPub)
	if err != nil {
		t.Fatalf("deriving alice secret: %v", err)
	}
	sharedBob, err := DeriveSharedSecret(bobSeed, alicePub)
	if err != nil {
		t.Fatalf("deriving bob secret: %v", err)
	}

	plaintext := []byte("full round trip request message")
	encrypted, err := EncryptThenMAC(sharedAlice, plaintext)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}

	req := &Request{
		Destination:      0xAA,
		Source:           0xBB,
		MAC:              [2]byte{encrypted[0], encrypted[1]},
		EncryptedPayload: encrypted[2:],
	}

	wire, err := req.ToBytes()
	if err != nil {
		t.Fatalf("ToBytes(): %v", err)
	}

	decoded, err := RequestFromBytes(wire)
	if err != nil {
		t.Fatalf("RequestFromBytes(): %v", err)
	}

	if decoded.Destination != req.Destination {
		t.Errorf("Destination = 0x%02x, want 0x%02x", decoded.Destination, req.Destination)
	}
	if decoded.Source != req.Source {
		t.Errorf("Source = 0x%02x, want 0x%02x", decoded.Source, req.Source)
	}

	if !decoded.VerifyMAC(sharedBob) {
		t.Fatal("VerifyMAC() failed after round trip")
	}

	got := decoded.Decrypt(sharedBob)
	if got == nil {
		t.Fatal("Decrypt() returned nil after round trip")
	}
	gotTrimmed := strings.TrimRight(string(got), "\x00")
	if gotTrimmed != string(plaintext) {
		t.Errorf("Decrypt() = %q, want %q", gotTrimmed, plaintext)
	}
}

func testPeers(t *testing.T) (alice, bob LocalIdentity, shared []byte) {
	t.Helper()
	alice, bob = NewLocalIdentityFromSeed([32]byte{1}), NewLocalIdentityFromSeed([32]byte{2})
	shared, err := alice.SharedSecret(bob.Identity)
	if err != nil {
		t.Fatal(err)
	}
	return alice, bob, shared
}

func TestNewRequest(t *testing.T) {
	alice, bob, shared := testPeers(t)
	plain := []byte{0x10, 0x20, 0x30, 0x40, 0x01}
	req, err := NewRequest(alice, bob.Identity, plain, shared)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := req.ToBytes()
	back, err := RequestFromBytes(wire)
	if err != nil || back.Destination != bob.PublicKey()[0] || back.Source != alice.PublicKey()[0] {
		t.Fatalf("RequestFromBytes = %+v, err %v", back, err)
	}
	bobShared, _ := bob.SharedSecret(alice.Identity)
	if got := back.Decrypt(bobShared); len(got) != 16 || !strings.HasPrefix(string(got), string(plain)) {
		t.Fatalf("Decrypt = %x, want %x zero-padded to 16", got, plain)
	}
}
