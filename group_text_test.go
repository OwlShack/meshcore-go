package meshcore

import (
	"bytes"
	"crypto/aes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestGroupTextFromBytes(t *testing.T) {
	tests := []struct {
		name                 string
		hex                  string
		wantErr              bool
		wantChannelHash      byte
		wantMAC              string
		wantEncryptedPayload string
	}{
		{
			name:                 "basic group text",
			hex:                  "A1C3D4E5F6A7B8C9",
			wantChannelHash:      0xA1,
			wantMAC:              "C3D4",
			wantEncryptedPayload: "E5F6A7B8C9",
		},
		{
			name:                 "large encrypted payload",
			hex:                  "A3E8883A7C376D8CED0FA0C0EB195D505A23D9FADC78379E1DBC44F7BBF8906D7A3302CB266B751B5683F67ECFAB43E0605DAB858896BC74810DDC37E68E436D5E1FB6",
			wantChannelHash:      0xA3,
			wantMAC:              "E888",
			wantEncryptedPayload: "3A7C376D8CED0FA0C0EB195D505A23D9FADC78379E1DBC44F7BBF8906D7A3302CB266B751B5683F67ECFAB43E0605DAB858896BC74810DDC37E68E436D5E1FB6",
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
			name:                 "2 bytes missing encrypted payload ok",
			hex:                  "A1C3D4",
			wantChannelHash:      0xA1,
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

			gt, err := GroupTextFromBytes(data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if gt.ChannelHash != tt.wantChannelHash {
				t.Errorf("ChannelHash = 0x%02x, want 0x%02x", gt.ChannelHash, tt.wantChannelHash)
			}
			if tt.wantMAC != "" {
				if got := hex.EncodeToString(gt.MAC[:]); !strings.EqualFold(got, tt.wantMAC) {
					t.Errorf("MAC = %s, want %s", got, tt.wantMAC)
				}
			}
			if tt.wantEncryptedPayload != "" {
				if got := hex.EncodeToString(gt.EncryptedPayload); !strings.EqualFold(got, tt.wantEncryptedPayload) {
					t.Errorf("EncryptedPayload = %s, want %s", got, tt.wantEncryptedPayload)
				}
			}
		})
	}
}

func TestGroupTextToBytes(t *testing.T) {
	tests := []struct {
		name    string
		gt      GroupText
		wantHex string
	}{
		{
			name: "minimal payload",
			gt: GroupText{
				ChannelHash:      0xA1,
				MAC:              [2]byte{0xC3, 0xD4},
				EncryptedPayload: []byte{0xE5, 0xF6, 0xA7, 0xB8, 0xC9},
			},
			wantHex: "A1C3D4E5F6A7B8C9",
		},
		{
			name: "single block payload",
			gt: GroupText{
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
			gt: GroupText{
				ChannelHash:      0xDE,
				MAC:              [2]byte{0xBE, 0xEF},
				EncryptedPayload: []byte{},
			},
			wantHex: "DEBEEF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.gt.ToBytes()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotHex := hex.EncodeToString(got); !strings.EqualFold(gotHex, tt.wantHex) {
				t.Errorf("ToBytes() = %s, want %s", gotHex, tt.wantHex)
			}
		})
	}
}

func TestGroupTextDecrypt(t *testing.T) {
	channelKey := make([]byte, 32)
	for i := range channelKey {
		channelKey[i] = byte(i + 1)
	}

	t.Run("wrong channel key returns nil", func(t *testing.T) {
		plaintext := []byte("hello group")
		encrypted, err := EncryptThenMAC(channelKey, plaintext)
		if err != nil {
			t.Fatalf("encrypting: %v", err)
		}

		gt := &GroupText{
			ChannelHash:      0xAA,
			MAC:              [2]byte{encrypted[0], encrypted[1]},
			EncryptedPayload: encrypted[2:],
		}

		wrongKey := make([]byte, 32)
		got := gt.Decrypt(wrongKey)
		if got != nil {
			t.Errorf("Decrypt() with wrong key = %x, want nil", got)
		}
	})

	t.Run("correct channel key recovers plaintext", func(t *testing.T) {
		plaintext := []byte("hello from group")
		encrypted, err := EncryptThenMAC(channelKey, plaintext)
		if err != nil {
			t.Fatalf("encrypting: %v", err)
		}

		gt := &GroupText{
			ChannelHash:      0xAA,
			MAC:              [2]byte{encrypted[0], encrypted[1]},
			EncryptedPayload: encrypted[2:],
		}

		got := gt.Decrypt(channelKey)
		if got == nil {
			t.Fatal("Decrypt() returned nil, expected plaintext")
		}
		gotTrimmed := strings.TrimRight(string(got), "\x00")
		if gotTrimmed != string(plaintext) {
			t.Errorf("Decrypt() = %q, want %q", gotTrimmed, plaintext)
		}
	})
}

