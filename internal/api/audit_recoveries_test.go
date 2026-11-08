//go:build local

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// recordRecovery records a recovery of e's trail, as the recovery step would after a restore behind its anchors.
func recordRecovery(t *testing.T, e *env) storage.AuditRecovery {
	t.Helper()
	var rec storage.AuditRecovery
	if err := workerStore(t).Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		recs, err := u.Audit().Recoveries(ctx)
		if err != nil {
			return err
		}
		rec, err = u.Audit().RecordRecovery(ctx, storage.NewAuditRecovery{Epoch: len(recs) + 1, RestoredSeq: 4, RestoredHash: make([]byte, 32),
			MissingFrom: 5, MissingTo: 9, DetectedAt: time.Date(2026, 11, 10, 2, 0, 0, 0, time.UTC),
			AnchorKey: audit.RecoveryKey(e.books.Org, len(recs)+1)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return rec
}

// A recovery of the audit trail from a restore behind its anchors (AUD-09, design D8): listed for those with V on "Audit
// trail", with what it no longer proves and the runbook; acknowledged once, by the Owner, signed in, against its ETag.
// Nobody else may, as nobody holds A on the row.
// req: AUD-09
// req: FR-CTL-03
// asvs: V8.2.1, V16.4.2
func TestARecoveryIsListedAndAcknowledgedByTheOwner(t *testing.T) {
	e := newEnv(t)
	rec := recordRecovery(t, e)
	list := e.call(http.MethodGet, "/v1/audit/recoveries", e.as(authz.Auditor), nil)
	items, _ := list.Body["items"].([]any)
	if list.Status != 200 || len(items) != 1 {
		t.Fatalf("the recoveries: %d %s", list.Status, list.Raw)
	}
	if got := items[0].(map[string]any); got["id"] != rec.ID.String() || got["missing_from"].(float64) != 5 || got["missing_to"].(float64) != 9 ||
		got["acknowledged_by"] != nil || got["runbook"] != audit.Runbook {
		t.Errorf("the recovery: %v", got)
	}
	path := "/v1/audit/recoveries/" + rec.ID.String() + "/acknowledge"
	owner := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: e.books.Org, Roles: []authz.Role{authz.Owner},
		UserID: uuid.New()})
	e.problemIs(e.call(http.MethodPost, path, e.as(authz.Auditor), nil, "If-Match", etag(1)), "acknowledgeAuditRecovery", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, path, e.as(authz.DPO), nil, "If-Match", etag(1)), "acknowledgeAuditRecovery", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, path, owner, nil), "acknowledgeAuditRecovery", 428, CodeIfMatchRequired)
	e.problemIs(e.call(http.MethodPost, path, owner, nil, "If-Match", etag(2)), "acknowledgeAuditRecovery", 412, CodeStale)
	e.problemIs(e.call(http.MethodPost, "/v1/audit/recoveries/"+uuid.NewString()+"/acknowledge", owner, nil, "If-Match", etag(1)),
		"acknowledgeAuditRecovery", 404, CodeNotFound)
	done := e.call(http.MethodPost, path, owner, nil, "If-Match", etag(1))
	if done.Status != 200 || done.str("acknowledged_by") == "" || done.Header.Get("ETag") != etag(2) {
		t.Fatalf("the acknowledgement: %d %s", done.Status, done.Raw)
	}
	e.problemIs(e.call(http.MethodPost, path, owner, nil, "If-Match", etag(2)), "acknowledgeAuditRecovery", 409, audit.CodeAcknowledged)
}

// matrixAuditRecoveries are the authorisation matrix's rows for restores behind the anchors (AUD-09).
var matrixAuditRecoveries = map[string]func(e *env, tok string) (response, func() bool){
	"listAuditRecoveries": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/audit/recoveries", tok, nil), nil
	},
	"acknowledgeAuditRecovery": func(e *env, tok string) (response, func() bool) {
		rec := recordRecovery(e.t, e)
		r := e.call(http.MethodPost, "/v1/audit/recoveries/"+rec.ID.String()+"/acknowledge", steppedUp(e, tok), nil, "If-Match", etag(1))
		return r, func() bool { // still waiting
			var waiting bool
			_ = workerStore(e.t).Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
				recs, err := u.Audit().Recoveries(ctx)
				for _, x := range recs {
					if x.ID == rec.ID {
						waiting = x.AcknowledgedBy == nil
					}
				}
				return err
			})
			return waiting
		}
	},
}
