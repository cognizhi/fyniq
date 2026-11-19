package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The super-admin console's two reads (FR-ADM-07; docs/design/screens SuperAdminConsole): the platform's health across every
// organisation, and the installation's Super admins. For a Super admin only; counts, times and names, no organisation's
// record and no secret.

// tickLate is how long after its last beat the Scheduler's tick, which runs every 5 minutes, counts as late (§12.11).
const tickLate = 15 * time.Minute

// PlatformJob is a platform job's latest beat.
type PlatformJob struct {
	Name     string    `json:"name" doc:"The job: tick is the Scheduler's, audit.anchor and audit.verify the audit trail's, and so on." example:"tick"`
	LastBeat time.Time `json:"last_beat" doc:"When it last ran." example:"2026-11-20T09:25:00Z"`
	Detail   string    `json:"detail,omitempty" doc:"What it reported, shortened." example:"ok"`
}

// PlatformOutbox is the background work's backlog across organisations.
type PlatformOutbox struct {
	Pending       int64      `json:"pending" doc:"Work waiting or running." example:"3"`
	Dead          int64      `json:"dead" doc:"Work that failed its attempts and needs someone." example:"0"`
	OldestPending *time.Time `json:"oldest_pending,omitempty" doc:"Since when the oldest waiting work has waited." example:"2026-11-20T09:20:00Z"`
}

// PlatformHealth is the platform's health.
type PlatformHealth struct {
	Status  string         `json:"status" enum:"ok,degraded" doc:"degraded when the Scheduler's tick is late or has never run, or work is dead (§12.11)." example:"ok"`
	Reasons []string       `json:"reasons" nullable:"false" doc:"Why it is degraded, in words; empty when ok." example:"[\"The Scheduler's tick last ran 40 minutes ago.\"]"`
	Jobs    []PlatformJob  `json:"jobs" nullable:"false" doc:"The platform jobs' latest beats, by name."`
	Outbox  PlatformOutbox `json:"outbox" doc:"The background work's backlog."`
	AsAt    time.Time      `json:"as_at" doc:"When it was read." example:"2026-11-20T09:30:00Z"`
}

// TransformSchema implements huma.SchemaTransformer: an example for each reason (NFR-API-01).
func (PlatformHealth) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"reasons": "The Scheduler's tick last ran 40 minutes ago."})
	return s
}

