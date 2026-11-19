//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Every role that lists the organisation's members (the API, the worker that fans a notification out, the executor and the
// jobs) can: the list reads a function for each of its columns, and a function granted to one role alone broke the fan-out
// (a regression of 000704).
// req: FR-ADM-01
// asvs: V8.2.1
func TestEveryRoleThatListsMembersMayReadTheirActivity(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Member list roles Pte Ltd")
	now := clock.System{}.Now()
	user := founder(t, store(t, "fyniq_api"), b.Org, 91, now)
	for _, role := range []string{"fyniq_api", "fyniq_worker", "fyniq_executor"} {
		if err := store(t, role).Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			ms, err := u.Members().List(ctx, now)
			if err != nil {
				return err
			}
			found := false
			for _, m := range ms {
				found = found || m.UserID == user
			}
			if !found {
				t.Errorf("%s does not list the member", role)
			}
			return nil
		}); err != nil {
			t.Errorf("%s lists the members: %v", role, err)
		}
	}
}