func TestGroupTextVerifyMAC(t *testing.T) {
	channelKey := make([]byte, 32)
	for i := range channelKey {
		channelKey[i] = byte(i + 1)
	}

	plaintext := []byte("verify me")
	encrypted, err := EncryptThenMAC(channelKey, plaintext)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}

	gt := &GroupText{
		ChannelHash:      0xAA,
		MAC:              [2]byte{encrypted[0], encrypted[1]},
		EncryptedPayload: encrypted[2:],
	}

	t.Run("valid channel key", func(t *testing.T) {
		if !gt.VerifyMAC(channelKey) {
			t.Error("VerifyMAC() = false, want true")
		}
	})

	t.Run("wrong channel key", func(t *testing.T) {
		wrongKey := make([]byte, 32)
		if gt.VerifyMAC(wrongKey) {
			t.Error("VerifyMAC() = true, want false")
		}
	})
}

func TestGroupTextRoundTrip(t *testing.T) {
	channelKey := make([]byte, 32)
	for i := range channelKey {
		channelKey[i] = byte(i + 1)
	}

	plaintext := []byte("full round trip test message")
	encrypted, err := EncryptThenMAC(channelKey, plaintext)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}

	gt := &GroupText{
		ChannelHash:      0xAA,
		MAC:              [2]byte{encrypted[0], encrypted[1]},
		EncryptedPayload: encrypted[2:],
	}

	wire, err := gt.ToBytes()
	if err != nil {
		t.Fatalf("ToBytes(): %v", err)
	}

	decoded, err := GroupTextFromBytes(wire)
	if err != nil {
		t.Fatalf("GroupTextFromBytes(): %v", err)
	}

	if decoded.ChannelHash != gt.ChannelHash {
		t.Errorf("ChannelHash = 0x%02x, want 0x%02x", decoded.ChannelHash, gt.ChannelHash)
	}

	if !decoded.VerifyMAC(channelKey) {
		t.Fatal("VerifyMAC() failed after round trip")
	}

	got := decoded.Decrypt(channelKey)
	if got == nil {
		t.Fatal("Decrypt() returned nil after round trip")
	}
	gotTrimmed := strings.TrimRight(string(got), "\x00")
	if gotTrimmed != string(plaintext) {
		t.Errorf("Decrypt() = %q, want %q", gotTrimmed, plaintext)
	}
}

func TestGroupTextPayload_EncryptDecryptRoundTrip(t *testing.T) {
	ch := NewChannelFromHashtag("test-channel")

	payload := &GroupTextPayload{
		Timestamp: 1717000000,
		Flags:     0,
		Sender:    "Alice",
		Text:      "Hello, world!",
	}

	grp, err := payload.Encrypt(ch.Hash, ch.PSK[:])
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}
	if grp.ChannelHash != ch.Hash {
		t.Errorf("ChannelHash = 0x%02x, want 0x%02x", grp.ChannelHash, ch.Hash)
	}

	decoded, err := grp.DecryptStruct(ch.PSK[:])
	if err != nil {
		t.Fatalf("DecryptStruct() error: %v", err)
	}
	if decoded.Timestamp != payload.Timestamp {
		t.Errorf("Timestamp = %d, want %d", decoded.Timestamp, payload.Timestamp)
	}
	if decoded.Flags != payload.Flags {
		t.Errorf("Flags = %d, want %d", decoded.Flags, payload.Flags)
	}
	if decoded.Sender != payload.Sender {
		t.Errorf("Sender = %q, want %q", decoded.Sender, payload.Sender)
	}
	if decoded.Text != payload.Text {
		t.Errorf("Text = %q, want %q", decoded.Text, payload.Text)
	}
}

func TestGroupTextPayload_EncryptDecryptNoSender(t *testing.T) {
	ch := NewChannelFromHashtag("nosender")

	payload := &GroupTextPayload{
		Timestamp: 12345,
		Flags:     0,
		Text:      "plain message",
	}

	grp, err := payload.Encrypt(ch.Hash, ch.PSK[:])
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	decoded, err := grp.DecryptStruct(ch.PSK[:])
	if err != nil {
		t.Fatalf("DecryptStruct() error: %v", err)
	}
	if decoded.Sender != "" {
		t.Errorf("Sender = %q, want empty", decoded.Sender)
	}
	if decoded.Text != payload.Text {
		t.Errorf("Text = %q, want %q", decoded.Text, payload.Text)
	}
}

func TestGroupTextPayload_DecryptWrongKey(t *testing.T) {
	ch := NewChannelFromHashtag("correct-key")
	wrongPSK := [16]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	payload := &GroupTextPayload{
		Timestamp: 999,
		Flags:     0,
		Text:      "secret",
	}

	grp, err := payload.Encrypt(ch.Hash, ch.PSK[:])
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	_, err = grp.DecryptStruct(wrongPSK[:])
	if !errors.Is(err, ErrBadMAC) {
		t.Fatalf("DecryptStruct error = %v, want ErrBadMAC", err)
	}
}

