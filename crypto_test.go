package meshcore

import (
	"bytes"
	"crypto/aes"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

func TestDeriveSharedSecret(t *testing.T) {
	t.Run("invalid seed length", func(t *testing.T) {
		peer := make([]byte, 32)
		_, err := DeriveSharedSecret([]byte{0x01, 0x02, 0x03}, peer)
		if err == nil {
			t.Fatal("expected error for short seed, got nil")
		}
		if !strings.Contains(err.Error(), "private key seed must be 32 bytes") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid peer public key length", func(t *testing.T) {
		seed := make([]byte, 32)
		_, err := DeriveSharedSecret(seed, []byte{0x01})
		if err == nil {
			t.Fatal("expected error for short peer key, got nil")
		}
		if !strings.Contains(err.Error(), "peer public key must be 32 bytes") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("valid key pair produces 32 byte secret", func(t *testing.T) {
		pubA, privA, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}
		pubB, _, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}

		secret, err := DeriveSharedSecret(privA.Seed(), pubB)
		if err != nil {
			t.Fatalf("DeriveSharedSecret: %v", err)
		}
		if len(secret) != 32 {
			t.Errorf("secret length = %d, want 32", len(secret))
		}
		if bytes.Equal(secret, []byte(pubA)) || bytes.Equal(secret, []byte(pubB)) {
			t.Error("shared secret should not equal either public key")
		}
	})

	t.Run("symmetric: A->B equals B->A", func(t *testing.T) {
		pubA, privA, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatalf("GenerateKey A: %v", err)
		}
		pubB, privB, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatalf("GenerateKey B: %v", err)
		}

		secretAB, err := DeriveSharedSecret(privA.Seed(), pubB)
		if err != nil {
			t.Fatalf("DeriveSharedSecret A->B: %v", err)
		}
		secretBA, err := DeriveSharedSecret(privB.Seed(), pubA)
		if err != nil {
			t.Fatalf("DeriveSharedSecret B->A: %v", err)
		}

		if !bytes.Equal(secretAB, secretBA) {
			t.Errorf("shared secrets differ:\n  A->B: %x\n  B->A: %x", secretAB, secretBA)
		}
	})

	t.Run("different peers produce different secrets", func(t *testing.T) {
		_, privA, _ := ed25519.GenerateKey(nil)
		pubB, _, _ := ed25519.GenerateKey(nil)
		pubC, _, _ := ed25519.GenerateKey(nil)

		secretAB, err := DeriveSharedSecret(privA.Seed(), pubB)
		if err != nil {
			t.Fatalf("DeriveSharedSecret A->B: %v", err)
		}
		secretAC, err := DeriveSharedSecret(privA.Seed(), pubC)
		if err != nil {
			t.Fatalf("DeriveSharedSecret A->C: %v", err)
		}

		if bytes.Equal(secretAB, secretAC) {
			t.Error("different peers should produce different shared secrets")
		}
	})

	t.Run("invalid 32-byte peer key point", func(t *testing.T) {
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}

		badPeer := make([]byte, 32)
		badPeer[0] = 0x02

		_, err = DeriveSharedSecret(priv.Seed(), badPeer)
		if err == nil {
			t.Fatal("expected error for invalid peer point, got nil")
		}
		if !strings.Contains(err.Error(), "converting peer public key") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestEdPublicToX25519(t *testing.T) {
	t.Run("valid ed25519 public key", func(t *testing.T) {
		pub, _, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}

		x25519Pub, err := edPublicToX25519(pub)
		if err != nil {
			t.Fatalf("edPublicToX25519: %v", err)
		}
		if len(x25519Pub) != 32 {
			t.Errorf("x25519 public key length = %d, want 32", len(x25519Pub))
		}
	})

	t.Run("invalid point returns error", func(t *testing.T) {
		badKey := make([]byte, 32)
		badKey[0] = 0x02
		_, err := edPublicToX25519(badKey)
		if err == nil {
			t.Fatal("expected error for invalid point, got nil")
		}
	})
}

