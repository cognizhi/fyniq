package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/accessreview"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// DataClassView is a class of personal data Fyniq holds, from its catalogue (CMP-PD-02, rev 0.244).
type DataClassView struct {
	Key        string `json:"key" doc:"The class." example:"members"`
	What       string `json:"what" doc:"What it is." example:"Members' names, sign-in email addresses and roles"`
	Why        string `json:"why" doc:"Why it is held." example:"To let people sign in and do their work"`
	Where      string `json:"where" doc:"Where it is held." example:"The database, in Singapore"`
	Who        string `json:"who" doc:"Who reads it." example:"Appendix D's Users, roles, team mode"`
	Retention  string `json:"retention" doc:"How long it is kept." example:"While a member"`
	Protection string `json:"protection" doc:"How it is protected." example:"Shreddable in the audit trail"`
	Transfers  string `json:"transfers" doc:"Where it goes abroad, if anywhere." example:"None"`
}

// NotHeldView is personal data Fyniq never holds, with why.
type NotHeldView struct {
	Key  string `json:"key" doc:"What is not held." example:"nric"`
	What string `json:"what" doc:"In words." example:"NRIC and FIN numbers"`
	Why  string `json:"why" doc:"Why it is not held." example:"Payroll's live in Nemura"`
}

// InventoryEntryView is the latest version of an entry the organisation added, for processing outside Fyniq.
type InventoryEntryView struct {
	EntryID   string `json:"entry_id" format:"uuid" doc:"The entry." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Version   int    `json:"version" doc:"Its version; the ETag carries it for If-Match." example:"2"`
	What      string `json:"what" doc:"What personal data." example:"Job applicants' CVs"`
	Why       string `json:"why" doc:"Why the organisation holds it." example:"To hire"`
	Where     string `json:"where" doc:"Where it is held." example:"A shared drive in Singapore"`
	Who       string `json:"who" doc:"Who reads it." example:"The founders"`
	Retention string `json:"retention" doc:"How long it is kept." example:"6 months after the role is filled"`
	Transfers string `json:"transfers" doc:"Where it goes abroad, if anywhere." example:"None"`
	NeedsDPIA bool   `json:"needs_dpia" doc:"Whether the processing needs a data protection impact assessment (CMP-PD-08)." example:"false"`
	State     string `json:"state" enum:"open,retired" doc:"open while the processing goes on; retired once it stops." example:"open"`
	SavedBy   string `json:"saved_by" doc:"Who saved this version, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	SavedAt   string `json:"saved_at" format:"date-time" doc:"When." example:"2026-11-09T02:00:00Z"`
}

// InventoryEntryInput is what an entry says.
type InventoryEntryInput struct {
	What      string `json:"what" minLength:"1" maxLength:"500" doc:"What personal data." example:"Job applicants' CVs"`
	Why       string `json:"why" minLength:"1" maxLength:"1000" doc:"Why the organisation holds it." example:"To hire"`
	Where     string `json:"where" minLength:"1" maxLength:"500" doc:"Where it is held." example:"A shared drive in Singapore"`
	Who       string `json:"who" minLength:"1" maxLength:"500" doc:"Who reads it." example:"The founders"`
	Retention string `json:"retention" minLength:"1" maxLength:"500" doc:"How long it is kept." example:"6 months after the role is filled"`
	Transfers string `json:"transfers" minLength:"1" maxLength:"500" doc:"Where it goes abroad, or None." example:"None"`
	NeedsDPIA bool   `json:"needs_dpia" doc:"Whether it needs a data protection impact assessment." example:"false"`
	State     string `json:"state,omitempty" enum:"open,retired" doc:"open, the default, or retired once the processing stops." example:"open"`
}

