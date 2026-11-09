package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------- the Compliance Center's verifications (CMP-01; p1-compliance-center D4)

func (u *unitOfWork) Compliance() storage.Compliance { return compliance{u} }

type compliance struct{ u *unitOfWork }

func toControlRun(r sqlcdb.FyniqControlRun) storage.ControlRun {
	return storage.ControlRun{ID: r.ID, Control: r.Control, Frequency: r.Frequency, Check: r.CheckHow, Since: r.Since, RanAt: r.RanAt,
		OK: r.Ok, Count: int(r.ItemCount), Items: r.Items, Detail: r.Detail}
}

func (c compliance) RecordRun(ctx context.Context, r storage.ControlRun) (storage.ControlRun, error) {
	if err := c.u.check(); err != nil {
		return storage.ControlRun{}, err
	}
	items := r.Items
	if len(items) == 0 {
		items = []byte("[]")
	}
	row, err := c.u.q.InsertControlRun(ctx, sqlcdb.InsertControlRunParams{Control: r.Control, Frequency: r.Frequency, CheckHow: r.Check,
		Since: r.Since, RanAt: r.RanAt, Ok: r.OK, ItemCount: int32(min(max(r.Count, 0), 1<<30)), Items: items, Detail: r.Detail}) // #nosec G115 -- bounded
	if err != nil {
		return storage.ControlRun{}, mapError(err)
	}
	return toControlRun(row), nil
}

func (c compliance) LatestRuns(ctx context.Context) ([]storage.ControlRun, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.LatestControlRuns(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ControlRun, len(rows))
	for i, r := range rows {
		out[i] = toControlRun(r)
	}
	return out, nil
}

func (c compliance) DualControl(ctx context.Context, since time.Time) ([]storage.DualControlApproval, []storage.DualControlRelease, error) {
	if err := c.u.check(); err != nil {
		return nil, nil, err
	}
	approvals, err := c.u.q.DualControlApprovals(ctx, &since)
	if err != nil {
		return nil, nil, mapError(err)
	}
	releases, err := c.u.q.DualControlReleases(ctx, since)
	if err != nil {
		return nil, nil, mapError(err)
	}
	a := make([]storage.DualControlApproval, len(approvals))
	for i, r := range approvals {
		a[i] = storage.DualControlApproval{ID: r.ID, ObjectType: r.ObjectType, ObjectKey: r.ObjectKey, DecidedAt: r.DecidedAt,
			Signatures: int(r.Signatures), OwnSignatures: int(r.OwnSignatures)}
	}
	rel := make([]storage.DualControlRelease, len(releases))
	for i, r := range releases {
		rel[i] = storage.DualControlRelease{BatchID: r.BatchID, ReleasedAt: r.ReleasedAt, ApprovalID: r.ApprovalID, BreakGlassID: r.BreakGlassID,
			Approved: r.Approved, Signatures: int(r.Signatures), OwnSignatures: int(r.OwnSignatures), BreakGlassUsed: r.BreakGlassUsed}
	}
	return a, rel, nil
}

func (c compliance) SecondFactors(ctx context.Context, since time.Time) ([]storage.MemberSecondFactor, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.MembersSecondFactor(ctx, since)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.MemberSecondFactor, len(rows))
	for i, r := range rows {
		out[i] = storage.MemberSecondFactor{UserID: r.UserID, Passkeys: int(r.Passkeys), SessionsWith: int(r.SessionsWith),
			SessionsWithout: int(r.SessionsWithout)}
		if r.SessionsWithout > 0 {
			at := r.LastWithout
			out[i].LastWithout = &at
		}
	}
	return out, nil
}

func (c compliance) UnreconciledReads(ctx context.Context, since time.Time, actions []string, sample int) ([]storage.UnreconciledRead, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.ExportsWithoutTheirRecords(ctx, sqlcdb.ExportsWithoutTheirRecordsParams{Since: since, Actions: actions,
		Sample: int32(min(max(sample, 1), 100000))}) // #nosec G115 -- bounded
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.UnreconciledRead, len(rows))
	for i, r := range rows {
		out[i] = storage.UnreconciledRead{Kind: r.Kind, ObjectKey: r.ObjectKey, At: r.At, Reason: r.Reason}
	}
	return out, nil
}