func TestEncrypt(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	tests := []struct {
		name       string
		plaintext  []byte
		wantOutLen int
	}{
		{
			name:       "empty input",
			plaintext:  []byte{},
			wantOutLen: 0,
		},
		{
			name:       "exact one block (16 bytes)",
			plaintext:  []byte("0123456789ABCDEF"),
			wantOutLen: 16,
		},
		{
			name:       "partial block (5 bytes pads to 16)",
			plaintext:  []byte("Hello"),
			wantOutLen: 16,
		},
		{
			name:       "two exact blocks (32 bytes)",
			plaintext:  []byte("0123456789ABCDEF0123456789ABCDEF"),
			wantOutLen: 32,
		},
		{
			name:       "one block plus partial (20 bytes pads to 32)",
			plaintext:  []byte("01234567890123456789"),
			wantOutLen: 32,
		},
		{
			name:       "single byte pads to 16",
			plaintext:  []byte{0x42},
			wantOutLen: 16,
		},
		{
			name:       "15 bytes pads to 16",
			plaintext:  bytes.Repeat([]byte{0xAA}, 15),
			wantOutLen: 16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Encrypt(key, tt.plaintext)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}
			if len(got) != tt.wantOutLen {
				t.Errorf("ciphertext length = %d, want %d", len(got), tt.wantOutLen)
			}

			if len(tt.plaintext) > 0 && bytes.Equal(got[:len(tt.plaintext)], tt.plaintext) {
				t.Error("ciphertext should differ from plaintext")
			}
		})
	}

	t.Run("short shared secret panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic for short shared secret, got nil")
			}
		}()

		_, _ = Encrypt([]byte{0x01}, []byte("hello"))
	})
}

func TestDecrypt(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	t.Run("encrypt then decrypt exact block recovers plaintext", func(t *testing.T) {
		plaintext := []byte("0123456789ABCDEF")

		encrypted, err := Encrypt(key, plaintext)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}

		decrypted, err := Decrypt(key, encrypted)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}

		if !bytes.Equal(decrypted, plaintext) {
			t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
		}
	})

	t.Run("encrypt then decrypt partial block recovers padded plaintext", func(t *testing.T) {
		plaintext := []byte("Hello")

		encrypted, err := Encrypt(key, plaintext)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}

		decrypted, err := Decrypt(key, encrypted)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}

		trimmed := bytes.TrimRight(decrypted, "\x00")
		if !bytes.Equal(trimmed, plaintext) {
			t.Errorf("decrypted (trimmed) = %q, want %q", trimmed, plaintext)
		}
	})

	t.Run("encrypt then decrypt multi-block", func(t *testing.T) {
		plaintext := []byte("0123456789ABCDEF0123456789ABCDEF")

		encrypted, err := Encrypt(key, plaintext)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}

		decrypted, err := Decrypt(key, encrypted)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}

		if !bytes.Equal(decrypted, plaintext) {
			t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
		}
	})

	t.Run("wrong key produces garbage", func(t *testing.T) {
		plaintext := []byte("0123456789ABCDEF")

		encrypted, err := Encrypt(key, plaintext)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}

		wrongKey := make([]byte, 32)
		for i := range wrongKey {
			wrongKey[i] = byte(i + 100)
		}

		decrypted, err := Decrypt(wrongKey, encrypted)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}

		if bytes.Equal(decrypted, plaintext) {
			t.Error("wrong key should not recover plaintext")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		decrypted, err := Decrypt(key, []byte{})
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if len(decrypted) != 0 {
			t.Errorf("decrypted length = %d, want 0", len(decrypted))
		}
	})

	t.Run("short shared secret panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic for short shared secret, got nil")
			}
		}()

		_, _ = Decrypt([]byte{0x01}, []byte("0123456789ABCDEF"))
	})
}