// RetentionRow is a line of the retention schedule: a kind of data, how long it is kept, and how it is deleted.
type RetentionRow struct {
	Data      string `json:"data" doc:"The data." example:"Security events' IP address and browser"`
	Retention string `json:"retention" doc:"How long it is kept, with the setting that says so." example:"400 days (retention.security_events_days)"`
	Deletion  string `json:"deletion" doc:"How it is deleted." example:"Erased daily, with a signed register entry"`
}

// InventoryReviewState is the inventory's quarterly review (CTL-DP-05).
type InventoryReviewState struct {
	Quarter      string `json:"quarter,omitempty" format:"date" doc:"The quarter waiting for its review: its first day; absent when none waits." example:"2026-07-01"`
	Label        string `json:"label,omitempty" doc:"The quarter, as people say it." example:"Q3 2026"`
	DueBy        string `json:"due_by,omitempty" format:"date" doc:"The last day its review is on time: 14 days after the quarter ends." example:"2026-10-14"`
	Overdue      bool   `json:"overdue" doc:"true once that day has passed." example:"false"`
	LastQuarter  string `json:"last_quarter,omitempty" format:"date" doc:"The latest quarter reviewed." example:"2026-04-01"`
	LastReviewer string `json:"last_reviewed_by,omitempty" doc:"Who reviewed it, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	LastAt       string `json:"last_reviewed_at,omitempty" format:"date-time" doc:"When." example:"2026-07-03T02:00:00Z"`
}

// DataInventory is the organisation's personal-data inventory and retention schedule (CMP-PD-02, CMP-05).
type DataInventory struct {
	Classes  []DataClassView      `json:"classes" nullable:"false" doc:"What Fyniq itself holds, from its catalogue: the same for every organisation."`
	NotHeld  []NotHeldView        `json:"not_held" nullable:"false" doc:"What Fyniq never holds."`
	Entries  []InventoryEntryView `json:"entries" nullable:"false" doc:"What the organisation added, for its processing outside Fyniq, at their latest versions."`
	Schedule []RetentionRow       `json:"schedule" nullable:"false" doc:"The retention schedule, from the settings in force today."`
	Review   InventoryReviewState `json:"review" doc:"The quarterly review."`
}

type (
	dataInventoryOutput struct{ Body DataInventory }
	// InventoryEntryPath names an entry (exported: huma reads only an exported embedded struct).
	InventoryEntryPath struct {
		EntryID string `path:"entry_id" format:"uuid" doc:"The entry." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	}
	inventoryCreateInput struct {
		KeyHeader
		PreferHeader
		Body InventoryEntryInput
	}
	inventoryReviseInput struct {
		InventoryEntryPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body InventoryEntryInput
	}
	inventoryEntryOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The entry's version, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     InventoryEntryView
	}
	inventoryReviewInput struct {
		KeyHeader
		PreferHeader
		Body struct {
			Quarter string `json:"quarter" format:"date" doc:"The quarter reviewed, by its first day, as review.quarter names it." example:"2026-07-01"`
			Note    string `json:"note,omitempty" maxLength:"2000" doc:"What the review found or changed." example:"Added the job applicants' CVs."`
		}
	}
	inventoryReviewOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     InventoryReviewState
	}
)

func inventoryEntryView(e storage.InventoryEntry) InventoryEntryView {
	return InventoryEntryView{EntryID: e.EntryID.String(), Version: e.Version, What: e.What, Why: e.Why, Where: e.Where, Who: e.Who,
		Retention: e.Retention, Transfers: e.Transfers, NeedsDPIA: e.NeedsDPIA, State: e.State, SavedBy: e.SavedBy,
		SavedAt: e.SavedAt.UTC().Format(time.RFC3339)}
}

func inventoryETag(version int) string { return etag("v" + strconv.Itoa(version)) }

// settingOn is a setting's value in force on day, in u.
func settingOn(ctx context.Context, u storage.UnitOfWork, key string, day time.Time) (string, error) {
	def, ok := settings.Lookup(key)
	if !ok {
		return "", fmt.Errorf("no setting %s", key)
	}
	changes, err := u.Settings().Changes(ctx, key)
	if err != nil {
		return "", err
	}
	return settings.On(def, changes, day).Value, nil
}

