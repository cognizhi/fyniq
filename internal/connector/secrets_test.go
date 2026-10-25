package connector_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
)

type countingStore struct {
	ports.SecretStore
	values map[ports.SecretRef]string
	reads  int
}

func (s *countingStore) Get(_ context.Context, ref ports.SecretRef) (string, error) {
	s.reads++
	v, ok := s.values[ref]
	if !ok {
		return "", ports.ErrNotFound
	}
	return v, nil
}

// A version is read once per SecretTTL; a new reference is read at once; an error is never kept; Forget drops one.
// req: FR-INT-20
func TestSecretCacheReadsEachVersionOncePerTTL(t *testing.T) {
	start := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	c := clock.NewFrozen(start)
	s := &countingStore{values: map[ports.SecretRef]string{"v1": "one", "v2": "two"}}
	cache := connector.NewSecretCache(s, c)
	ctx := context.Background()
	for range 3 {
		if v, err := cache.Get(ctx, "v1"); err != nil || v != "one" {
			t.Fatalf("%q %v", v, err)
		}
	}
	if s.reads != 1 {
		t.Fatalf("%d reads within the TTL", s.reads)
	}
	if v, _ := cache.Get(ctx, "v2"); v != "two" || s.reads != 2 {
		t.Fatalf("a new reference: %q after %d reads", v, s.reads)
	}
	c.Set(start.Add(connector.SecretTTL))
	if _, _ = cache.Get(ctx, "v1"); s.reads != 3 {
		t.Fatalf("after the TTL: %d reads", s.reads)
	}
	for range 2 {
		if _, err := cache.Get(ctx, "gone"); !errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("%v", err)
		}
	}
	if s.reads != 5 {
		t.Fatalf("errors were kept: %d reads", s.reads)
	}
	cache.Forget("v1")
	if _, _ = cache.Get(ctx, "v1"); s.reads != 6 {
		t.Fatalf("after Forget: %d reads", s.reads)
	}
}
