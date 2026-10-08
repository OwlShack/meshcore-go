package meshcore

import (
	"crypto/aes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"

	"filippo.io/edwards25519"
	"golang.org/x/crypto/curve25519"
)

const (
	cipherKeySize = 16 // AES-128
	cipherMACSize = 2  // truncated HMAC
)

var (
	// ErrBadMAC is returned when the truncated HMAC does not match.
	ErrBadMAC = errors.New("meshcore: mac mismatch")
	// ErrTooShort is returned when a payload is shorter than its fixed header.
	ErrTooShort = errors.New("meshcore: data too short")
	// ErrNotBlockAligned is returned when ciphertext is not a whole number of AES blocks.
	ErrNotBlockAligned = errors.New("meshcore: ciphertext not block aligned")
)

// DeriveSharedSecret computes an X25519 shared secret from an Ed25519
// private key seed (32 bytes) and a peer's Ed25519 public key (32 bytes).
func DeriveSharedSecret(privateKeySeed []byte, peerPublicKey []byte) ([]byte, error) {
	if len(privateKeySeed) != 32 {
		return nil, fmt.Errorf("private key seed must be 32 bytes, got %d", len(privateKeySeed))
	}
	if len(peerPublicKey) != 32 {
		return nil, fmt.Errorf("peer public key must be 32 bytes, got %d", len(peerPublicKey))
	}

	x25519Private := edPrivateToX25519(privateKeySeed)

	x25519Public, err := edPublicToX25519(peerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("converting peer public key: %w", err)
	}

	shared, err := curve25519.X25519(x25519Private, x25519Public)
	if err != nil {
		return nil, fmt.Errorf("computing shared secret: %w", err)
	}

	return shared, nil
}

// deriveExpandedPubKey derives the public key from an expanded key's clamped 32-byte scalar.
func deriveExpandedPubKey(scalar []byte) []byte {
	a, _ := edwards25519.NewScalar().SetBytesWithClamping(scalar)
	return new(edwards25519.Point).ScalarBaseMult(a).Bytes()
}

// signWithExpandedKey produces an RFC 8032 signature from an expanded private
// key (clamped scalar ‖ prefix).
func signWithExpandedKey(scalar, prefix, pub, message []byte) []byte {
	a, _ := edwards25519.NewScalar().SetBytesWithClamping(scalar)

	hr := sha512.New()
	hr.Write(prefix)
	hr.Write(message)
	r, _ := edwards25519.NewScalar().SetUniformBytes(hr.Sum(nil))
	rBytes := new(edwards25519.Point).ScalarBaseMult(r).Bytes()

	hk := sha512.New()
	hk.Write(rBytes)
	hk.Write(pub)
	hk.Write(message)
	k, _ := edwards25519.NewScalar().SetUniformBytes(hk.Sum(nil))

	// S = r + k*a  (mod L)
	s := edwards25519.NewScalar().MultiplyAdd(k, a, r)

	return append(rBytes, s.Bytes()...)
}

// sharedSecretFromScalar X25519s with an expanded key's already-clamped scalar
// (curve25519.X25519 re-clamps idempotently).
func sharedSecretFromScalar(scalar, peerPublicKey []byte) ([]byte, error) {
	x25519Public, err := edPublicToX25519(peerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("converting peer public key: %w", err)
	}
	return curve25519.X25519(scalar, x25519Public)
}

// edPrivateToX25519 converts an Ed25519 private key seed to a clamped X25519 private key.
func edPrivateToX25519(seed []byte) []byte {
	h := sha512.Sum512(seed)
	h[0] &= 248
	h[31] &= 127
	h[31] |= 64
	return h[:32]
}

// edPublicToX25519 converts an Ed25519 public key to an X25519 public key.
func edPublicToX25519(edPub []byte) ([]byte, error) {
	p, err := new(edwards25519.Point).SetBytes(edPub)
	if err != nil {
		return nil, err
	}
	return p.BytesMontgomery(), nil
}