func toControlState(r sqlcdb.FyniqControlState) storage.ControlState {
	return storage.ControlState{ID: r.ID, Control: r.Control, Status: r.Status, Detail: r.Detail, Checks: r.Checks, FailingSince: r.FailingSince,
		RunAt: r.RunAt, ChangedAt: r.ChangedAt, Version: int(r.Version)}
}

func (c compliance) States(ctx context.Context) ([]storage.ControlState, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.ListControlStates(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ControlState, len(rows))
	for i, r := range rows {
		out[i] = toControlState(r)
	}
	return out, nil
}

// failing reports whether a status is fail or overdue.
func failing(status string) bool { return status == "fail" || status == "overdue" }

func (c compliance) SaveState(ctx context.Context, s storage.ControlState, version int) (storage.ControlState, error) {
	if err := c.u.check(); err != nil {
		return storage.ControlState{}, err
	}
	checks := s.Checks
	if len(checks) == 0 {
		checks = []byte("[]")
	}
	if version == 0 {
		row, err := c.u.q.InsertControlState(ctx, sqlcdb.InsertControlStateParams{Control: s.Control, Status: s.Status, Detail: s.Detail,
			Checks: checks, FailingSince: s.FailingSince, RunAt: s.RunAt, ChangedAt: s.ChangedAt})
		if err != nil {
			return storage.ControlState{}, mapError(err) // a state written meanwhile is CodeVersion (control_state_one)
		}
		out := toControlState(row)
		v := out.Version
		action := "control.status_changed"
		if failing(s.Status) {
			action = "control.failed"
		}
		return out, c.u.event(ctx, action, "control_state", out.ID, &v, map[string][2]any{"control": {nil, s.Control},
			"status": {nil, s.Status}})
	}
	before, err := c.u.q.GetControlState(ctx, s.Control)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ControlState{}, &storage.Error{Code: storage.CodeVersion, Message: "the control has no state at that version"}
	}
	if err != nil {
		return storage.ControlState{}, mapError(err)
	}
	row, err := c.u.q.UpdateControlState(ctx, sqlcdb.UpdateControlStateParams{Control: s.Control, Version: int32(min(max(version, 1), 1<<30)), // #nosec G115 -- bounded
		Status: s.Status, Detail: s.Detail, Checks: checks, FailingSince: s.FailingSince, RunAt: s.RunAt, ChangedAt: s.ChangedAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ControlState{}, &storage.Error{Code: storage.CodeVersion, Message: "the control's state changed since it was read"}
	}
	if err != nil {
		return storage.ControlState{}, mapError(err)
	}
	out := toControlState(row)
	if before.Status == s.Status {
		return out, nil // the same status: when it ran moved, and the trail has nothing to add
	}
	action := "control.status_changed"
	switch {
	case failing(s.Status) && !failing(before.Status):
		action = "control.failed"
	case !failing(s.Status) && failing(before.Status):
		action = "control.recovered"
	}
	v := out.Version
	return out, c.u.event(ctx, action, "control_state", out.ID, &v, map[string][2]any{"control": {s.Control, s.Control},
		"status": {before.Status, s.Status}})
}

// ------------------------------------------------------------------ the risk register (CMP-06; p1-compliance-center D7)

func riskFieldsOf(title, description, owner, likelihood, impact, mitigation string, months int16, next time.Time, state string) storage.RiskFields {
	return storage.RiskFields{Title: title, Description: description, Owner: owner, Likelihood: likelihood, Impact: impact,
		Mitigation: mitigation, ReviewMonths: int(months), NextReviewOn: next, State: state}
}

