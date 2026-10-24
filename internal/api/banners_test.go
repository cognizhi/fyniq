//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/setupcode"
)

// The Owner or Administrator publishes banners to the organisation, or to some roles, now or later; a critical banner
// is never dismissible and a link is a path in Fyniq or an https address. Each person sees the live banners meant for
// them, critical first, and dismisses the dismissible ones for themselves; a withdrawn banner ends, once. Publishing and
// withdrawing are audited.
// req: FR-NTF-03
func TestBannersAndAnnouncements(t *testing.T) {
	f := newFounders(t)
	e := f.e
	owner := f.as(f.weiling)
	now := e.clock.Now()
	at := func(d time.Duration) string { return now.Add(d).UTC().Format(time.RFC3339) }
	publish := func(body map[string]any) response {
		return e.call(http.MethodPost, "/v1/announcements", owner, body, "Idempotency-Key", uuid.NewString())
	}

	info := publish(map[string]any{"message": "The office is closed on 3 November.", "link": "/help/organisation-settings", "severity": "info",
		"ends_at": at(48 * time.Hour)})
	if info.Status != http.StatusCreated || info.str("status") != "live" || info.Body["dismissible"] != true {
		t.Fatalf("publishing: %d %s", info.Status, info.Raw)
	}
	for _, bad := range []map[string]any{
		{"message": "x", "severity": "critical", "dismissible": true, "ends_at": at(time.Hour)},
		{"message": "x", "severity": "info", "link": "javascript:alert(1)", "ends_at": at(time.Hour)},
		{"message": "x", "severity": "info", "roles": []string{"ai"}, "ends_at": at(time.Hour)},
		{"message": "x", "severity": "info", "starts_at": at(-time.Hour), "ends_at": at(time.Hour)},
		{"message": "x", "severity": "info", "starts_at": at(2 * time.Hour), "ends_at": at(time.Hour)},
		{"message": "   ", "severity": "info", "ends_at": at(time.Hour)},
	} {
		if r := publish(bad); r.Status != http.StatusUnprocessableEntity {
			t.Errorf("%v: %d %s", bad, r.Status, r.Raw)
		}
	}
	if r := e.call(http.MethodPost, "/v1/announcements", owner, map[string]any{"message": "A preview", "severity": "info", "ends_at": at(time.Hour)},
		"Prefer", "return=preview"); r.Status != http.StatusOK || r.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("a preview: %d %s", r.Status, r.Raw)
	}
	critical := publish(map[string]any{"message": "The bank feed is down: record payments by hand.", "severity": "critical", "roles": []string{"owner"},
		"ends_at": at(24 * time.Hour)})
	later := publish(map[string]any{"message": "Year-end close starts next week.", "severity": "warning", "starts_at": at(24 * time.Hour),
		"ends_at": at(72 * time.Hour), "link": "https://status.example.com/close"})
	if critical.Body["dismissible"] != false || later.str("status") != "scheduled" {
		t.Fatalf("the critical and the scheduled: %s %s", critical.Raw, later.Raw)
	}

	// The Owner sees the critical one first; a Tax agent only the one for everyone; nobody sees the scheduled one yet.
	b := e.call(http.MethodGet, "/v1/banners", owner, nil)
	items, _ := b.Body["items"].([]any)
	if b.Status != 200 || len(items) != 2 || items[0].(map[string]any)["id"] != critical.str("id") || items[1].(map[string]any)["id"] != info.str("id") ||
		items[1].(map[string]any)["scope"] != "organisation" {
		t.Fatalf("the Owner's banners: %d %s", b.Status, b.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/banners", e.as(authz.TaxAgent), nil); len(r.Body["items"].([]any)) != 1 {
		t.Fatalf("a Tax agent's banners: %s", r.Raw)
	}

	// Dismissing: the info for the Owner alone, never the critical one, nothing that is not showing.
	if r := e.call(http.MethodPost, "/v1/banners/"+critical.str("id")+"/dismiss", owner, nil); r.problemCode() != CodeBannerStays {
		t.Fatalf("dismissing the critical banner: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/banners/"+later.str("id")+"/dismiss", owner, nil); r.problemCode() != CodeBannerNone {
		t.Fatalf("dismissing a scheduled banner: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/banners/"+info.str("id")+"/dismiss", owner, nil, "Idempotency-Key", uuid.NewString()); r.Status != 200 ||
		len(r.Body["items"].([]any)) != 1 {
		t.Fatalf("dismissing the info banner: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/banners", owner, nil); strings.Contains(r.Raw, info.str("id")) {
		t.Fatalf("a dismissed banner still shows: %s", r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/banners", f.as(f.ahkow), nil); !strings.Contains(r.Raw, info.str("id")) {
		t.Fatalf("the Owner's dismissal hid it from Ah Kow: %s", r.Raw)
	}

	// Withdrawing ends it, once; the list shows each with its status.
	w := e.call(http.MethodPost, "/v1/announcements/"+critical.str("id")+"/withdraw", owner, nil, "Idempotency-Key", uuid.NewString())
	if w.Status != 200 || w.str("status") != "ended" || w.Body["withdrawn_at"] == nil {
		t.Fatalf("withdrawing: %d %s", w.Status, w.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/announcements/"+critical.str("id")+"/withdraw", owner, nil); r.problemCode() != CodeBannerEnded {
		t.Fatalf("withdrawing twice: %d %s", r.Status, r.Raw)
	}
	l := e.call(http.MethodGet, "/v1/announcements", e.as(authz.Auditor), nil)
	if l.Status != 200 || len(l.Body["items"].([]any)) != 3 || !strings.Contains(l.Raw, `"status":"scheduled"`) || !strings.Contains(l.Raw, `"status":"ended"`) {
		t.Fatalf("the announcements: %d %s", l.Status, l.Raw)
	}
	trail := auditTrail(t, e)
	if strings.Count(trail, "announcement.published") != 3 || !strings.Contains(trail, "announcement.withdrawn") {
		t.Errorf("the trail has %d publications", strings.Count(trail, "announcement.published"))
	}
}

// A Super admin publishes a banner to everyone: it shows to every member of every organisation, as the installation's,
// and its publishing and withdrawal are in the installation's trail; nobody else publishes one.
// req: FR-NTF-03
// req: FR-ADM-07
func TestAnnouncementsToEveryone(t *testing.T) {
	ctx := context.Background()
	s := startSessions(t)
	desk := s.browser()
	desk.signIn("founder-a", true)
	code, hash := setupcode.New()
	if err := postgres.SetSetupCode(ctx, s.migrator, hash); err != nil {
		t.Fatal(err)
	}
	if r := desk.call(http.MethodPost, "/v1/setup", "same-origin", map[string]any{"setup_code": code, "display_name": "Mei Ling Tan",
		"organisation": map[string]any{"name": "Cognizhi Pte. Ltd.", "base_currency": "SGD"}}); r.Status != http.StatusCreated {
		t.Fatalf("setup: %d %s", r.Status, r.Raw)
	}
	body := map[string]any{"message": "Fyniq is down for maintenance on Sunday from 02:00 to 04:00.", "severity": "warning",
		"link": "https://status.example.com", "ends_at": s.clock.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)}
	p := desk.call(http.MethodPost, "/v1/installation/announcements", "same-origin", body)
	if p.Status != http.StatusCreated || p.str("status") != "live" || !strings.HasPrefix(p.str("published_by"), "user:") {
		t.Fatalf("publishing to everyone: %d %s", p.Status, p.Raw)
	}
	b := desk.call(http.MethodGet, "/v1/banners", "same-origin", nil)
	if items, _ := b.Body["items"].([]any); len(items) != 1 || items[0].(map[string]any)["scope"] != "installation" {
		t.Fatalf("the banners: %d %s", b.Status, b.Raw)
	}
	if r := desk.call(http.MethodPost, "/v1/banners/"+p.str("id")+"/dismiss", "same-origin", nil); r.Status != 200 {
		t.Fatalf("dismissing the installation's banner: %d %s", r.Status, r.Raw)
	}
	if l := desk.call(http.MethodGet, "/v1/installation/announcements", "same-origin", nil); len(l.Body["items"].([]any)) != 1 {
		t.Fatalf("the installation's announcements: %s", l.Raw)
	}
	if w := desk.call(http.MethodPost, "/v1/installation/announcements/"+p.str("id")+"/withdraw", "same-origin", nil); w.Status != 200 || w.str("status") != "ended" {
		t.Fatalf("withdrawing: %d %s", w.Status, w.Raw)
	}
	other := s.browser()
	other.signIn("someone-else", true)
	s.problemIs(other.call(http.MethodPost, "/v1/installation/announcements", "same-origin", body), "publishInstallationAnnouncement", 403, CodeNotSuperAdmin)
	trail := s.installationTrail(t)
	for _, want := range []string{"installation_announcement.published", "installation_announcement.withdrawn"} {
		if !strings.Contains(trail, want) {
			t.Errorf("the installation's trail lacks %s", want)
		}
	}
}

// matrixBanner publishes a live, dismissible banner to the matrix organisation and returns its ID.
func matrixBanner(e *env) string {
	var id uuid.UUID
	fixture := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "matrix-fixture"}})
	if err := e.store.Do(fixture, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		now := e.clock.Now()
		a, err := u.Announcements().Publish(ctx, storage.NewAnnouncement{Message: "Matrix", Severity: "info", StartsAt: now, EndsAt: now.Add(time.Hour),
			Dismissible: true, At: now})
		id = a.ID
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return id.String()
}

// matrixAnnouncements counts the matrix organisation's announcements.
func matrixAnnouncements(e *env) int {
	var n int
	_ = e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Announcements().List(ctx)
		n = len(all)
		return err
	})
	return n
}