// Decrypt decrypts src using AES-128 ECB with the first 16 bytes of sharedSecret.
func Decrypt(sharedSecret []byte, src []byte) ([]byte, error) {
	if len(sharedSecret) < cipherKeySize {
		return nil, fmt.Errorf("meshcore: key is %d bytes, need at least %d", len(sharedSecret), cipherKeySize)
	}
	block, err := aes.NewCipher(sharedSecret[:cipherKeySize])
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	dest := make([]byte, len(src))
	for i := 0; i+aes.BlockSize <= len(src); i += aes.BlockSize {
		block.Decrypt(dest[i:i+aes.BlockSize], src[i:i+aes.BlockSize])
	}

	return dest, nil
}

// Encrypt encrypts src using AES-128 ECB with the first 16 bytes of
// sharedSecret, zero-padding a partial final block.
func Encrypt(sharedSecret []byte, src []byte) ([]byte, error) {
	if len(sharedSecret) < cipherKeySize {
		return nil, fmt.Errorf("meshcore: key is %d bytes, need at least %d", len(sharedSecret), cipherKeySize)
	}
	block, err := aes.NewCipher(sharedSecret[:cipherKeySize])
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	outLen := len(src)
	if outLen%aes.BlockSize != 0 {
		outLen += aes.BlockSize - (outLen % aes.BlockSize)
	}
	dest := make([]byte, outLen)

	i := 0
	for i+aes.BlockSize <= len(src) {
		block.Encrypt(dest[i:i+aes.BlockSize], src[i:i+aes.BlockSize])
		i += aes.BlockSize
	}
	if i < len(src) {
		var tmp [aes.BlockSize]byte
		copy(tmp[:], src[i:])
		block.Encrypt(dest[i:i+aes.BlockSize], tmp[:])
	}

	return dest, nil
}

// EncryptThenMAC encrypts src and returns a 2-byte HMAC-SHA256 MAC followed by the ciphertext.
func EncryptThenMAC(sharedSecret []byte, src []byte) ([]byte, error) {
	encrypted, err := Encrypt(sharedSecret, src)
	if err != nil {
		return nil, err
	}

	mac := hmac.New(sha256.New, sharedSecret)
	mac.Write(encrypted)
	computed := mac.Sum(nil)

	result := make([]byte, cipherMACSize+len(encrypted))
	copy(result[:cipherMACSize], computed[:cipherMACSize])
	copy(result[cipherMACSize:], encrypted)

	return result, nil
}

// MACThenDecrypt verifies the leading 2-byte HMAC-SHA256 MAC of src and decrypts the rest.
func MACThenDecrypt(sharedSecret []byte, src []byte) ([]byte, error) {
	if len(src) <= cipherMACSize {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooShort, len(src))
	}
	if (len(src)-cipherMACSize)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("%w: %d bytes", ErrNotBlockAligned, len(src)-cipherMACSize)
	}

	mac := hmac.New(sha256.New, sharedSecret)
	mac.Write(src[cipherMACSize:])
	computed := mac.Sum(nil)

	if !hmac.Equal(computed[:cipherMACSize], src[:cipherMACSize]) {
		return nil, ErrBadMAC
	}

	return Decrypt(sharedSecret, src[cipherMACSize:])
}

// macEncrypt is EncryptThenMAC split into MAC and ciphertext fields.
func macEncrypt(key, plain []byte) (mac [2]byte, enc []byte, err error) {
	out, err := EncryptThenMAC(key, plain)
	if err != nil {
		return mac, nil, err
	}
	copy(mac[:], out)
	return mac, out[cipherMACSize:], nil
}

// macDecryptErr is MACThenDecrypt for split MAC/ciphertext fields.
func macDecryptErr(key []byte, mac [2]byte, enc []byte) ([]byte, error) {
	return MACThenDecrypt(key, append(mac[:], enc...))
}

// macDecrypt is macDecryptErr with a nil-on-failure result.
func macDecrypt(key []byte, mac [2]byte, enc []byte) []byte {
	plain, _ := macDecryptErr(key, mac, enc)
	return plain
}
