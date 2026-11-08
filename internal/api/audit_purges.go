package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ The purges after retention (AUD-09, AUD-10)

// AuditPurge is a purge after retention behind its signed boundary: of the audit trail, its events up to one, or of the
// ledger, a financial year carried forward.
type AuditPurge struct {
	Kind            string           `json:"kind" enum:"trail,ledger" doc:"trail: the audit trail's events up to last_seq (AUD-09). ledger: a financial year's entries, carried forward to the boundary (AUD-10)." example:"ledger"`
	LastSeq         *int64           `json:"last_seq,omitempty" doc:"trail: the last event purged; verification starts after it." example:"4120"`
	LastOccurredAt  *string          `json:"last_occurred_at,omitempty" format:"date-time" doc:"trail: when that event occurred." example:"2019-03-31T15:59:59Z"`
	Boundary        *string          `json:"boundary,omitempty" format:"date" doc:"ledger: the first day kept whole; every report from it on is unchanged, and an earlier day is refused (BALANCE-PURGED)." example:"2020-01-01"`
	CarriedRevision *int64           `json:"carried_revision,omitempty" doc:"ledger: the ledger revision after the carry-forward; an \"as at\" revision before it is refused." example:"18342"`
	Approval        string           `json:"approval" doc:"The setting change that switched the purge after retention on, under dual control." example:"retention.purge_after_retention v1"`
	Counts          map[string]int64 `json:"counts" doc:"What it purged and kept: events and security events, or entries and lines purged and kept and carry-forward entries." example:"{\"entries_purged\":1840,\"carry_forward_entries\":212}"`
	AnchorKey       string           `json:"anchor_key" doc:"Where its signed copy is in the anchors' write-once bucket." example:"ledger-boundaries/01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10/2020-01-01-4f1a2b3c4d5e.json"`
	WrittenAt       string           `json:"written_at" format:"date-time" doc:"When it was written." example:"2027-04-04T02:00:00Z"`
}

// PurgedPosting is a record's posting the ledger's purge removed: what its tombstone keeps.
type PurgedPosting struct {
	PostingIdentity string `json:"posting_identity" doc:"The posting's identity, as the record's history names it." example:"invoice:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10:v1:invoice@1"`
	SourceType      string `json:"source_type" doc:"Its source document's type." example:"invoice"`
	PostingDate     string `json:"posting_date" format:"date" doc:"The day it was posted on." example:"2019-03-14"`
	LedgerRevision  int64  `json:"ledger_revision" doc:"The ledger revision it was recorded at." example:"1204"`
	Reversal        bool   `json:"reversal" doc:"true for a reversal of another posting, such as a void." example:"false"`
	Boundary        string `json:"boundary" format:"date" doc:"The boundary it was carried forward to: the record's balance is in its carry-forward, dated the day before." example:"2020-01-01"`
}

// AuditPurges are the purges after retention, oldest first.
type AuditPurges struct {
	Items    []AuditPurge    `json:"items" nullable:"false" doc:"Every purge after retention, the trail's and the ledger's, oldest first; none while the purge has never run."`
	Postings []PurgedPosting `json:"postings,omitempty" doc:"With record_id: that record's postings the ledger's purge removed, in the order they were posted."`
}

type auditPurgesInput struct {
	RecordID string `query:"record_id" format:"uuid" doc:"A source document: also list its postings the ledger's purge removed." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
}

type auditPurgesOutput struct{ Body AuditPurges }

func registerAuditPurges(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.ListAuditPurges, exempt(huma.Operation{
		OperationID: "listAuditPurges", Method: http.MethodGet, Path: "/v1/audit/purges", Tags: []string{"Audit trail"},
		Summary: "List the purges after retention",
		Description: "Every purge after retention behind its signed boundary, oldest first (AUD-09, AUD-10): the audit trail's, each up " +
			"to an event, and the ledger's, each a financial year carried forward to the first day of the next. With record_id, also that " +
			"record's postings the ledger's purge removed, from their tombstones, so its view can say when they went. Nothing is listed " +
			"while retention.purge_after_retention has never run. Changes nothing.",
	}, "cursor", "A purge after retention runs at most weekly, one financial year or one segment of the trail at a time: few, read whole."),
		append([]string{CodeInvalid}, locks...), func(ctx context.Context, in *auditPurgesInput) (*auditPurgesOutput, error) {
			p, _ := authn.From(ctx)
			out := &auditPurgesOutput{Body: AuditPurges{Items: []AuditPurge{}}}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				trail, err := u.Audit().Boundaries(ctx)
				if err != nil {
					return err
				}
				ledger, err := u.Retention().LedgerBoundaries(ctx)
				if err != nil {
					return err
				}
				for _, b := range trail {
					seq, at := b.LastSeq, b.LastOccurredAt.UTC().Format(time.RFC3339)
					out.Body.Items = append(out.Body.Items, AuditPurge{Kind: "trail", LastSeq: &seq, LastOccurredAt: &at, Approval: b.Approval,
						Counts: b.Counts, AnchorKey: b.AnchorKey, WrittenAt: b.WrittenAt.UTC().Format(time.RFC3339)})
				}
				for _, b := range ledger {
					day, rev := b.Boundary.Format(time.DateOnly), b.CarriedRevision
					out.Body.Items = append(out.Body.Items, AuditPurge{Kind: "ledger", Boundary: &day, CarriedRevision: &rev, Approval: b.Approval,
						Counts: b.Counts, AnchorKey: b.AnchorKey, WrittenAt: b.WrittenAt.UTC().Format(time.RFC3339)})
				}
				slices.SortStableFunc(out.Body.Items, func(x, y AuditPurge) int { return strings.Compare(x.WrittenAt, y.WrittenAt) })
				if in.RecordID == "" {
					return nil
				}
				id, err := uuid.Parse(in.RecordID) // huma checked its format
				if err != nil {
					return invalid([]FieldProblem{{Code: CodeInvalid, Field: "query.record_id", Message: "is not an ID", Fix: "Send the record's ID"}})
				}
				gone, err := u.Ledger().PurgedOfSource(ctx, id)
				for _, g := range gone {
					out.Body.Postings = append(out.Body.Postings, PurgedPosting{PostingIdentity: g.PostingIdentity, SourceType: g.Source.Type,
						PostingDate: g.PostingDate.Format(time.DateOnly), LedgerRevision: g.LedgerRevision, Reversal: g.ReversesEntryID != nil,
						Boundary: g.Boundary.Format(time.DateOnly)})
				}
				return err
			})
			return out, err
		})
}
