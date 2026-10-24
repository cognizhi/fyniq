//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func newEvidence(org, doc uuid.UUID, typ string) storage.NewEvidence {
	h := strings.Repeat("ab", 32)
	return storage.NewEvidence{DocumentID: doc, EvidenceType: typ, FileName: "receipt.pdf", MediaType: "application/pdf", Size: 42,
		SHA256: h, OriginalSHA256: h, Source: storage.SourceUpload, DataClass: storage.ClassGeneral,
		StagingKey: "staging/" + org.String() + "/" + uuid.Must(uuid.NewV7()).String(), UploadedBy: "user:test"}
}

func attach(t *testing.T, s storage.Store, org, doc uuid.UUID, typ string) storage.Evidence {
	t.Helper()
	var e storage.Evidence
	if err := s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		e, err = u.Evidence().Attach(ctx, newEvidence(org, doc, typ))
		return err
	}); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	return e
}

// Evidence through the storage port, end to end: attached with its hashes, uploader, source and parent in
// the audit trail; removed only while its journal is a draft; committed by the worker once the journal is
// posted, and append-only from then on.
// req: FR-DOC-07
// req: FR-DOC-13
// req: CTL-AUD-06
// asvs: V16.2.1
func TestEvidenceIsAppendOnlyOnceItsRecordIsFinal(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	b := books(t, "Evidence Pte Ltd")
	svc := journal.Service{Store: api}
	doc, err := svc.Create(ctx, b.Org, saleDraft(t, b, porttest.Day(10, 14), "250.00"))
	if err != nil {
		t.Fatal(err)
	}
	draftFile, kept := attach(t, api, b.Org, doc.ID, "quote"), attach(t, api, b.Org, doc.ID, "receipt")
	var events int
	err = asOrg(ctx, conn(t, "fyniq_readonly"), b.Org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM fyniq.audit_event WHERE action = 'evidence.attached'
		  AND body::jsonb->'changes'->'sha256'->>1 = $1 AND body::jsonb->'changes'->'document_id'->>1 = $2
		  AND body::jsonb->'changes'->'source'->>1 = 'upload' AND body::jsonb->'changes'->'uploaded_by'->>1 = 'user:test'`,
			strings.Repeat("ab", 32), doc.ID.String()).Scan(&events)
	})
	if err != nil || events != 2 {
		t.Fatalf("audit events with the hash, parent, source and uploader: %d, %v; want 2", events, err)
	}
	// While the journal is a draft a file can be removed, and nothing can be committed.
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		e, err := u.Evidence().Remove(ctx, draftFile.ID, draftFile.Version)
		if err == nil && (e.State != storage.EvidenceRemoved || e.RemovedAt == nil) {
			t.Errorf("removed: %+v", e)
		}
		return err
	}); err != nil {
		t.Fatalf("Remove from a draft: %v", err)
	}
	until := time.Date(2034, 1, 1, 0, 0, 0, 0, time.UTC)
	key := "evidence/" + b.Org.String() + "/" + kept.ID.String()
	err = worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Evidence().Finalise(ctx, kept.ID, key, until)
		return err
	})
	if storage.CodeOf(err) != storage.CodeEvidence {
		t.Fatalf("Finalise on a draft: %v; want %s", err, storage.CodeEvidence)
	}
	if _, err := svc.Post(ctx, b.Org, doc.ID); err != nil {
		t.Fatal(err)
	}
	// Posted: removal is refused; the API's role cannot commit; the worker can, once.
	err = api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Evidence().Remove(ctx, kept.ID, kept.Version)
		return err
	})
	if storage.CodeOf(err) != storage.CodeEvidence {
		t.Fatalf("Remove from a posted journal: %v; want %s", err, storage.CodeEvidence)
	}
	var waiting []storage.Evidence
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		waiting, err = u.Evidence().Unfinalised(ctx, 10)
		return err
	}); err != nil || len(waiting) != 1 || waiting[0].ID != kept.ID {
		t.Fatalf("Unfinalised = %v, %v; want the kept file only", waiting, err)
	}
	err = api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Evidence().Finalise(ctx, kept.ID, key, until)
		return err
	})
	if err == nil {
		t.Fatal("the API's role committed a file: only the worker may")
	}
	var final storage.Evidence
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		final, err = u.Evidence().Finalise(ctx, kept.ID, key, until)
		return err
	}); err != nil || final.State != storage.EvidenceFinal || final.LockedKey != key || !final.RetainUntil.Equal(until) {
		t.Fatalf("Finalise: %+v, %v", final, err)
	}
	// The database refuses every other change, whoever asks.
	for _, role := range allRoles {
		c := conn(t, role)
		for _, stmt := range []string{
			"UPDATE fyniq.evidence SET state = 'removed', removed_at = now(), version = version + 1 WHERE state = 'final'",
			"UPDATE fyniq.evidence SET state = 'attached', version = version + 1 WHERE state = 'removed'",
			"UPDATE fyniq.evidence SET retain_until = retain_until - interval '1 day', version = version + 1 WHERE state = 'final'",
			"UPDATE fyniq.evidence SET sha256 = repeat('cd', 32), version = version + 1",
			"UPDATE fyniq.evidence SET locked_key = staging_key, version = version + 1 WHERE state = 'final'",
			"DELETE FROM fyniq.evidence",
			"TRUNCATE fyniq.evidence",
			"TRUNCATE fyniq.evidence CASCADE", // with the sends that name its files (FR-AR-06)
		} {
			err := asOrg(ctx, c, b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
			// 0A000: a plain TRUNCATE stops at the foreign keys of document_send before any trigger runs.
			wantState(t, role+": "+stmt, err, "42501", "FY120", "FY121", "FY122", "FY123", "FY124", "23514", "0A000")
		}
	}
	// The retain-until moves later, by the worker, audited.
	err = asOrg(ctx, conn(t, "fyniq_worker"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.evidence SET retain_until = retain_until + interval '1 year', version = version + 1 WHERE state = 'final'")
		return err
	})
	wantState(t, "extend the retain-until without an audit event", err, "FY061")
	if n := count(t, b.Org, "evidence"); n != 2 {
		t.Errorf("%d evidence rows after the attacks; want 2", n)
	}
}

// Evidence of one organisation is invisible to another, and cannot be attached to its documents.
// req: FR-DOC-07
// req: SEC-02
// asvs: V8.2.2
func TestEvidenceStaysInItsOrganisation(t *testing.T) {
	ctx := context.Background()
	s := store(t, "fyniq_api")
	a, other := books(t, "Evidence A Pte Ltd"), books(t, "Evidence B Pte Ltd")
	doc, err := journal.Service{Store: s}.Create(ctx, a.Org, saleDraft(t, a, porttest.Day(10, 14), "10.00"))
	if err != nil {
		t.Fatal(err)
	}
	e := attach(t, s, a.Org, doc.ID, "receipt")
	if err := s.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Evidence().Get(ctx, e.ID); err == nil {
			t.Error("another organisation read the file")
		}
		_, err := u.Evidence().Attach(ctx, newEvidence(other.Org, doc.ID, "forged"))
		return err
	}); err == nil {
		t.Fatal("a file was attached to another organisation's journal")
	}
}