func TestEncryptECBMode(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	block := []byte("0123456789ABCDEF")
	twoBlocks := append(block, block...)

	encrypted, err := Encrypt(key, twoBlocks)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if !bytes.Equal(encrypted[:aes.BlockSize], encrypted[aes.BlockSize:]) {
		t.Error("ECB mode: identical plaintext blocks should produce identical ciphertext blocks")
	}
}

func TestEncryptThenMAC(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	t.Run("output has 2 byte MAC prefix", func(t *testing.T) {
		plaintext := []byte("Hello MeshCore")

		result, err := EncryptThenMAC(key, plaintext)
		if err != nil {
			t.Fatalf("EncryptThenMAC: %v", err)
		}

		expectedCipherLen := cipherMACSize + 16
		if len(result) != expectedCipherLen {
			t.Errorf("output length = %d, want %d", len(result), expectedCipherLen)
		}
	})

	t.Run("MAC is deterministic", func(t *testing.T) {
		plaintext := []byte("same input")

		result1, _ := EncryptThenMAC(key, plaintext)
		result2, _ := EncryptThenMAC(key, plaintext)

		if !bytes.Equal(result1, result2) {
			t.Error("EncryptThenMAC should be deterministic for same key and plaintext")
		}
	})

	t.Run("different keys produce different MAC", func(t *testing.T) {
		plaintext := []byte("same input")

		key2 := make([]byte, 32)
		for i := range key2 {
			key2[i] = byte(i + 50)
		}

		result1, _ := EncryptThenMAC(key, plaintext)
		result2, _ := EncryptThenMAC(key2, plaintext)

		if bytes.Equal(result1[:cipherMACSize], result2[:cipherMACSize]) {
			t.Error("different keys should usually produce different MACs")
		}
	})
}

