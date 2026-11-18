//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The database keeps a setting's value to 1 to 200 characters, but for the two lists, the fields the AI never reads
// and the task types it may propose, which may be empty and run to 4,000 (migration 000373): whatever the API sends,
// another setting's empty or longer value, or a list past 4,000, is refused.
// req: AI-32
// req: AI-02
// req: FR-ORG-08
func TestOnlyTheListSettingsMayBeEmptyOrLong(t *testing.T) {
	api := store(t, "fyniq_api")
	b := porttest.NewBooks(t, api, "Lists Pte Ltd")
	from := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	version := map[string]int{}
	record := func(key, value string) error {
		return api.Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			version[key]++
			_, err := u.Settings().Record(ctx, key, "", value, from, version[key])
			if err != nil {
				version[key]--
			}
			return err
		})
	}
	for _, c := range []struct{ key, value string }{
		{"ai.task_types", "sec.create_task"}, {"ai.task_types", ""}, {"ai.task_types", strings.Repeat("t", 4000)},
		{"ai.exclusions", "notes"}, {"ai.exclusions", ""},
		{"sales.payment_terms_days", strings.Repeat("9", 200)},
	} {
		if err := record(c.key, c.value); err != nil {
			t.Errorf("%s = %d characters: %v", c.key, len(c.value), err)
		}
	}
	for _, c := range []struct{ key, value string }{
		{"ai.task_types", strings.Repeat("t", 4001)}, {"ai.exclusions", strings.Repeat("n", 4001)},
		{"sales.payment_terms_days", ""}, {"sales.payment_terms_days", strings.Repeat("9", 201)},
		{"features.ai_assistant", ""},
	} {
		if err := record(c.key, c.value); storage.CodeOf(err) != storage.CodeInvalid {
			t.Errorf("%s = %d characters: want %s, got %v", c.key, len(c.value), storage.CodeInvalid, err)
		}
	}
}