// retentionSchedule is the schedule of §11.9 with the organisation's settings in force on day.
func retentionSchedule(ctx context.Context, u storage.UnitOfWork, day time.Time) ([]RetentionRow, error) {
	v := map[string]string{}
	for _, k := range []string{"retention.records_years", "retention.security_events_days", "retention.ai_conversations_days", "retention.purge_after_retention"} {
		s, err := settingOn(ctx, u, k, day)
		if err != nil {
			return nil, err
		}
		v[k] = s
	}
	purge := "the ledger and its audit trail are kept for life (retention.purge_after_retention is off)"
	if v["retention.purge_after_retention"] == "true" {
		purge = "the ledger and its audit trail are purged too (retention.purge_after_retention is on)"
	}
	return []RetentionRow{
		{Data: "The books, documents, their files and the audit trail", Retention: v["retention.records_years"] + " years after the end of the financial year they relate to (retention.records_years)",
			Deletion: "Documents and files are purged by the retention job; " + purge},
		{Data: "Security events' IP address and browser", Retention: v["retention.security_events_days"] + " days (retention.security_events_days)",
			Deletion: "Erased daily, each erasure entered in a signed register first"},
		{Data: "The audit trail's personal values", Retention: "The records' retention, as above",
			Deletion: "Shredded per person and financial year once the Compliance officer approves (§11.8)"},
		{Data: "AI conversations", Retention: v["retention.ai_conversations_days"] + " days (retention.ai_conversations_days)",
			Deletion: "Purged once the AI assistant keeps conversations (p1-ai-assistant)"},
		{Data: "Application logs", Retention: "30 days", Deletion: "Deleted by the log store [live-verify]"},
		{Data: "Backups", Retention: "Point-in-time recovery 7 days; daily backups 35 days; monthly exports 1 year",
			Deletion: "Deleted automatically [live-verify]"},
	}, nil
}

// inventoryReviewOf is the inventory's review on today, for an organisation whose earliest member joined at since.
func inventoryReviewOf(reviews []storage.InventoryReview, today, since time.Time) InventoryReviewState {
	var out InventoryReviewState
	var quarters []time.Time
	for _, r := range reviews {
		quarters = append(quarters, r.Quarter)
		out.LastQuarter, out.LastReviewer, out.LastAt = r.Quarter.Format(time.DateOnly), r.ReviewedBy, r.ReviewedAt.UTC().Format(time.RFC3339)
	}
	if q, ok := accessreview.ToAttest(today, since); ok && !accessreview.Covered(q, quarters) {
		out.Quarter, out.Label, out.DueBy = q.Format(time.DateOnly), accessreview.Label(q), accessreview.DueBy(q).Format(time.DateOnly)
		out.Overdue = today.After(accessreview.DueBy(q))
	}
	if q, ok := accessreview.Due(today, since); ok && !accessreview.Covered(q, quarters) {
		out.Quarter, out.Label, out.DueBy, out.Overdue = q.Format(time.DateOnly), accessreview.Label(q), accessreview.DueBy(q).Format(time.DateOnly), true
	}
	return out
}

