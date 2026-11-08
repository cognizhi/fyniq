package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ Restores behind the anchors (AUD-09, design D8)

// AuditRecovery is a recovery of the audit trail from a restore behind its anchors.
type AuditRecovery struct {
	ID             string  `json:"id" format:"uuid" doc:"The recovery's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Epoch          int     `json:"epoch" doc:"The anchor epoch it started: anchors written since are under this epoch." example:"1"`
	RestoredSeq    int64   `json:"restored_seq" doc:"The restored head: the last event whose hash matches every anchor; the chain continues from it." example:"4120"`
	MissingFrom    int64   `json:"missing_from" doc:"The first event the anchors signed that the chain no longer proves." example:"4121"`
	MissingTo      int64   `json:"missing_to" doc:"The last such event: verification explains the range from missing_from to it." example:"4388"`
	DetectedAt     string  `json:"detected_at" format:"date-time" doc:"When the recovery step found the restore." example:"2026-11-10T02:00:04Z"`
	AnchorKey      string  `json:"anchor_key" doc:"Where its signed copy is in the anchors' write-once bucket." example:"audit-anchors/01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10/recovery/1.json"`
	Runbook        string  `json:"runbook" doc:"What to follow before acknowledging it." example:"docs/runbooks/audit-restore.md"`
	Version        int     `json:"version" doc:"1 while it waits, 2 once acknowledged: its ETag is the version in quotes, for If-Match." example:"1"`
	AcknowledgedBy *string `json:"acknowledged_by" format:"uuid" doc:"The Owner who acknowledged it; null while it waits, and the check of the audit trail (C.2 #3) fails." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e11"`
	AcknowledgedAt *string `json:"acknowledged_at" format:"date-time" doc:"When; null while it waits." example:"2026-11-10T09:12:00Z"`
}

// AuditRecoveries are the trail's recoveries, oldest first.
type AuditRecoveries struct {
	Items []AuditRecovery `json:"items" nullable:"false" doc:"Every recovery, oldest first; none for a trail never restored behind its anchors."`
}

// AuditRecoveryPath names a recovery.
type AuditRecoveryPath struct {
	RecoveryID string `path:"recovery_id" format:"uuid" doc:"The recovery's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
}

type auditRecoveriesOutput struct{ Body AuditRecoveries }

type auditRecoveryAcknowledgeInput struct {
	AuditRecoveryPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type auditRecoveryOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The recovery's state, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     AuditRecovery
}

func auditRecoveryOf(r storage.AuditRecovery) AuditRecovery {
	out := AuditRecovery{ID: r.ID.String(), Epoch: r.Epoch, RestoredSeq: r.RestoredSeq, MissingFrom: r.MissingFrom, MissingTo: r.MissingTo,
		DetectedAt: r.DetectedAt.UTC().Format(time.RFC3339), AnchorKey: r.AnchorKey, Runbook: audit.Runbook, Version: 1}
	if r.AcknowledgedBy != nil {
		by, at := r.AcknowledgedBy.String(), r.AcknowledgedAt.UTC().Format(time.RFC3339)
		out.AcknowledgedBy, out.AcknowledgedAt, out.Version = &by, &at, 2
	}
	return out
}

// recoveryETag is a recovery's version as If-Match names it: 1 waiting, 2 acknowledged.
func recoveryETag(r storage.AuditRecovery) string { return etag(auditRecoveryOf(r).Version) }

func registerAuditRecoveries(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)
	register(a, authz.ListAuditRecoveries, exempt(huma.Operation{
		OperationID: "listAuditRecoveries", Method: http.MethodGet, Path: "/v1/audit/recoveries", Tags: []string{"Audit trail"},
		Summary: "List the audit trail's recoveries from restores behind its anchors",
		Description: "Every recovery of the organisation's audit trail (AUD-09, design D8), oldest first. A restore from a backup that " +
			"landed behind an anchor already published is found by the recovery step, which the nightly verification runs first: it " +
			"records the restored head, the anchored range no longer proven, and its signed copy, and starts the next anchor epoch. " +
			"Verification explains that range; the check of the audit trail (C.2 #3) fails until the Owner acknowledges the recovery. " +
			"Changes nothing.",
	}, "cursor", "A trail's recoveries are one per restore behind its anchors: few, and read whole, as the check lists each one waiting."),
		locks, func(ctx context.Context, _ *struct{}) (*auditRecoveriesOutput, error) {
			p, _ := authn.From(ctx)
			out := &auditRecoveriesOutput{Body: AuditRecoveries{Items: []AuditRecovery{}}}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				recs, err := u.Audit().Recoveries(ctx)
				for _, r := range recs {
					out.Body.Items = append(out.Body.Items, auditRecoveryOf(r))
				}
				return err
			})
			return out, err
		})

	register(a, authz.AcknowledgeAuditRecovery, huma.Operation{
		OperationID: "acknowledgeAuditRecovery", Method: http.MethodPost, Path: "/v1/audit/recoveries/{recovery_id}/acknowledge", Tags: []string{"Audit trail"},
		Summary: "Acknowledge a recovery of the audit trail",
		Description: "Records that you, the Owner, followed the restore runbook (docs/runbooks/audit-restore.md) for a recovery of the " +
			"audit trail from a restore behind its anchors (AUD-09, design D8), once. The check of the audit trail (C.2 #3) passes again " +
			"once every recovery is acknowledged; verification still explains the range the recovery no longer proves. Nobody holds A on " +
			"\"Audit trail\", so the Owner acknowledges for now. Needs a person signed in to Fyniq; never the AI. Side effects: the " +
			"acknowledgement, audited (audit.restore_acknowledged). Needs If-Match with its ETag. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeSessionRequired, audit.CodeAcknowledged}, writes...),
		func(ctx context.Context, in *auditRecoveryAcknowledgeInput) (*auditRecoveryOutput, error) {
			id, err := parseID("recovery_id", in.RecoveryID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			if p.Via != authn.ViaSession || p.AI() || p.UserID == uuid.Nil {
				return nil, problem(CodeSessionRequired, "Acknowledging a recovery of the audit trail needs a person with a Fyniq user, signed in to Fyniq.",
					"Sign in to Fyniq in the browser and do it there.")
			}
			if in.IfMatch == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AuditRecovery, error) {
				recs, err := u.Audit().Recoveries(ctx)
				if err != nil {
					return AuditRecovery{}, err
				}
				i := slices.IndexFunc(recs, func(r storage.AuditRecovery) bool { return r.ID == id })
				if i < 0 {
					return AuditRecovery{}, problem(CodeNotFound, "No recovery of the audit trail has that ID.", "List the recoveries to find its ID")
				}
				if err := checkIfMatch(in.IfMatch, recoveryETag(recs[i])); err != nil {
					return AuditRecovery{}, err
				}
				if recs[i].AcknowledgedBy != nil {
					return AuditRecovery{}, problem(audit.CodeAcknowledged, "The recovery was acknowledged already.", "Nothing to do")
				}
				r, err := u.Audit().AcknowledgeRecovery(ctx, id, p.UserID, a.deps.Clock.Now())
				if errors.Is(err, ports.ErrNotFound) {
					return AuditRecovery{}, problem(audit.CodeAcknowledged, "The recovery was acknowledged already.", "Nothing to do")
				}
				return auditRecoveryOf(r), err
			})
			if err != nil {
				return nil, err
			}
			out := &auditRecoveryOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.ETag, out.Replayed = etag(w.Body.Version), replayedHeader(w.Replayed)
			}
			return out, nil
		})
}
