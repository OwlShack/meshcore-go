package meshcore

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestPathFromBytes(t *testing.T) {
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
			name:                 "basic path",
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

			path, err := PathFromBytes(data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if path.Destination != tt.wantDestination {
				t.Errorf("Destination = 0x%02x, want 0x%02x", path.Destination, tt.wantDestination)
			}
			if path.Source != tt.wantSource {
				t.Errorf("Source = 0x%02x, want 0x%02x", path.Source, tt.wantSource)
			}
			if tt.wantMAC != "" {
				if got := hex.EncodeToString(path.MAC[:]); !strings.EqualFold(got, tt.wantMAC) {
					t.Errorf("MAC = %s, want %s", got, tt.wantMAC)
				}
			}
			if tt.wantEncryptedPayload != "" {
				if got := hex.EncodeToString(path.EncryptedPayload); !strings.EqualFold(got, tt.wantEncryptedPayload) {
					t.Errorf("EncryptedPayload = %s, want %s", got, tt.wantEncryptedPayload)
				}
			}
		})
	}
}

func TestPathToBytes(t *testing.T) {
	tests := []struct {
		name    string
		path    Path
		wantHex string
	}{
		{
			name: "minimal payload",
			path: Path{
				Destination:      0xA1,
				Source:           0xB2,
				MAC:              [2]byte{0xC3, 0xD4},
				EncryptedPayload: []byte{0xE5, 0xF6, 0xA7, 0xB8, 0xC9},
			},
			wantHex: "A1B2C3D4E5F6A7B8C9",
		},
		{
			name: "single block payload",
			path: Path{
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
			path: Path{
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
			got, err := tt.path.ToBytes()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotHex := hex.EncodeToString(got); !strings.EqualFold(gotHex, tt.wantHex) {
				t.Errorf("ToBytes() = %s, want %s", gotHex, tt.wantHex)
			}
		})
	}
}

func TestPathDecrypt(t *testing.T) {
	t.Run("wrong shared secret returns nil", func(t *testing.T) {
		_, aliceSeed, _ := generateTestKeyPair(t)
		_, bobSeed, _ := generateTestKeyPair(t)
		_, _, carolPub := generateTestKeyPair(t)

		sharedAliceBob, err := DeriveSharedSecret(aliceSeed, carolPub)
		if err != nil {
			t.Fatalf("deriving alice-carol secret: %v", err)
		}

		plaintext := []byte("path data")
		encrypted, err := EncryptThenMAC(sharedAliceBob, plaintext)
		if err != nil {
			t.Fatalf("encrypting: %v", err)
		}

		path := &Path{
			Destination:      0xAA,
			Source:           0xBB,
			MAC:              [2]byte{encrypted[0], encrypted[1]},
			EncryptedPayload: encrypted[2:],
		}

		wrongSecret, err := DeriveSharedSecret(bobSeed, carolPub)
		if err != nil {
			t.Fatalf("deriving wrong secret: %v", err)
		}

		got := path.Decrypt(wrongSecret)
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

		plaintext := []byte("path test data")
		encrypted, err := EncryptThenMAC(sharedAlice, plaintext)
		if err != nil {
			t.Fatalf("encrypting: %v", err)
		}

		path := &Path{
			Destination:      0xAA,
			Source:           0xBB,
			MAC:              [2]byte{encrypted[0], encrypted[1]},
			EncryptedPayload: encrypted[2:],
		}

		got := path.Decrypt(sharedBob)
		if got == nil {
			t.Fatal("Decrypt() returned nil, expected plaintext")
		}
		gotTrimmed := strings.TrimRight(string(got), "\x00")
		if gotTrimmed != string(plaintext) {
			t.Errorf("Decrypt() = %q, want %q", gotTrimmed, plaintext)
		}
	})
}

func TestPathVerifyMAC(t *testing.T) {
	_, aliceSeed, alicePub := generateTestKeyPair(t)
	_, bobSeed, bobPub := generateTestKeyPair(t)
	_, _, carolPub := generateTestKeyPair(t)

	sharedAlice, err := DeriveSharedSecret(aliceSeed, bobPub)
	if err != nil {
		t.Fatalf("deriving alice secret: %v", err)
	}

	plaintext := []byte("verify path")
	encrypted, err := EncryptThenMAC(sharedAlice, plaintext)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}

	path := &Path{
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
		if !path.VerifyMAC(sharedBob) {
			t.Error("VerifyMAC() = false, want true")
		}
	})

	t.Run("wrong shared secret", func(t *testing.T) {
		wrongSecret, err := DeriveSharedSecret(bobSeed, carolPub)
		if err != nil {
			t.Fatalf("deriving wrong secret: %v", err)
		}
		if path.VerifyMAC(wrongSecret) {
			t.Error("VerifyMAC() = true, want false")
		}
	})
}