// SuperAdminEntry is an active Super admin.
type SuperAdminEntry struct {
	UserID      string    `json:"user_id" format:"uuid" doc:"Their user." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	DisplayName string    `json:"display_name" doc:"Their name." example:"Wei Ling Tan"`
	AddedAt     time.Time `json:"added_at" doc:"Since when they have been a Super admin." example:"2026-10-01T09:00:00Z"`
}

// SuperAdminList is the installation's Super admins.
type SuperAdminList struct {
	Items   []SuperAdminEntry `json:"items" nullable:"false" doc:"The active Super admins, longest-serving first (at most two in P0: the last is only replaced, FR-ADM-07)."`
	Warning string            `json:"warning,omitempty" doc:"Set while there is only one: a second keeps the installation recoverable (CTL-IAM-07)." example:"Only one Super admin: invite a second."`
}

type platformHealthOutput struct{ Body PlatformHealth }
type superAdminListOutput struct{ Body SuperAdminList }

func registerInstallationConsole(a *API) {
	admin := append([]string{CodeNotSuperAdmin, CodeStepUpRequired, CodeSessionRequired, CodeUnavailable}, storage.CodeRetry, storage.CodeLockTimeout)

	register(a, authz.GetPlatformHealth, huma.Operation{
		OperationID: "getPlatformHealth", Method: http.MethodGet, Path: "/v1/installation/health", Tags: []string{"Installation"},
		Summary: "Read the platform's health",
		Description: "The platform's health across every organisation (FR-ADM-07): the platform jobs' latest beats, the Scheduler's tick among " +
			"them, and the background work's backlog; degraded when the tick is late or has never run, or work is dead. " + authz.GetPlatformHealth.Note +
			" Read-only; no side effects.",
	}, admin, func(ctx context.Context, _ *struct{}) (*platformHealthOutput, error) {
		by, err := a.superAdmin(ctx, "Reading the platform's health")
		if err != nil {
			return nil, err
		}
		var h storage.PlatformHealth
		if err := a.inInstallation(ctx, by, func(ctx context.Context, _ uuid.UUID, u storage.UnitOfWork) (err error) {
			h, err = u.SuperAdmins().Health(ctx)
			return err
		}); err != nil {
			return nil, err
		}
		return &platformHealthOutput{Body: platformHealthOf(h, a.deps.Clock.Now())}, nil
	})

	register(a, authz.ListSuperAdmins, huma.Operation{
		OperationID: "listSuperAdmins", Method: http.MethodGet, Path: "/v1/installation/super-admins", Tags: []string{"Installation"},
		Summary: "List the Super admins",
		Description: "The installation's active Super admins, longest-serving first (FR-ADM-07), with a warning while there is only one. " +
			authz.ListSuperAdmins.Note + " Read-only; no side effects.",
	}, admin, func(ctx context.Context, _ *struct{}) (*superAdminListOutput, error) {
		by, err := a.superAdmin(ctx, "Listing the Super admins")
		if err != nil {
			return nil, err
		}
		out := SuperAdminList{Items: []SuperAdminEntry{}}
		if err := a.inInstallation(ctx, by, func(ctx context.Context, _ uuid.UUID, u storage.UnitOfWork) error {
			all, err := u.SuperAdmins().List(ctx)
			for _, s := range all {
				out.Items = append(out.Items, SuperAdminEntry{UserID: s.UserID.String(), DisplayName: s.DisplayName, AddedAt: s.AddedAt.UTC()})
			}
			return err
		}); err != nil {
			return nil, err
		}
		if len(out.Items) == 1 {
			out.Warning = "Only one Super admin: invite a second."
		}
		return &superAdminListOutput{Body: out}, nil
	})
}

// platformHealthOf shapes the health and says whether it is degraded and why.
func platformHealthOf(h storage.PlatformHealth, now time.Time) PlatformHealth {
	out := PlatformHealth{Status: "ok", Reasons: []string{}, Jobs: []PlatformJob{}, AsAt: now.UTC(),
		Outbox: PlatformOutbox{Pending: h.OutboxPending, Dead: h.OutboxDead, OldestPending: h.OldestPending}}
	ticked := false
	for _, b := range h.Heartbeats {
		out.Jobs = append(out.Jobs, PlatformJob{Name: b.Name, LastBeat: b.LastBeat.UTC(), Detail: b.Detail})
		if b.Name == "tick" {
			ticked = true
			if now.Sub(b.LastBeat) > tickLate {
				out.Reasons = append(out.Reasons, "The Scheduler's tick last ran "+ageWords(now.Sub(b.LastBeat))+" ago.")
			}
		}
	}
	if !ticked {
		out.Reasons = append(out.Reasons, "The Scheduler's tick has never run.")
	}
	if h.OutboxDead > 0 {
		out.Reasons = append(out.Reasons, "Background work has failed its attempts and needs someone.")
	}
	if len(out.Reasons) > 0 {
		out.Status = "degraded"
	}
	slices.SortFunc(out.Jobs, func(x, y PlatformJob) int { return cmpStrings(x.Name, y.Name) })
	return out
}

func cmpStrings(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// ageWords says a duration in words: "40 minutes", "3 hours".
func ageWords(d time.Duration) string {
	switch {
	case d < time.Hour:
		return pluralWords(int(d/time.Minute), "minute")
	case d < 48*time.Hour:
		return pluralWords(int(d/time.Hour), "hour")
	}
	return pluralWords(int(d/(24*time.Hour)), "day")
}

func pluralWords(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}