func TestMACThenDecrypt(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	t.Run("round trip recovers plaintext", func(t *testing.T) {
		plaintext := []byte("round trip test!")

		encrypted, err := EncryptThenMAC(key, plaintext)
		if err != nil {
			t.Fatalf("EncryptThenMAC: %v", err)
		}

		decrypted, err := MACThenDecrypt(key, encrypted)
		if err != nil {
			t.Fatalf("MACThenDecrypt: %v", err)
		}
		if decrypted == nil {
			t.Fatal("MACThenDecrypt returned nil (MAC rejected)")
		}

		if !bytes.Equal(decrypted, plaintext) {
			t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
		}
	})

	t.Run("round trip with partial block", func(t *testing.T) {
		plaintext := []byte("short")

		encrypted, err := EncryptThenMAC(key, plaintext)
		if err != nil {
			t.Fatalf("EncryptThenMAC: %v", err)
		}

		decrypted, err := MACThenDecrypt(key, encrypted)
		if err != nil {
			t.Fatalf("MACThenDecrypt: %v", err)
		}
		if decrypted == nil {
			t.Fatal("MACThenDecrypt returned nil (MAC rejected)")
		}

		trimmed := bytes.TrimRight(decrypted, "\x00")
		if !bytes.Equal(trimmed, plaintext) {
			t.Errorf("decrypted (trimmed) = %q, want %q", trimmed, plaintext)
		}
	})

	t.Run("wrong key returns nil (MAC mismatch)", func(t *testing.T) {
		plaintext := []byte("secret message!!")

		encrypted, err := EncryptThenMAC(key, plaintext)
		if err != nil {
			t.Fatalf("EncryptThenMAC: %v", err)
		}

		wrongKey := make([]byte, 32)
		for i := range wrongKey {
			wrongKey[i] = byte(i + 100)
		}

		decrypted, err := MACThenDecrypt(wrongKey, encrypted)
		if !errors.Is(err, ErrBadMAC) {
			t.Fatalf("MACThenDecrypt error = %v, want ErrBadMAC", err)
		}
		if decrypted != nil {
			t.Error("expected nil for wrong key, got data")
		}
	})

	t.Run("validly MACed partial block is rejected", func(t *testing.T) {
		enc := make([]byte, aes.BlockSize+1)
		mac := hmac.New(sha256.New, key)
		mac.Write(enc)
		src := append(mac.Sum(nil)[:cipherMACSize], enc...)

		if _, err := MACThenDecrypt(key, src); !errors.Is(err, ErrNotBlockAligned) {
			t.Fatalf("error = %v, want ErrNotBlockAligned", err)
		}
	})

	t.Run("too short input returns error", func(t *testing.T) {
		_, err := MACThenDecrypt(key, []byte{0x00})
		if !errors.Is(err, ErrTooShort) {
			t.Fatalf("error = %v, want ErrTooShort", err)
		}
		if !strings.Contains(err.Error(), "data too short") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("exactly 2 bytes (MAC only, no data) returns error", func(t *testing.T) {
		_, err := MACThenDecrypt(key, []byte{0x00, 0x00})
		if err == nil {
			t.Fatal("expected error for 2 byte input, got nil")
		}
		if !strings.Contains(err.Error(), "data too short") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("tampered ciphertext fails MAC", func(t *testing.T) {
		plaintext := []byte("do not tamper!!!")

		encrypted, err := EncryptThenMAC(key, plaintext)
		if err != nil {
			t.Fatalf("EncryptThenMAC: %v", err)
		}

		tampered := make([]byte, len(encrypted))
		copy(tampered, encrypted)
		tampered[cipherMACSize] ^= 0xFF

		decrypted, err := MACThenDecrypt(key, tampered)
		if !errors.Is(err, ErrBadMAC) {
			t.Fatalf("MACThenDecrypt error = %v, want ErrBadMAC", err)
		}
		if decrypted != nil {
			t.Error("expected nil for tampered ciphertext, got data")
		}
	})

	t.Run("tampered MAC fails verification", func(t *testing.T) {
		plaintext := []byte("do not tamper!!!")

		encrypted, err := EncryptThenMAC(key, plaintext)
		if err != nil {
			t.Fatalf("EncryptThenMAC: %v", err)
		}

		tampered := make([]byte, len(encrypted))
		copy(tampered, encrypted)
		tampered[0] ^= 0xFF

		decrypted, err := MACThenDecrypt(key, tampered)
		if !errors.Is(err, ErrBadMAC) {
			t.Fatalf("MACThenDecrypt error = %v, want ErrBadMAC", err)
		}
		if decrypted != nil {
			t.Error("expected nil for tampered MAC, got data")
		}
	})
}

func TestEncryptDecryptFullPipeline(t *testing.T) {
	pubA, privA, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey A: %v", err)
	}
	pubB, privB, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey B: %v", err)
	}

	secretAB, err := DeriveSharedSecret(privA.Seed(), pubB)
	if err != nil {
		t.Fatalf("DeriveSharedSecret A->B: %v", err)
	}

	plaintext := []byte("Hello from A to B via MeshCore!")

	encrypted, err := EncryptThenMAC(secretAB, plaintext)
	if err != nil {
		t.Fatalf("EncryptThenMAC: %v", err)
	}

	secretBA, err := DeriveSharedSecret(privB.Seed(), pubA)
	if err != nil {
		t.Fatalf("DeriveSharedSecret B->A: %v", err)
	}

	decrypted, err := MACThenDecrypt(secretBA, encrypted)
	if err != nil {
		t.Fatalf("MACThenDecrypt: %v", err)
	}
	if decrypted == nil {
		t.Fatal("MACThenDecrypt returned nil (MAC rejected)")
	}

	trimmed := bytes.TrimRight(decrypted, "\x00")
	if !bytes.Equal(trimmed, plaintext) {
		t.Errorf("decrypted = %q, want %q", trimmed, plaintext)
	}
}

func TestAES_FIPS197_Vector(t *testing.T) {
	secret := hexDecode(t, "000102030405060708090a0b0c0d0e0f"+"ffffffffffffffffffffffffffffffff")
	pt := hexDecode(t, "00112233445566778899aabbccddeeff")
	want := hexDecode(t, "69c4e0d86a7b0430d8cdb78070b4c55a")

	ct, err := Encrypt(secret, pt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ct, want) {
		t.Fatalf("Encrypt() = %x, want %x", ct, want)
	}
	back, err := Decrypt(secret, want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, pt) {
		t.Fatalf("Decrypt() = %x, want %x", back, pt)
	}
	ct2, _ := Encrypt(secret, append(append([]byte{}, pt...), pt...))
	if !bytes.Equal(ct2, append(append([]byte{}, want...), want...)) {
		t.Fatalf("ECB two-block Encrypt() = %x", ct2)
	}
	padded, _ := Encrypt(secret, pt[:5])
	ref, _ := Encrypt(secret, append(pt[:5], make([]byte, 11)...))
	if !bytes.Equal(padded, ref) {
		t.Fatal("partial block is not zero-padded")
	}
}

func TestEncryptedPayloads_RejectShortMAC(t *testing.T) {
	eph := bytes.Repeat([]byte{0x11}, PubKeySize)
	cases := []struct {
		name string
		in   []byte
		call func([]byte) error
	}{
		{"text message", []byte{1, 2, 3}, func(b []byte) error { _, err := TextMessageFromBytes(b); return err }},
		{"request", []byte{1, 2, 3}, func(b []byte) error { _, err := RequestFromBytes(b); return err }},
		{"response", []byte{1, 2, 3}, func(b []byte) error { _, err := ResponseFromBytes(b); return err }},
		{"path", []byte{1, 2, 3}, func(b []byte) error { _, err := PathFromBytes(b); return err }},
		{"group text", []byte{1, 2}, func(b []byte) error { _, err := GroupTextFromBytes(b); return err }},
		{"group data", []byte{1, 2}, func(b []byte) error { _, err := GroupDataFromBytes(b); return err }},
		{"anon req", append(append([]byte{1}, eph...), 2), func(b []byte) error { _, err := AnonReqFromBytes(b); return err }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.call(c.in)
			if !errors.Is(err, ErrTooShort) {
				t.Fatalf("%d-byte input: error = %v, want ErrTooShort", len(c.in), err)
			}
			if err := c.call(append(c.in, 0xAA)); err != nil {
				t.Fatalf("full header rejected: %v", err)
			}
		})
	}
}

func TestEncryptedPayloads_BadMACContract(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	enc, err := EncryptThenMAC(key, []byte{0x01, 0xAA, PayloadTypeAck, 1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	mac := [2]byte{enc[0] ^ 0xFF, enc[1]}
	wrong := bytes.Repeat([]byte{8}, 32)

	tm := &TextMessage{MAC: mac, EncryptedPayload: enc[2:]}
	if tm.Decrypt(key) != nil || tm.VerifyMAC(key) {
		t.Error("TextMessage accepted tampered MAC")
	}
	tm.MAC = [2]byte{enc[0], enc[1]}
	if tm.Decrypt(wrong) != nil || tm.Decrypt(key) == nil {
		t.Error("TextMessage Decrypt wrong-key/right-key contract broken")
	}

	p := &Path{MAC: mac, EncryptedPayload: enc[2:]}
	if _, err := p.DecryptStruct(key); !errors.Is(err, ErrBadMAC) {
		t.Errorf("Path.DecryptStruct error = %v, want ErrBadMAC", err)
	}
	g := &GroupText{MAC: mac, EncryptedPayload: enc[2:]}
	if _, err := g.DecryptStruct(key); !errors.Is(err, ErrBadMAC) {
		t.Errorf("GroupText.DecryptStruct error = %v, want ErrBadMAC", err)
	}
	if _, err := (&GroupText{}).DecryptStruct(key); !errors.Is(err, ErrTooShort) {
		t.Errorf("empty GroupText.DecryptStruct error = %v, want ErrTooShort", err)
	}
}
