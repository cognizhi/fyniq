package connector

import (
	"context"
	"sync"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// SecretTTL is how long a secret version's value is kept in memory. A reference names one version, which never
// changes, so a cached value is never another key: the cache can only miss that a version was disabled or destroyed
// within the last SecretTTL, and replacing a secret changes the reference at once.
const SecretTTL = 5 * time.Minute

// SecretCache reads secret versions through a store, each at most once per SecretTTL, so a send does not call Secret
// Manager every time. Errors are never kept.
type SecretCache struct {
	store ports.SecretStore
	clock ports.Clock

	mu     sync.Mutex
	values map[ports.SecretRef]cached
}

type cached struct {
	value string
	until time.Time
}

// NewSecretCache reads through store.
func NewSecretCache(store ports.SecretStore, clock ports.Clock) *SecretCache {
	return &SecretCache{store: store, clock: clock, values: map[ports.SecretRef]cached{}}
}

// Get returns the value ref names.
func (c *SecretCache) Get(ctx context.Context, ref ports.SecretRef) (string, error) {
	now := c.clock.Now()
	c.mu.Lock()
	if v, ok := c.values[ref]; ok && now.Before(v.until) {
		c.mu.Unlock()
		return v.value, nil
	}
	c.mu.Unlock()
	value, err := c.store.Get(ctx, ref)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for r, v := range c.values { // drop what has expired, so the map holds only what is in use
		if !now.Before(v.until) {
			delete(c.values, r)
		}
	}
	c.values[ref] = cached{value: value, until: now.Add(SecretTTL)}
	return value, nil
}

// Forget drops ref, as this instance does when it disables or destroys it.
func (c *SecretCache) Forget(ref ports.SecretRef) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.values, ref)
}
