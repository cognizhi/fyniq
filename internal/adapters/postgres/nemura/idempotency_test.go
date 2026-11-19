//go:build local

package nemura_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/nemura/api"
)

// The table of idempotency keys: of many identical claims at once exactly one wins; a repeat after the answer returns it; the same key with another request hash is a mismatch; a key is a person's own and an
// organisation's own; a key expires after 24 hours and a stale in-flight claim is cleared; a released key may be tried again; and the table holds no body when the answer is not kept (golden rule 4, NM-ARC-03).
// req: NM-ARC-03
// asvs: V11.1.4, V8.2.1, V8.3.4
func TestTheIdempotencyKeysOfWritesAreClaimedOnceAndKeptForADay(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	other := newCompany(t)
	st := nemura.IdempotencyStore{Store: store(t, "nemura_api")}
	now := d("2026-11-20").Add(10 * time.Hour)
	hash := bytes.Repeat([]byte{1}, 32)
	hash2 := bytes.Repeat([]byte{2}, 32)
	var owned atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := st.Claim(ctx, lt.c.org, lt.c.owner, "race", hash, now)
			if err != nil {
				t.Error(err)
				return
			}
			if c.Owned {
				owned.Add(1)
			} else if !c.InFlight {
				t.Errorf("a loser that is not told an identical request is running: %+v", c)
			}
		}()
	}
	wg.Wait()
	if owned.Load() != 1 {
		t.Fatalf("eight identical claims at once: %d owned", owned.Load())
	}
	if c, _ := st.Claim(ctx, lt.c.org, lt.c.owner, "race", hash2, now); !c.Mismatch {
		t.Errorf("another request on the key: %+v", c)
	}
	if err := st.Finish(ctx, lt.c.org, lt.c.owner, "race", api.IdemRecord{Status: 201, ContentType: "application/json", Body: []byte(`{"id":"x"}`), HadBody: true, BodyKept: true}); err != nil {
		t.Fatal(err)
	}
	c, err := st.Claim(ctx, lt.c.org, lt.c.owner, "race", hash, now.Add(time.Hour))
	if err != nil || c.Owned || c.InFlight || c.Mismatch || c.Rec.Status != 201 || string(c.Rec.Body) != `{"id":"x"}` || !c.Rec.BodyKept {
		t.Errorf("a repeat: %+v %v", c, err)
	}
	// A key is the person's own and the organisation's own.
	if c, _ := st.Claim(ctx, lt.c.org, lt.c.pay, "race", hash, now); !c.Owned {
		t.Errorf("another person's use of the same key: %+v", c)
	}
	if c, _ := st.Claim(ctx, other.org, other.owner, "race", hash, now); !c.Owned {
		t.Errorf("another organisation's use of the same key: %+v", c)
	}
	// Expiry: after 24 hours the key is free again.
	if c, _ := st.Claim(ctx, lt.c.org, lt.c.owner, "race", hash2, now.Add(24*time.Hour+time.Second)); !c.Owned {
		t.Errorf("an expired key: %+v", c)
	}
	// A claim whose request died is cleared after two minutes; a released key may be tried again.
	if c, _ := st.Claim(ctx, lt.c.org, lt.c.owner, "stale", hash, now); !c.Owned {
		t.Fatal("claim")
	}
	if c, _ := st.Claim(ctx, lt.c.org, lt.c.owner, "stale", hash, now.Add(time.Minute)); !c.InFlight {
		t.Errorf("a running request: %+v", c)
	}
	if c, _ := st.Claim(ctx, lt.c.org, lt.c.owner, "stale", hash, now.Add(3*time.Minute)); !c.Owned {
		t.Errorf("a stale claim: %+v", c)
	}
	if err := st.Release(ctx, lt.c.org, lt.c.owner, "stale"); err != nil {
		t.Fatal(err)
	}
	if c, _ := st.Claim(ctx, lt.c.org, lt.c.owner, "stale", hash, now.Add(4*time.Minute)); !c.Owned {
		t.Errorf("a released key: %+v", c)
	}
	// An answer that is not kept holds no body, and the database refuses a body that was not kept.
	if c, _ := st.Claim(ctx, lt.c.org, lt.c.owner, "pay", hash, now); !c.Owned {
		t.Fatal("claim")
	}
	if err := st.Finish(ctx, lt.c.org, lt.c.owner, "pay", api.IdemRecord{Status: 201, ContentType: "application/json", HadBody: true}); err != nil {
		t.Fatal(err)
	}
	su := connect(t, db.SuperURL())
	var body []byte
	var kept bool
	if err := su.QueryRow(ctx, "SELECT body, body_kept FROM nemura.idempotency_key WHERE key = 'pay'").Scan(&body, &kept); err != nil || body != nil || kept {
		t.Errorf("a pay answer: %v %v %v", body, kept, err)
	}
	if _, err := su.Exec(ctx, "UPDATE nemura.idempotency_key SET body = '\\x01' WHERE key = 'pay'"); err == nil || !strings.Contains(err.Error(), "body_only_if_kept") {
		t.Errorf("a body that was not kept: %v", err)
	}
	if _, err := su.Exec(ctx, "UPDATE nemura.idempotency_key SET expires_at = expires_at + interval '1 day' WHERE key = 'pay'"); err == nil || !strings.Contains(err.Error(), "idempotency_key_window") {
		t.Errorf("a window other than 24 hours: %v", err)
	}
}