func (c compliance) Risks(ctx context.Context) ([]storage.Risk, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.ListRisks(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	reviews, err := c.u.q.LatestRiskReviews(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	last := map[uuid.UUID]*storage.RiskReview{}
	for _, r := range reviews {
		last[r.RiskID] = &storage.RiskReview{Version: int(r.Version), ReviewedBy: r.ReviewedBy, ReviewedAt: r.ReviewedAt, Note: r.Note,
			NextReviewOn: r.NextReviewOn}
	}
	out := make([]storage.Risk, len(rows))
	for i, r := range rows {
		out[i] = storage.Risk{ID: r.ID, Ref: r.Ref, CreatedAt: r.CreatedAt, Version: int(r.Version),
			RiskFields: riskFieldsOf(r.Title, r.Description, r.OwnerRole, r.Likelihood, r.Impact, r.Mitigation, r.ReviewMonths, r.NextReviewOn, r.State),
			SavedBy:    r.SavedBy, SavedAt: r.SavedAt, LastReview: last[r.ID]}
		if l := last[r.ID]; l != nil && l.Version == out[i].Version {
			out[i].NextReviewOn = l.NextReviewOn // a review of this version set when the next one falls due
		}
	}
	return out, nil
}

// risk is one risk at its latest version, or CodeNotFound.
func (c compliance) risk(ctx context.Context, id uuid.UUID) (storage.Risk, error) {
	all, err := c.Risks(ctx)
	if err != nil {
		return storage.Risk{}, err
	}
	for _, r := range all {
		if r.ID == id {
			return r, nil
		}
	}
	return storage.Risk{}, ports.ErrNotFound
}

func (c compliance) insertVersion(ctx context.Context, id uuid.UUID, version int, f storage.RiskFields, savedBy string, at time.Time) error {
	_, err := c.u.q.InsertRiskVersion(ctx, sqlcdb.InsertRiskVersionParams{RiskID: id, Version: int32(min(max(version, 1), 1<<30)), // #nosec G115 -- bounded
		Title: f.Title, Description: f.Description, OwnerRole: f.Owner, Likelihood: f.Likelihood, Impact: f.Impact, Mitigation: f.Mitigation,
		ReviewMonths: int16(min(max(f.ReviewMonths, 1), 12)), NextReviewOn: f.NextReviewOn, State: f.State, SavedBy: savedBy, SavedAt: at}) // #nosec G115 -- bounded
	return mapError(err)
}

func (c compliance) CreateRisk(ctx context.Context, n storage.NewRisk) (storage.Risk, error) {
	if err := c.u.check(); err != nil {
		return storage.Risk{}, err
	}
	ref := n.Ref
	if ref == "" {
		next, err := c.u.q.NextRiskRef(ctx)
		if err != nil {
			return storage.Risk{}, mapError(err)
		}
		ref = next
	}
	row, err := c.u.q.InsertRisk(ctx, sqlcdb.InsertRiskParams{Ref: ref, CreatedAt: n.At})
	if err != nil {
		return storage.Risk{}, mapError(err)
	}
	if err := c.insertVersion(ctx, row.ID, 1, n.RiskFields, n.SavedBy, n.At); err != nil {
		return storage.Risk{}, err
	}
	v := 1
	if err := c.u.event(ctx, "risk.saved", "risk", row.ID, &v, map[string][2]any{"ref": {nil, ref}, "title": {nil, n.Title},
		"owner": {nil, n.Owner}, "likelihood": {nil, n.Likelihood}, "impact": {nil, n.Impact}, "next_review_on": {nil, n.NextReviewOn.Format(time.DateOnly)},
		"state": {nil, n.State}}); err != nil {
		return storage.Risk{}, err
	}
	return c.risk(ctx, row.ID)
}

func (c compliance) ReviseRisk(ctx context.Context, id uuid.UUID, f storage.RiskFields, savedBy string, at time.Time, version int) (storage.Risk, error) {
	if err := c.u.check(); err != nil {
		return storage.Risk{}, err
	}
	before, err := c.risk(ctx, id)
	if err != nil {
		return storage.Risk{}, err
	}
	if before.Version != version {
		return storage.Risk{}, &storage.Error{Code: storage.CodeVersion, Message: "the risk changed since it was read"}
	}
	if err := c.insertVersion(ctx, id, version+1, f, savedBy, at); err != nil {
		return storage.Risk{}, err
	}
	changes := map[string][2]any{}
	for name, pair := range map[string][2]any{"title": {before.Title, f.Title}, "description": {before.Description, f.Description},
		"owner": {before.Owner, f.Owner}, "likelihood": {before.Likelihood, f.Likelihood}, "impact": {before.Impact, f.Impact},
		"mitigation": {before.Mitigation, f.Mitigation}, "review_months": {before.ReviewMonths, f.ReviewMonths},
		"next_review_on": {before.NextReviewOn.Format(time.DateOnly), f.NextReviewOn.Format(time.DateOnly)}, "state": {before.State, f.State}} {
		if pair[0] != pair[1] {
			changes[name] = pair
		}
	}
	v := version + 1
	if err := c.u.event(ctx, "risk.saved", "risk", id, &v, changes); err != nil {
		return storage.Risk{}, err
	}
	return c.risk(ctx, id)
}

func (c compliance) ReviewRisk(ctx context.Context, id uuid.UUID, r storage.RiskReview) (storage.Risk, error) {
	if err := c.u.check(); err != nil {
		return storage.Risk{}, err
	}
	before, err := c.risk(ctx, id)
	if err != nil {
		return storage.Risk{}, err
	}
	if before.Version != r.Version {
		return storage.Risk{}, &storage.Error{Code: storage.CodeVersion, Message: "the risk changed since it was read"}
	}
	if _, err := c.u.q.InsertRiskReview(ctx, sqlcdb.InsertRiskReviewParams{RiskID: id, Version: int32(min(max(r.Version, 1), 1<<30)), // #nosec G115 -- bounded
		ReviewedBy: r.ReviewedBy, ReviewedAt: r.ReviewedAt, Note: r.Note, NextReviewOn: r.NextReviewOn}); err != nil {
		return storage.Risk{}, mapError(err)
	}
	v := r.Version
	if err := c.u.event(ctx, "risk.reviewed", "risk", id, &v, map[string][2]any{"next_review_on": {
		before.NextReviewOn.Format(time.DateOnly), r.NextReviewOn.Format(time.DateOnly)}, "note": {nil, r.Note}}); err != nil {
		return storage.Risk{}, err
	}
	return c.risk(ctx, id)
}

// ------------------------------------------------------------------ the policy library (CMP-07; p1-compliance-center D8)

func (c compliance) Policies(ctx context.Context) (storage.PolicyLibrary, error) {
	var out storage.PolicyLibrary
	if err := c.u.check(); err != nil {
		return out, err
	}
	drafts, err := c.u.q.LatestPolicyDrafts(ctx)
	if err != nil {
		return out, mapError(err)
	}
	for _, d := range drafts {
		out.Drafts = append(out.Drafts, storage.PolicyDraft{Key: d.Key, Revision: int(d.Revision), Body: d.Body, SHA256: d.Sha256, SavedBy: d.SavedBy,
			SavedAt: d.SavedAt})
	}
	versions, err := c.u.q.ListPolicyVersions(ctx)
	if err != nil {
		return out, mapError(err)
	}
	for _, v := range versions {
		out.Versions = append(out.Versions, toPolicyVersion(v))
	}
	acks, err := c.u.q.ListPolicyAcknowledgements(ctx)
	if err != nil {
		return out, mapError(err)
	}
	for _, a := range acks {
		out.Acknowledgements = append(out.Acknowledgements, storage.PolicyAcknowledgement{Key: a.Key, Version: int(a.Version), By: a.AcknowledgedBy,
			At: a.AcknowledgedAt})
	}
	return out, nil
}

func toPolicyVersion(v sqlcdb.FyniqPolicyVersion) storage.PublishedPolicy {
	return storage.PublishedPolicy{ID: v.ID, Key: v.Key, Version: int(v.Version), Title: v.Title, Body: v.Body, SHA256: v.Sha256, From: v.FromDraft,
		PublishedBy: v.PublishedBy, PublishedAt: v.PublishedAt}
}

func (c compliance) SavePolicyDraft(ctx context.Context, d storage.PolicyDraft) (storage.PolicyDraft, error) {
	if err := c.u.check(); err != nil {
		return storage.PolicyDraft{}, err
	}
	row, err := c.u.q.InsertPolicyDraft(ctx, sqlcdb.InsertPolicyDraftParams{Key: d.Key, Revision: int32(min(max(d.Revision, 1), 1<<30)), // #nosec G115 -- bounded
		Body: d.Body, SavedBy: d.SavedBy, SavedAt: d.SavedAt})
	if err != nil {
		return storage.PolicyDraft{}, mapError(err)
	}
	v := int(row.Revision)
	return storage.PolicyDraft{Key: row.Key, Revision: v, Body: row.Body, SHA256: row.Sha256, SavedBy: row.SavedBy, SavedAt: row.SavedAt},
		c.u.event(ctx, "policy.draft_saved", "policy_draft", row.ID, &v, map[string][2]any{"key": {nil, row.Key}, "revision": {nil, v},
			"sha256": {nil, fmt.Sprintf("%x", row.Sha256)}})
}

func (c compliance) PublishPolicy(ctx context.Context, v storage.PublishedPolicy) (storage.PublishedPolicy, error) {
	if err := c.u.check(); err != nil {
		return storage.PublishedPolicy{}, err
	}
	row, err := c.u.q.InsertPolicyVersion(ctx, sqlcdb.InsertPolicyVersionParams{Key: v.Key, Version: int32(min(max(v.Version, 1), 1<<30)), // #nosec G115 -- bounded
		Title: v.Title, Body: v.Body, FromDraft: v.From, PublishedBy: v.PublishedBy, PublishedAt: v.PublishedAt})
	if err != nil {
		return storage.PublishedPolicy{}, mapError(err)
	}
	out := toPolicyVersion(row)
	n := out.Version
	return out, c.u.event(ctx, "policy.published", "policy_version", row.ID, &n, map[string][2]any{"key": {nil, row.Key}, "version": {nil, n},
		"from": {nil, row.FromDraft}, "sha256": {nil, fmt.Sprintf("%x", row.Sha256)}})
}

func (c compliance) AcknowledgePolicy(ctx context.Context, a storage.PolicyAcknowledgement) error {
	if err := c.u.check(); err != nil {
		return err
	}
	row, err := c.u.q.InsertPolicyAcknowledgement(ctx, sqlcdb.InsertPolicyAcknowledgementParams{Key: a.Key, Version: int32(min(max(a.Version, 1), 1<<30)), // #nosec G115 -- bounded
		AcknowledgedBy: a.By, AcknowledgedAt: a.At})
	if err != nil {
		return mapError(err)
	}
	return c.u.event(ctx, "policy.acknowledged", "policy_acknowledgement", row.ID, nil, map[string][2]any{"key": {nil, row.Key},
		"version": {nil, int(row.Version)}})
}

func (c compliance) ConsentPurposes(ctx context.Context) ([]storage.ConsentPurpose, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.ListConsentPurposes(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ConsentPurpose, 0, len(rows))
	for _, r := range rows {
		out = append(out, storage.ConsentPurpose{ID: r.ID, Name: r.Name, Description: r.Description, Audience: r.Audience,
			NeedsConsent: r.NeedsConsent, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

func (c compliance) CreateConsentPurpose(ctx context.Context, p storage.ConsentPurpose) (storage.ConsentPurpose, error) {
	if err := c.u.check(); err != nil {
		return storage.ConsentPurpose{}, err
	}
	r, err := c.u.q.InsertConsentPurpose(ctx, sqlcdb.InsertConsentPurposeParams{Name: p.Name, Description: p.Description, Audience: p.Audience,
		NeedsConsent: p.NeedsConsent, CreatedBy: p.CreatedBy, CreatedAt: p.CreatedAt})
	if err != nil {
		return storage.ConsentPurpose{}, mapError(err)
	}
	if err := c.u.event(ctx, "consent_purpose.created", "consent_purpose", r.ID, nil, map[string][2]any{"name": {nil, r.Name},
		"audience": {nil, r.Audience}, "needs_consent": {nil, r.NeedsConsent}}); err != nil {
		return storage.ConsentPurpose{}, err
	}
	return storage.ConsentPurpose{ID: r.ID, Name: r.Name, Description: r.Description, Audience: r.Audience, NeedsConsent: r.NeedsConsent,
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt}, nil
}

func (c compliance) Consents(ctx context.Context) ([]storage.ConsentRecord, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.ListConsentRecords(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ConsentRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, storage.ConsentRecord{ID: r.ID, PurposeID: r.PurposeID, Person: r.Person, Event: r.Event, At: r.At,
			RecordedBy: r.RecordedBy, Note: str(r.Note)})
	}
	return out, nil
}

func (c compliance) RecordConsent(ctx context.Context, rec storage.ConsentRecord) (storage.ConsentRecord, error) {
	if err := c.u.check(); err != nil {
		return storage.ConsentRecord{}, err
	}
	r, err := c.u.q.InsertConsentRecord(ctx, sqlcdb.InsertConsentRecordParams{PurposeID: rec.PurposeID, Person: rec.Person, Event: rec.Event,
		At: rec.At, RecordedBy: rec.RecordedBy, Note: opt(rec.Note)})
	if err != nil {
		return storage.ConsentRecord{}, mapError(err)
	}
	if err := c.u.event(ctx, "consent."+r.Event, "consent_record", r.ID, nil, map[string][2]any{"purpose_id": {nil, r.PurposeID},
		"person": {nil, r.Person}, "event": {nil, r.Event}}); err != nil {
		return storage.ConsentRecord{}, err
	}
	return storage.ConsentRecord{ID: r.ID, PurposeID: r.PurposeID, Person: r.Person, Event: r.Event, At: r.At, RecordedBy: r.RecordedBy,
		Note: str(r.Note)}, nil
}

func (c compliance) InventoryEntries(ctx context.Context) ([]storage.InventoryEntry, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.ListDataInventoryEntries(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	var out []storage.InventoryEntry
	for _, r := range rows { // by entry, then version: the last of each entry is its latest
		e := storage.InventoryEntry{EntryID: r.EntryID, Version: int(r.Version), What: r.What, Why: r.Why, Where: r.WhereHeld, Who: r.Who,
			Retention: r.Retention, Transfers: r.Transfers, NeedsDPIA: r.NeedsDpia, State: r.State, SavedBy: r.SavedBy, SavedAt: r.SavedAt}
		if n := len(out); n > 0 && out[n-1].EntryID == e.EntryID {
			out[n-1] = e
			continue
		}
		out = append(out, e)
	}
	slices.SortStableFunc(out, func(a, b storage.InventoryEntry) int { return strings.Compare(a.EntryID.String(), b.EntryID.String()) }) // v7: by time
	return out, nil
}

func (c compliance) SaveInventoryEntry(ctx context.Context, e storage.InventoryEntry) (storage.InventoryEntry, error) {
	if err := c.u.check(); err != nil {
		return storage.InventoryEntry{}, err
	}
	if e.EntryID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return storage.InventoryEntry{}, err
		}
		e.EntryID = id
	}
	if err := c.u.q.InsertDataInventoryEntry(ctx, sqlcdb.InsertDataInventoryEntryParams{EntryID: e.EntryID,
		Version: int32(min(max(e.Version, 1), 1<<30)), What: e.What, // #nosec G115 -- bounded
		Why: e.Why, WhereHeld: e.Where, Who: e.Who, Retention: e.Retention, Transfers: e.Transfers, NeedsDpia: e.NeedsDPIA, State: e.State,
		SavedBy: e.SavedBy, SavedAt: e.SavedAt}); err != nil {
		return storage.InventoryEntry{}, mapError(err)
	}
	v := e.Version
	if err := c.u.event(ctx, "data_inventory_entry.saved", "data_inventory_entry", e.EntryID, &v, map[string][2]any{"what": {nil, e.What},
		"why": {nil, e.Why}, "where": {nil, e.Where}, "retention": {nil, e.Retention}, "transfers": {nil, e.Transfers},
		"needs_dpia": {nil, e.NeedsDPIA}, "state": {nil, e.State}}); err != nil {
		return storage.InventoryEntry{}, err
	}
	return e, nil
}

func (c compliance) InventoryReviews(ctx context.Context) ([]storage.InventoryReview, error) {
	if err := c.u.check(); err != nil {
		return nil, err
	}
	rows, err := c.u.q.ListDataInventoryReviews(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.InventoryReview, 0, len(rows))
	for _, r := range rows {
		out = append(out, storage.InventoryReview{ID: r.ID, Quarter: r.Quarter, ReviewedBy: r.ReviewedBy, ReviewedAt: r.ReviewedAt, Note: str(r.Note)})
	}
	return out, nil
}

func (c compliance) ReviewInventory(ctx context.Context, r storage.InventoryReview) (storage.InventoryReview, error) {
	if err := c.u.check(); err != nil {
		return storage.InventoryReview{}, err
	}
	row, err := c.u.q.InsertDataInventoryReview(ctx, sqlcdb.InsertDataInventoryReviewParams{Quarter: r.Quarter,
		ReviewedBy: r.ReviewedBy, ReviewedAt: r.ReviewedAt, Note: opt(r.Note)})
	if err != nil {
		return storage.InventoryReview{}, mapError(err)
	}
	if err := c.u.event(ctx, "data_inventory.reviewed", "data_inventory_review", row.ID, nil, map[string][2]any{
		"quarter": {nil, row.Quarter.Format(time.DateOnly)}, "note": {nil, nilIfEmpty(r.Note)}}); err != nil {
		return storage.InventoryReview{}, err
	}
	return storage.InventoryReview{ID: row.ID, Quarter: row.Quarter, ReviewedBy: row.ReviewedBy, ReviewedAt: row.ReviewedAt, Note: str(row.Note)}, nil
}

func (c compliance) DPIAs(ctx context.Context) ([]storage.DPIAVersion, []storage.DPIAApproval, error) {
	if err := c.u.check(); err != nil {
		return nil, nil, err
	}
	rows, err := c.u.q.ListDPIAVersions(ctx)
	if err != nil {
		return nil, nil, mapError(err)
	}
	versions := make([]storage.DPIAVersion, 0, len(rows))
	for _, r := range rows {
		versions = append(versions, storage.DPIAVersion{Key: r.Key, Version: int(r.Version), Title: r.Title, Body: r.Body, SHA256: r.Sha256,
			From: r.FromDraft, EntryID: r.EntryID, SavedBy: r.SavedBy, SavedAt: r.SavedAt})
	}
	arows, err := c.u.q.ListDPIAApprovals(ctx)
	if err != nil {
		return nil, nil, mapError(err)
	}
	approvals := make([]storage.DPIAApproval, 0, len(arows))
	for _, r := range arows {
		approvals = append(approvals, storage.DPIAApproval{Key: r.Key, Version: int(r.Version), ApprovedBy: r.ApprovedBy, ApprovedAt: r.ApprovedAt})
	}
	return versions, approvals, nil
}

func (c compliance) SaveDPIA(ctx context.Context, v storage.DPIAVersion) (storage.DPIAVersion, error) {
	if err := c.u.check(); err != nil {
		return storage.DPIAVersion{}, err
	}
	r, err := c.u.q.InsertDPIAVersion(ctx, sqlcdb.InsertDPIAVersionParams{Key: v.Key, Version: int32(min(max(v.Version, 1), 1<<30)), // #nosec G115 -- bounded
		Title: v.Title, Body: v.Body, FromDraft: v.From, EntryID: v.EntryID, SavedBy: v.SavedBy, SavedAt: v.SavedAt})
	if err != nil {
		return storage.DPIAVersion{}, mapError(err)
	}
	ver := int(r.Version)
	if err := c.u.event(ctx, "dpia.saved", "dpia_version", r.ID, &ver, map[string][2]any{"key": {nil, r.Key}, "title": {nil, r.Title},
		"from": {nil, r.FromDraft}, "sha256": {nil, fmt.Sprintf("%x", r.Sha256)}}); err != nil {
		return storage.DPIAVersion{}, err
	}
	return storage.DPIAVersion{Key: r.Key, Version: ver, Title: r.Title, Body: r.Body, SHA256: r.Sha256, From: r.FromDraft, EntryID: r.EntryID,
		SavedBy: r.SavedBy, SavedAt: r.SavedAt}, nil
}

func (c compliance) ApproveDPIA(ctx context.Context, a storage.DPIAApproval) error {
	if err := c.u.check(); err != nil {
		return err
	}
	r, err := c.u.q.InsertDPIAApproval(ctx, sqlcdb.InsertDPIAApprovalParams{Key: a.Key, Version: int32(min(max(a.Version, 1), 1<<30)), // #nosec G115 -- bounded
		ApprovedBy: a.ApprovedBy, ApprovedAt: a.ApprovedAt})
	if err != nil {
		return mapError(err)
	}
	ver := int(r.Version)
	return c.u.event(ctx, "dpia.approved", "dpia_approval", r.ID, &ver, map[string][2]any{"key": {nil, r.Key}, "version": {nil, ver}})
}