func TestGroupTextPayload_EncryptClampsToMaxTextLen(t *testing.T) {
	ch := NewChannelFromHashtag("clamp")
	sender := "Alice"
	prefix := sender + ": "
	text := strings.Repeat("x", MaxTextLen-len(prefix)-1) + "🙂" + "tail"
	grp, err := (&GroupTextPayload{Timestamp: 1, Sender: sender, Text: text}).Encrypt(ch.Hash, ch.PSK[:])
	if err != nil {
		t.Fatal(err)
	}
	plain := grp.Decrypt(ch.PSK[:])
	body := strings.TrimRight(string(plain[5:]), "\x00")
	if len(body) != MaxTextLen-1 || !strings.HasPrefix(body, prefix) || strings.Contains(body, "tail") {
		t.Fatalf("body len %d = %q", len(body), body)
	}
	got, err := grp.DecryptStruct(ch.PSK[:])
	if err != nil || got.Sender != sender || got.Text != strings.Repeat("x", MaxTextLen-len(prefix)-1) {
		t.Fatalf("DecryptStruct = %+v err %v", got, err)
	}
	full := strings.Repeat("y", MaxTextLen-len(prefix))
	grp, _ = (&GroupTextPayload{Timestamp: 1, Sender: sender, Text: full}).Encrypt(ch.Hash, ch.PSK[:])
	got, _ = grp.DecryptStruct(ch.PSK[:])
	if got.Text != full {
		t.Fatalf("exact-length text was clamped: %d bytes", len(got.Text))
	}
}

func TestGroupTextPayload_EncryptEmptySenderKeepsPrefix(t *testing.T) {
	ch := NewChannelFromHashtag("nosender")
	grp, err := (&GroupTextPayload{Timestamp: 1, Text: "hi"}).Encrypt(ch.Hash, ch.PSK)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{1, 0, 0, 0, 0, ':', ' ', 'h', 'i'}; !bytes.HasPrefix(grp.Decrypt(ch.PSK), want) {
		t.Fatalf("plaintext = %q, want prefix %q", grp.Decrypt(ch.PSK), want)
	}
}

func TestGroupText_DecryptStructDropsNonPlainType(t *testing.T) {
	ch := NewChannelFromHashtag("types")
	for _, flags := range []byte{0x00, 0x03, TxtTypeCLIData << 2, TxtTypeSignedPlain << 2} {
		enc, err := EncryptThenMAC(ch.PSK, append([]byte{1, 0, 0, 0, flags}, "Bob: hi"...))
		if err != nil {
			t.Fatal(err)
		}
		grp := &GroupText{ChannelHash: ch.Hash, MAC: [2]byte{enc[0], enc[1]}, EncryptedPayload: enc[2:]}
		_, err = grp.DecryptStruct(ch.PSK)
		if plain := flags>>2 == TxtTypePlain; (err == nil) != plain {
			t.Errorf("flags 0x%02x: err %v, want accepted %v", flags, err, plain)
		}
	}
}

func TestGroupText_256BitChannel(t *testing.T) {
	psk := make([]byte, 32)
	for i := range psk {
		psk[i] = byte(i + 1)
	}
	ch, err := NewChannelFromPSK("wide", psk)
	if err != nil {
		t.Fatal(err)
	}
	if h := sha256.Sum256(psk); ch.Hash != h[0] {
		t.Fatalf("Hash = 0x%02x, want SHA256 of all 32 bytes 0x%02x", ch.Hash, h[0])
	}
	grp, err := (&GroupTextPayload{Timestamp: 5, Sender: "Ana", Text: "kia ora"}).Encrypt(ch.Hash, ch.PSK)
	if err != nil {
		t.Fatal(err)
	}

	block, _ := aes.NewCipher(psk[:16])
	var want [16]byte
	block.Encrypt(want[:], append([]byte{5, 0, 0, 0, 0}, "Ana: kia ora"...)[:16])
	mac := hmac.New(sha256.New, psk)
	mac.Write(grp.EncryptedPayload)
	if !bytes.Equal(grp.EncryptedPayload[:16], want[:]) || !bytes.Equal(grp.MAC[:], mac.Sum(nil)[:2]) {
		t.Fatal("want AES-128 under the first 16 bytes and HMAC under all 32")
	}

	got, err := grp.DecryptStruct(ch.PSK)
	if err != nil || got.Sender != "Ana" || got.Text != "kia ora" {
		t.Fatalf("DecryptStruct = %+v, err %v", got, err)
	}
	half := append(append([]byte(nil), psk[:16]...), make([]byte, 16)...)
	if grp.VerifyMAC(half) {
		t.Fatal("MAC verified without the upper 16 key bytes")
	}
}

func TestGroupText_128BitKeyMatchesZeroPadded(t *testing.T) {
	ch := PublicChannel()
	grp, _ := (&GroupTextPayload{Timestamp: 1, Sender: "a", Text: "b"}).Encrypt(ch.Hash, ch.PSK)
	if !grp.VerifyMAC(append(append([]byte(nil), ch.PSK...), make([]byte, 16)...)) {
		t.Fatal("16-byte key MAC differs from its zero-padded 32-byte form")
	}
}
