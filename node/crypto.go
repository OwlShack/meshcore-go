package node

import (
	"math"
	"sync"
	"sync/atomic"

	meshcore "github.com/OwlShack/meshcore-go"
)

const maxCachedSecrets = 100

type cachedSecret struct {
	secret []byte
	used   atomic.Uint64
}

type secretCache struct {
	mu      sync.RWMutex
	self    meshcore.LocalIdentity
	secrets map[[meshcore.PubKeySize]byte]*cachedSecret
	tick    atomic.Uint64
}

func newSecretCache(self meshcore.LocalIdentity) *secretCache {
	return &secretCache{
		self:    self,
		secrets: make(map[[meshcore.PubKeySize]byte]*cachedSecret),
	}
}

func (sc *secretCache) reset(self meshcore.LocalIdentity) {
	sc.mu.Lock()
	sc.self = self
	sc.secrets = make(map[[meshcore.PubKeySize]byte]*cachedSecret)
	sc.mu.Unlock()
}

func (sc *secretCache) get(peer meshcore.Identity) ([]byte, error) {
	key := peer.PublicKey()

	sc.mu.RLock()
	entry, ok := sc.secrets[key]
	sc.mu.RUnlock()
	if ok {
		entry.used.Store(sc.tick.Add(1))
		out := make([]byte, len(entry.secret))
		copy(out, entry.secret)
		return out, nil
	}

	secret, err := sc.self.SharedSecret(peer)
	if err != nil {
		return nil, err
	}

	entry = &cachedSecret{secret: secret}
	entry.used.Store(sc.tick.Add(1))

	sc.mu.Lock()
	if _, ok := sc.secrets[key]; !ok && len(sc.secrets) >= maxCachedSecrets {
		sc.evictLeastRecentlyUsedLocked()
	}
	sc.secrets[key] = entry
	sc.mu.Unlock()

	out := make([]byte, len(secret))
	copy(out, secret)
	return out, nil
}

func (sc *secretCache) evictLeastRecentlyUsedLocked() {
	var oldestKey [meshcore.PubKeySize]byte
	oldest := uint64(math.MaxUint64)
	for k, e := range sc.secrets {
		if u := e.used.Load(); u < oldest {
			oldest, oldestKey = u, k
		}
	}
	delete(sc.secrets, oldestKey)
}