func TestPathRoundTrip(t *testing.T) {
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

	plaintext := []byte("full round trip path message")
	encrypted, err := EncryptThenMAC(sharedAlice, plaintext)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}

	path := &Path{
		Destination:      0xAA,
		Source:           0xBB,
		MAC:              [2]byte{encrypted[0], encrypted[1]},
		EncryptedPayload: encrypted[2:],
	}

	wire, err := path.ToBytes()
	if err != nil {
		t.Fatalf("ToBytes(): %v", err)
	}

	decoded, err := PathFromBytes(wire)
	if err != nil {
		t.Fatalf("PathFromBytes(): %v", err)
	}

	if decoded.Destination != path.Destination {
		t.Errorf("Destination = 0x%02x, want 0x%02x", decoded.Destination, path.Destination)
	}
	if decoded.Source != path.Source {
		t.Errorf("Source = 0x%02x, want 0x%02x", decoded.Source, path.Source)
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

func TestParsePathPayload(t *testing.T) {
	plain := []byte{0x02, 0xAA, 0xBB, PayloadTypeAck | 0xF0, 1, 2, 3, 4, 0, 0}
	p, err := ParsePathPayload(plain)
	if err != nil {
		t.Fatal(err)
	}
	if hs := p.PathHashes(); len(hs) != 2 || hs[0][0] != 0xAA || hs[1][0] != 0xBB {
		t.Errorf("path hashes = %x", hs)
	}
	if p.ExtraType != PayloadTypeAck || len(p.Extra) != 6 || p.Extra[0] != 1 {
		t.Errorf("extra type %d data %x", p.ExtraType, p.Extra)
	}

	for _, bad := range []byte{0xC1, 0x40 | 40} {
		if _, err := ParsePathPayload([]byte{bad, 0, 0}); err == nil {
			t.Errorf("path_len 0x%02x accepted, want error", bad)
		}
	}
	if _, err := ParsePathPayload([]byte{0x03, 0xAA, 0xBB}); err == nil {
		t.Error("truncated path accepted, want error")
	}
}

func TestPathDecryptStruct(t *testing.T) {
	_, aliceSeed, alicePub := generateTestKeyPair(t)
	_, bobSeed, bobPub := generateTestKeyPair(t)
	sharedAlice, err := DeriveSharedSecret(aliceSeed, bobPub)
	if err != nil {
		t.Fatal(err)
	}
	sharedBob, err := DeriveSharedSecret(bobSeed, alicePub)
	if err != nil {
		t.Fatal(err)
	}

	plain := []byte{0x01, 0xAA, PayloadTypeAck, 1, 2, 3, 4}
	encrypted, err := EncryptThenMAC(sharedAlice, plain)
	if err != nil {
		t.Fatal(err)
	}
	path := &Path{MAC: [2]byte{encrypted[0], encrypted[1]}, EncryptedPayload: encrypted[2:]}

	got, err := path.DecryptStruct(sharedBob)
	if err != nil {
		t.Fatal(err)
	}
	if got.PathHashCount() != 1 || got.Path[0] != 0xAA || got.ExtraType != PayloadTypeAck || got.Extra[0] != 1 {
		t.Errorf("DecryptStruct() = %+v", got)
	}

	if _, err := path.DecryptStruct(make([]byte, 32)); !errors.Is(err, ErrBadMAC) {
		t.Errorf("DecryptStruct() with wrong key: error = %v, want ErrBadMAC", err)
	}
}

func FuzzParsePathPayload(f *testing.F) {
	f.Add([]byte{0x02, 0xAA, 0xBB, PayloadTypeAck | 0xF0, 1, 2, 3, 4, 0, 0})
	f.Add([]byte{0x01, 0xAA, PayloadTypeAck, 1, 2, 3, 4})
	f.Add([]byte{0x00, 0x00})
	f.Add([]byte{0xC1, 0, 0})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, plain []byte) {
		p, err := ParsePathPayload(plain)
		if err != nil {
			return
		}
		hs := p.PathHashes()
		if len(hs) != int(p.PathHashCount()) {
			t.Fatalf("PathHashes() = %d hashes, header says %d", len(hs), p.PathHashCount())
		}
		if p.ExtraType&^PacketTypeMask != 0 {
			t.Fatalf("ExtraType 0x%02x has reserved bits set", p.ExtraType)
		}
		if 1+len(p.Path)+1+len(p.Extra) != len(plain) {
			t.Fatalf("fields do not tile the input: %d+%d vs %d", len(p.Path), len(p.Extra), len(plain))
		}
	})
}