func (a *API) dataInventory(ctx context.Context, u storage.UnitOfWork) (DataInventory, error) {
	inv, err := compliance.LoadInventory()
	if err != nil {
		return DataInventory{}, err
	}
	out := DataInventory{Classes: []DataClassView{}, NotHeld: []NotHeldView{}, Entries: []InventoryEntryView{}}
	for _, c := range inv.Classes {
		out.Classes = append(out.Classes, DataClassView{Key: c.Key, What: c.What, Why: c.Why, Where: c.Where, Who: c.Who, Retention: c.Retention,
			Protection: c.Protection, Transfers: c.Transfers})
	}
	for _, n := range inv.NotHeld {
		out.NotHeld = append(out.NotHeld, NotHeldView{Key: n.Key, What: n.What, Why: n.Why})
	}
	es, err := u.Compliance().InventoryEntries(ctx)
	if err != nil {
		return DataInventory{}, err
	}
	for _, e := range es {
		out.Entries = append(out.Entries, inventoryEntryView(e))
	}
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return DataInventory{}, err
	}
	if out.Schedule, err = retentionSchedule(ctx, u, today); err != nil {
		return DataInventory{}, err
	}
	reviews, err := u.Compliance().InventoryReviews(ctx)
	if err != nil {
		return DataInventory{}, err
	}
	since, err := orgSince(ctx, a, u, today)
	if err != nil {
		return DataInventory{}, err
	}
	out.Review = inventoryReviewOf(reviews, today, since)
	return out, nil
}

// orgSince is the day the organisation's earliest member joined, or today.
func orgSince(ctx context.Context, a *API, u storage.UnitOfWork, today time.Time) (time.Time, error) {
	ms, err := u.Members().List(ctx, a.deps.Clock.Now())
	if err != nil {
		return time.Time{}, err
	}
	since := today
	for _, m := range ms {
		if d := m.Since.UTC().Truncate(24 * time.Hour); d.Before(since) {
			since = d
		}
	}
	return since, nil
}

func inventoryEntryOf(in InventoryEntryInput) (storage.InventoryEntry, error) {
	e := storage.InventoryEntry{What: strings.TrimSpace(in.What), Why: strings.TrimSpace(in.Why), Where: strings.TrimSpace(in.Where),
		Who: strings.TrimSpace(in.Who), Retention: strings.TrimSpace(in.Retention), Transfers: strings.TrimSpace(in.Transfers),
		NeedsDPIA: in.NeedsDPIA, State: cmpOr(in.State, "open")}
	var bad []FieldProblem
	for _, f := range []struct{ field, v string }{{"what", e.What}, {"why", e.Why}, {"where", e.Where}, {"who", e.Who}, {"retention", e.Retention},
		{"transfers", e.Transfers}} {
		if f.v == "" {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body." + f.field, Message: "is empty", Fix: "Say " + f.field + ", in a sentence"})
		}
	}
	if len(bad) > 0 {
		return e, invalid(bad)
	}
	return e, nil
}

