//go:build local

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/connectors"
	"github.com/cognizhi/fyniq/internal/chatroute"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

type chatPoster struct {
	posted []ports.ChatMessage
	err    error
}

func (p *chatPoster) Post(_ context.Context, m ports.ChatMessage) (ports.ChatReceipt, error) {
	if p.err != nil {
		return ports.ChatReceipt{}, p.err
	}
	p.posted = append(p.posted, m)
	return ports.ChatReceipt{ID: "1"}, nil
}

// An organisation's chat rules send the events they name to the channels they name, and nothing else (FR-NTF-04; p2-api-third-parties 7.2): an event of a type with a rule is queued
// once for each rule, with the type's label and a deep link and none of the event's own words; a type with no rule, a security or personal-data type, a connector that is not
// enabled, another organisation's connector and a sandbox organisation get nothing; the worker posts through the connector's adapter outside any unit of work, holds a message for a
// provider's wait and for a connector that is not enabled, and fails a refusal so the row is retried and then dead.
// req: FR-NTF-04
// req: FR-NTF-05
// asvs: V4.2.1, V8.2.1
func TestChatRoutingRulesSendOnlyTheEventsTheyNameToTheirChannels(t *testing.T) {
	ctx := context.Background()
	api := store(t, "fyniq_api")
	books := porttest.NewBooks(t, api, "Chat Pte Ltd")
	elsewhere := porttest.NewBooks(t, api, "Chat elsewhere Pte Ltd")
	creator := founder(t, api, books.Org, 81, clock.System{}.Now().UTC().Truncate(time.Microsecond))
	sandbox := placeSandbox(t, creator)
	at := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	cat, err := connectors.Catalogue()
	if err != nil {
		t.Fatal(err)
	}
	slack, ok := cat.Get("slack")
	if !ok {
		t.Fatal("no slack connector")
	}
	routes := []any{map[string]any{"event_type": "fyniq.approval.requested.v1", "channel": "#finance"}, map[string]any{"event_type": "fyniq.approval.requested.v1", "channel": "#ceo"},
		map[string]any{"event_type": "fyniq.security.alert.v1", "channel": "#security"}, map[string]any{"event_type": "fyniq.period.closed.v1", "channel": "#finance"}}
	install := func(org uuid.UUID, enable bool) uuid.UUID {
		var id uuid.UUID
		if err := api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := u.Connectors().InstallOrganisation(ctx, storage.NewConnector{Connector: "slack", ManifestVersion: slack.Manifest.Version, ManifestHash: slack.Manifest.Hash(), SecretNames: []string{"bot_token"},
				Consent: slack.Manifest.Consent(), Position: 1, At: at})
			if err != nil {
				return err
			}
			id = c.ID
			if !enable {
				return nil
			}
			cfg := map[string]any{"content_level": "minimal", "routes": routes}
			for _, step := range []struct{ s, state string }{{storage.StepConfigure, storage.ConnectorConfigured}, {storage.StepTest, storage.ConnectorTested}, {storage.StepEnable, storage.ConnectorEnabled}} {
				if step.s == storage.StepTest {
					c.Test = &storage.ConnectorTest{Passed: true, Reason: "ok", At: at}
				}
				if c, err = u.Connectors().ChangeOrganisation(ctx, c.ID, c.Version, storage.ConnectorChange{Step: step.s, State: step.state, Configuration: cfg, Position: 1, Test: c.Test,
					ManifestVersion: slack.Manifest.Version, ManifestHash: slack.Manifest.Hash(), SecretNames: []string{"bot_token"}, Consent: slack.Manifest.Consent(), At: at}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	mine := install(books.Org, true)
	_ = install(elsewhere.Org, true)
	idleOrg := porttest.NewBooks(t, api, "Chat idle Pte Ltd")
	_ = install(idleOrg.Org, false)

	h := newHarness(t, map[string]outbox.Work{chatroute.Kind: func(context.Context, uuid.UUID, storage.OutboxRow) error { return nil }})
	q := chatroute.Queuer{Catalogue: cat, Dispatcher: h.disp, Origin: "https://app.fyniq.example/"}
	queue := func(org uuid.UUID, typ string) []storage.OutboxRow {
		t.Helper()
		before := countKind(t, org, chatroute.Kind)
		if err := api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			return q.Queue(ctx, u, org, notify.ChatEvent{ID: uuid.New(), Type: typ, Label: "An approval waits for you", Link: "/inbox"})
		}); err != nil {
			t.Fatal(err)
		}
		h.push.Wait()
		return rowsOfKind(t, org, chatroute.Kind)[before:]
	}
	rows := queue(books.Org, "approval.requested")
	if len(rows) != 2 {
		t.Fatalf("an event with two rules queued %d messages", len(rows))
	}
	for _, r := range rows {
		s := string(r.Payload)
		if !strings.Contains(s, `"An approval waits for you: https://app.fyniq.example/inbox"`) || !strings.Contains(s, mine.String()) {
			t.Errorf("a queued message: %s", s)
		}
	}
	for name, got := range map[string][]storage.OutboxRow{
		"a type with no rule":             queue(books.Org, "bills.due"),
		"a security type with a rule":     queue(books.Org, "security.alert"),
		"another organisation's rule":     nil,
		"a connector that is not enabled": queue(idleOrg.Org, "approval.requested"),
		"a sandbox organisation":          queue(sandbox, "approval.requested"),
	} {
		if len(got) != 0 {
			t.Errorf("%s queued %d messages", name, len(got))
		}
	}
	if n := countKind(t, elsewhere.Org, chatroute.Kind); n != 0 {
		t.Errorf("a queue for one organisation reached another's: %d", n)
	}

	// The worker posts outside a unit of work, holds, and fails a refusal.
	post := &chatPoster{}
	var built atomic.Int32
	m := chatroute.Maker{Store: store(t, "fyniq_worker"), Clock: clock.System{}, Messaging: func(context.Context, storage.UnitOfWork, uuid.UUID) (ports.Messaging, error) {
		built.Add(1)
		return post, nil
	}}
	work := m.Work()
	if err := work(ctx, books.Org, rows[0]); err != nil || len(post.posted) != 1 || post.posted[0].Channel == "" {
		t.Fatalf("a post: %v %+v", err, post.posted)
	}
	post.err = ports.ChatLimited{After: 7 * time.Second}
	var held outbox.Held
	if err := work(ctx, books.Org, rows[0]); !errors.As(err, &held) || held.For != 7*time.Second {
		t.Errorf("a provider's wait: %v", err)
	}
	post.err = ports.ErrChatRefused
	if err := work(ctx, books.Org, rows[0]); !errors.Is(err, ports.ErrChatRefused) {
		t.Errorf("a refusal: %v", err)
	}
	m.Messaging = func(context.Context, storage.UnitOfWork, uuid.UUID) (ports.Messaging, error) {
		return nil, fmt.Errorf("%w: slack is paused", connector.ErrNotEnabled)
	}
	if err := m.Work()(ctx, books.Org, rows[0]); !errors.Is(err, outbox.ErrHeld) {
		t.Errorf("a connector that is not enabled: %v", err)
	}
	if err := work(ctx, books.Org, storage.OutboxRow{Payload: []byte(`{"connector_id":"` + uuid.Nil.String() + `"}`)}); err == nil {
		t.Error("a row that is not a chat message was worked")
	}
}

// rowsOfKind are an organisation's outbox rows of a kind, oldest first, read as the migrator within the organisation's own setting.
func rowsOfKind(t *testing.T, org uuid.UUID, kind string) []storage.OutboxRow {
	t.Helper()
	var out []storage.OutboxRow
	if err := asOrg(context.Background(), conn(t, "fyniq"), org, func(tx pgx.Tx) error {
		rs, err := tx.Query(context.Background(), `SELECT id, payload::text FROM fyniq.outbox WHERE organisation_id = $1 AND kind = $2 ORDER BY id`, org, kind)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var r storage.OutboxRow
			var p string
			if err := rs.Scan(&r.ID, &p); err != nil {
				return err
			}
			r.Payload = []byte(p)
			out = append(out, r)
		}
		return rs.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func countKind(t *testing.T, org uuid.UUID, kind string) int { return len(rowsOfKind(t, org, kind)) }
