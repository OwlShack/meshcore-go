package meshcore

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestNewChannelFromPSK(t *testing.T) {
	psk := make([]byte, 16)
	psk[0] = 0xAA
	ch, err := NewChannelFromPSK("test", psk)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.Name != "test" {
		t.Errorf("Name = %q, want %q", ch.Name, "test")
	}
	if ch.PSK[0] != 0xAA {
		t.Errorf("PSK[0] = 0x%02x, want 0xAA", ch.PSK[0])
	}
	if ch.Hash == 0 && bytes.Equal(ch.PSK, make([]byte, 16)) {
		t.Error("Hash should be derived from PSK")
	}
}

func TestNewChannelFromPSK_InvalidLength(t *testing.T) {
	for _, n := range []int{2, 15, 24, 33} {
		if _, err := NewChannelFromPSK("bad", make([]byte, n)); err == nil {
			t.Fatalf("expected error for %d-byte PSK", n)
		}
	}
}

func TestNewChannelFromBase64_256Bit(t *testing.T) {
	ch, err := NewChannelFromBase64("wide", "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=")
	if err != nil || len(ch.PSK) != 32 || ch.PSK[31] != 0x20 {
		t.Fatalf("NewChannelFromBase64 = %+v, err %v", ch, err)
	}
}

func TestNewChannelFromBase64(t *testing.T) {
	// 16 zero bytes in base64
	ch, err := NewChannelFromBase64("b64test", "AAAAAAAAAAAAAAAAAAAAAA==")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.Name != "b64test" {
		t.Errorf("Name = %q, want %q", ch.Name, "b64test")
	}
	if !bytes.Equal(ch.PSK, make([]byte, 16)) {
		t.Errorf("expected zero PSK, got %x", ch.PSK)
	}
}

func TestNewChannelFromBase64_Invalid(t *testing.T) {
	_, err := NewChannelFromBase64("bad", "not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestNewChannelFromHashtag(t *testing.T) {
	ch := NewChannelFromHashtag("general")
	if ch.Name != "#general" {
		t.Errorf("Name = %q, want %q", ch.Name, "#general")
	}
	ch2 := NewChannelFromHashtag("#general")
	if ch2.Name != "#general" {
		t.Errorf("Name = %q, want %q", ch2.Name, "#general")
	}
	if !bytes.Equal(ch.PSK, ch2.PSK) {
		t.Error("same hashtag should produce same PSK")
	}
	if ch.Hash != ch2.Hash {
		t.Error("same hashtag should produce same Hash")
	}
}

func TestNormalizeHashtag(t *testing.T) {
	if got := NormalizeHashtag("foo"); got != "#foo" {
		t.Errorf("NormalizeHashtag(foo) = %q, want #foo", got)
	}
	if got := NormalizeHashtag("#bar"); got != "#bar" {
		t.Errorf("NormalizeHashtag(#bar) = %q, want #bar", got)
	}
}

func TestDeriveHashtagPSK(t *testing.T) {
	psk := DeriveHashtagPSK("test")
	psk2 := DeriveHashtagPSK("#test")
	if psk != psk2 {
		t.Error("normalized and explicit should produce same PSK")
	}
	if psk == [16]byte{} {
		t.Error("PSK should not be all zeros")
	}
}

func TestDeriveChannelHash(t *testing.T) {
	psk := DeriveHashtagPSK("#hello")
	hash := DeriveChannelHash(psk)
	ch := NewChannelFromHashtag("hello")
	if hash != ch.Hash {
		t.Errorf("DeriveChannelHash = 0x%02x, want 0x%02x from NewChannelFromHashtag", hash, ch.Hash)
	}
}

func TestHashtagChannel_FirmwareVector(t *testing.T) {
	psk := DeriveHashtagPSK("test")
	if got := hex.EncodeToString(psk[:]); got != "9cd8fcf22a47333b591d96a2b848b73f" {
		t.Fatalf("DeriveHashtagPSK(test) = %s", got)
	}
	if got := DeriveChannelHash(psk); got != 0xd9 {
		t.Fatalf("DeriveChannelHash = 0x%02x, want 0xd9", got)
	}
	ch := NewChannelFromHashtag("#test")
	if !bytes.Equal(ch.PSK, psk[:]) || ch.Hash != 0xd9 || ch.Name != "#test" {
		t.Fatalf("NewChannelFromHashtag = %+v", ch)
	}
}

func TestPublicChannel(t *testing.T) {
	ch := PublicChannel()
	if ch.Name != "Public" || hex.EncodeToString(ch.PSK[:]) != "8b3387e9c5cdea6ac9e5edbaa115cd72" {
		t.Fatalf("PublicChannel() = %q %x", ch.Name, ch.PSK)
	}
	ch.PSK[0] ^= 0xFF
	if again := PublicChannel(); again == ch || again.PSK[0] == ch.PSK[0] {
		t.Fatal("PublicChannel() returned a shared entry")
	}
}