func registerDataInventory(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeInventory}, locks...)
	register(a, authz.GetDataInventory, exempt(huma.Operation{
		OperationID: "getDataInventory", Method: http.MethodGet, Path: "/v1/compliance/data-inventory", Tags: []string{"Compliance"},
		Summary: "Get the personal-data inventory and retention schedule",
		Description: "The personal-data inventory (CMP-PD-02, CMP-05): what Fyniq itself holds, from its catalogue, with what, why, " +
			"where, who reads it, how long it is kept, how it is protected and where it goes abroad; what Fyniq never holds; what the " +
			"organisation added for its processing outside Fyniq; the retention schedule from the settings in force; and the " +
			"quarterly review (CTL-DP-05). Read-only; no side effects.",
	}, "cursor", "The inventory is a handful of classes and entries, read whole as one register."), locks,
		func(ctx context.Context, _ *struct{}) (*dataInventoryOutput, error) {
			p, _ := authn.From(ctx)
			out := &dataInventoryOutput{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				out.Body, err = a.dataInventory(ctx, u)
				return err
			})
			return out, err
		})

	register(a, authz.CreateDataInventoryEntry, huma.Operation{
		OperationID: "createDataInventoryEntry", Method: http.MethodPost, Path: "/v1/compliance/data-inventory/entries", Tags: []string{"Compliance"},
		Summary:     "Add an entry to the personal-data inventory",
		Description: "Adds personal data the organisation processes outside Fyniq to its inventory (CMP-PD-02), at version 1. Side effects: the entry, the audit event data_inventory_entry.saved. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *inventoryCreateInput) (*inventoryEntryOutput, error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (InventoryEntryView, error) {
			e, err := inventoryEntryOf(in.Body)
			if err != nil {
				return InventoryEntryView{}, err
			}
			e.Version, e.SavedBy, e.SavedAt = 1, uploader(ctx), now
			saved, err := u.Compliance().SaveInventoryEntry(ctx, e)
			return inventoryEntryView(saved), err
		})
		if err != nil {
			return nil, err
		}
		return &inventoryEntryOutput{Status: w.Status, ETag: inventoryETag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.ReviseDataInventoryEntry, huma.Operation{
		OperationID: "reviseDataInventoryEntry", Method: http.MethodPut, Path: "/v1/compliance/data-inventory/entries/{entry_id}", Tags: []string{"Compliance"},
		Summary:     "Change an entry of the personal-data inventory",
		Description: "Saves the entry's next version (CMP-PD-02), or retires it when the processing stops. Earlier versions are kept. Side effects: the version, the audit event data_inventory_entry.saved. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), func(ctx context.Context, in *inventoryReviseInput) (*inventoryEntryOutput, error) {
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (InventoryEntryView, error) {
			es, err := u.Compliance().InventoryEntries(ctx)
			if err != nil {
				return InventoryEntryView{}, err
			}
			i := slices.IndexFunc(es, func(e storage.InventoryEntry) bool { return e.EntryID.String() == in.EntryID })
			if i < 0 {
				return InventoryEntryView{}, problem(CodeNotFound, "There is no inventory entry "+in.EntryID+".", "Use an entry_id from getDataInventory.")
			}
			if err := checkIfMatch(in.IfMatch, inventoryETag(es[i].Version)); err != nil {
				return InventoryEntryView{}, err
			}
			e, err := inventoryEntryOf(in.Body)
			if err != nil {
				return InventoryEntryView{}, err
			}
			e.EntryID, e.Version, e.SavedBy, e.SavedAt = uuid.MustParse(in.EntryID), es[i].Version+1, uploader(ctx), now
			saved, err := u.Compliance().SaveInventoryEntry(ctx, e)
			return inventoryEntryView(saved), err
		})
		if err != nil {
			return nil, err
		}
		return &inventoryEntryOutput{Status: w.Status, ETag: inventoryETag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.ReviewDataInventory, huma.Operation{
		OperationID: "reviewDataInventory", Method: http.MethodPost, Path: "/v1/compliance/data-inventory/reviews", Tags: []string{"Compliance"},
		Summary:     "Record the inventory's quarterly review",
		Description: "Records that the Compliance officer reviewed the whole personal-data inventory for the quarter waiting (CTL-DP-05), once a quarter. Side effects: the review, the audit event data_inventory.reviewed. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *inventoryReviewInput) (*inventoryReviewOutput, error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (InventoryReviewState, error) {
			inv, err := a.dataInventory(ctx, u)
			if err != nil {
				return InventoryReviewState{}, err
			}
			if inv.Review.Quarter == "" || inv.Review.Quarter != in.Body.Quarter {
				return InventoryReviewState{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.quarter", Message: "is not the quarter waiting for its review",
					Fix: "Review the quarter getDataInventory's review.quarter names; none waits until a quarter ends"}})
			}
			q, _ := time.Parse(time.DateOnly, in.Body.Quarter)
			if _, err := u.Compliance().ReviewInventory(ctx, storage.InventoryReview{Quarter: q, ReviewedBy: uploader(ctx), ReviewedAt: now,
				Note: strings.TrimSpace(in.Body.Note)}); err != nil {
				return InventoryReviewState{}, err
			}
			after, err := a.dataInventory(ctx, u)
			return after.Review, err
		})
		if err != nil {
			return nil, err
		}
		return &inventoryReviewOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