func TestPathPayloadToBytes(t *testing.T) {
	alice, bob, shared := testPeers(t)
	p := &PathPayload{PathLength: MakePathLen(2, 2), Path: []byte{0xA1, 0xA2, 0xB1, 0xB2}, ExtraType: PayloadTypeAck, Extra: []byte{1, 2, 3, 4}}
	plain, err := p.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{0x42, 0xA1, 0xA2, 0xB1, 0xB2, PayloadTypeAck, 1, 2, 3, 4}; !bytes.Equal(plain, want) {
		t.Fatalf("ToBytes = %x, want %x", plain, want)
	}
	pkt, err := NewPath(alice, bob.Identity, plain, shared)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := pkt.ToBytes()
	back, err := PathFromBytes(wire)
	if err != nil || back.Destination != bob.PublicKey()[0] || back.Source != alice.PublicKey()[0] {
		t.Fatalf("PathFromBytes = %+v, err %v", back, err)
	}
	got, err := back.DecryptStruct(shared)
	if err != nil || !bytes.Equal(got.Path, p.Path) || got.ExtraType != PayloadTypeAck || !bytes.HasPrefix(got.Extra, p.Extra) {
		t.Fatalf("DecryptStruct = %+v, err %v", got, err)
	}
}

func TestPathPayloadToBytesFiller(t *testing.T) {
	p := &PathPayload{PathLength: MakePathLen(1, 1), Path: []byte{0x7E}, ExtraType: PayloadTypeAck}
	a, err := p.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := p.ToBytes()
	if len(a) != 7 || a[2] != 0xFF || bytes.Equal(a[3:], b[3:]) {
		t.Fatalf("ToBytes = %x then %x, want 0x7E path, 0xFF, 4 fresh random bytes", a, b)
	}
}

func TestPathPayloadToBytesLimits(t *testing.T) {
	path := make([]byte, 64)
	ok := &PathPayload{PathLength: MakePathLen(2, 32), Path: path, Extra: make([]byte, maxCombinedPath-64-5)}
	if _, err := ok.ToBytes(); err != nil {
		t.Fatalf("combined path at the limit rejected: %v", err)
	}
	ok.Extra = append(ok.Extra, 0)
	if _, err := ok.ToBytes(); err == nil || !strings.HasPrefix(err.Error(), "meshcore: ") {
		t.Fatalf("combined path over the limit err = %v", err)
	}
	if _, err := (&PathPayload{PathLength: MakePathLen(1, 3), Path: []byte{1, 2}}).ToBytes(); err == nil || !strings.HasPrefix(err.Error(), "meshcore: ") {
		t.Fatalf("path shorter than path_len err = %v", err)
	}
	if _, err := (&PathPayload{PathLength: MakePathLen(4, 1), Path: []byte{1, 2, 3, 4}}).ToBytes(); err == nil || !strings.HasPrefix(err.Error(), "meshcore: ") {
		t.Fatalf("reserved hash size err = %v", err)
	}
}
