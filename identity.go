package meshcore

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"io"
)

// PathHashSize is the number of leading public-key bytes used as a compact identity hash.
const PathHashSize = 1

// Identity is a peer's public identity: an Ed25519 public key with verification
// and key-exchange helpers.
type Identity struct {
	pubKey [PubKeySize]byte
}

// NewIdentity creates an Identity from a 32-byte Ed25519 public key.
func NewIdentity(pub [PubKeySize]byte) Identity {
	return Identity{pubKey: pub}
}

// NewIdentityFromBytes creates an Identity from a 32-byte public key slice.
func NewIdentityFromBytes(pub []byte) (Identity, error) {
	if len(pub) != PubKeySize {
		return Identity{}, fmt.Errorf("public key must be %d bytes, got %d", PubKeySize, len(pub))
	}
	var id Identity
	copy(id.pubKey[:], pub)
	return id, nil
}

// NewIdentityFromHex creates an Identity from a hex-encoded public key string.
func NewIdentityFromHex(hexStr string) (Identity, error) {
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return Identity{}, fmt.Errorf("decoding hex: %w", err)
	}
	return NewIdentityFromBytes(b)
}

func (id Identity) PublicKey() [PubKeySize]byte {
	return id.pubKey
}

func (id Identity) PublicKeyBytes() []byte {
	b := make([]byte, PubKeySize)
	copy(b, id.pubKey[:])
	return b
}

// Prefix returns the first 6 bytes of the public key, the prefix form used by companion frames.
func (id Identity) Prefix() [6]byte {
	var p [6]byte
	copy(p[:], id.pubKey[:6])
	return p
}

// Hash returns the first PathHashSize bytes of the public key.
func (id Identity) Hash() []byte {
	h := make([]byte, PathHashSize)
	copy(h, id.pubKey[:PathHashSize])
	return h
}

// IsHashMatch reports whether hash matches the leading bytes of this identity's public key.
func (id Identity) IsHashMatch(hash []byte) bool {
	if len(hash) == 0 || len(hash) > PubKeySize {
		return false
	}
	for i := range hash {
		if id.pubKey[i] != hash[i] {
			return false
		}
	}
	return true
}

func (id Identity) Matches(other Identity) bool {
	return id.pubKey == other.pubKey
}

// Verify reports whether sig is a valid Ed25519 signature of message by this identity.
func (id Identity) Verify(message, sig []byte) bool {
	return ed25519.Verify(id.pubKey[:], message, sig)
}

// X25519PublicKey converts the Ed25519 public key to an X25519 public key.
func (id Identity) X25519PublicKey() ([]byte, error) {
	return edPublicToX25519(id.pubKey[:])
}

func (id Identity) String() string {
	return hex.EncodeToString(id.pubKey[:])
}

func (id Identity) IsZero() bool {
	return id.pubKey == [PubKeySize]byte{}
}

// LocalIdentity is the local node's full identity, adding signing and
// shared-secret derivation to Identity.
type LocalIdentity struct {
	Identity
	seed [ed25519.SeedSize]byte // zero when expanded is set

	expanded []byte // 64-byte expanded key (clamped scalar ‖ prefix); nil for seed-based identities
}

// GenerateLocalIdentity creates a random LocalIdentity from the given randomness source.
func GenerateLocalIdentity(rand io.Reader) (LocalIdentity, error) {
	pub, priv, err := ed25519.GenerateKey(rand)
	if err != nil {
		return LocalIdentity{}, fmt.Errorf("generating keypair: %w", err)
	}
	return localIdentityFromStdKey(pub, priv), nil
}

// NewLocalIdentityFromSeed creates a deterministic LocalIdentity from a 32-byte Ed25519 seed.
func NewLocalIdentityFromSeed(seed [ed25519.SeedSize]byte) LocalIdentity {
	priv := ed25519.NewKeyFromSeed(seed[:])
	pub := priv.Public().(ed25519.PublicKey)
	return localIdentityFromStdKey(pub, priv)
}

// NewLocalIdentityFromPrivateKey creates a LocalIdentity from a 64-byte Ed25519 private key.
func NewLocalIdentityFromPrivateKey(privKey ed25519.PrivateKey) (LocalIdentity, error) {
	if len(privKey) != ed25519.PrivateKeySize {
		return LocalIdentity{}, fmt.Errorf("private key must be %d bytes, got %d", ed25519.PrivateKeySize, len(privKey))
	}
	pub := privKey.Public().(ed25519.PublicKey)
	return localIdentityFromStdKey(pub, privKey), nil
}

// NewLocalIdentityFromExpandedKey creates a LocalIdentity from a 64-byte
// expanded private key (clamped scalar ‖ prefix), the prv.key format.
func NewLocalIdentityFromExpandedKey(prv []byte) (LocalIdentity, error) {
	if len(prv) != ed25519.PrivateKeySize {
		return LocalIdentity{}, fmt.Errorf("expanded private key must be %d bytes, got %d", ed25519.PrivateKeySize, len(prv))
	}
	var li LocalIdentity
	copy(li.pubKey[:], deriveExpandedPubKey(prv[:32]))
	li.expanded = bytes.Clone(prv)
	return li, nil
}

func localIdentityFromStdKey(pub ed25519.PublicKey, priv ed25519.PrivateKey) LocalIdentity {
	var li LocalIdentity
	copy(li.pubKey[:], pub)
	copy(li.seed[:], priv.Seed())
	return li
}

func (li LocalIdentity) Sign(message []byte) []byte {
	if li.expanded != nil {
		return signWithExpandedKey(li.expanded[:32], li.expanded[32:], li.pubKey[:], message)
	}
	return ed25519.Sign(ed25519.NewKeyFromSeed(li.seed[:]), message)
}

// PrivateKey returns the 64-byte Ed25519 private key, or nil for an expanded-key identity.
func (li LocalIdentity) PrivateKey() ed25519.PrivateKey {
	if li.expanded != nil {
		return nil
	}
	return ed25519.NewKeyFromSeed(li.seed[:])
}

// Seed returns the Ed25519 seed; ok is false for an expanded-key identity, which has none.
func (li LocalIdentity) Seed() (seed [ed25519.SeedSize]byte, ok bool) {
	return li.seed, li.expanded == nil
}

// SharedSecret computes an X25519 shared secret between this local identity and a peer.
func (li LocalIdentity) SharedSecret(peer Identity) ([]byte, error) {
	if li.expanded != nil {
		return sharedSecretFromScalar(li.expanded[:32], peer.pubKey[:])
	}
	return DeriveSharedSecret(li.seed[:], peer.pubKey[:])
}

// X25519PrivateKey converts the Ed25519 seed to a clamped X25519 private key.
func (li LocalIdentity) X25519PrivateKey() []byte {
	if li.expanded != nil {
		return bytes.Clone(li.expanded[:32])
	}
	return edPrivateToX25519(li.seed[:])
}
