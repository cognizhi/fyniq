//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The document inbox keeps what each captured file is, its reading and its draft, and the database allows only the lifecycle: received,
// reading, read, drafted (or dismissed); a failed one is read again; what the file is never changes; a row is never deleted; a drafted or
// dismissed one is final; only the API writes, and a reading cannot be recorded without the capture being Reading first.
// req: FR-DOC-14
// req: FR-DOC-12
// asvs: V8.2.2
func TestTheDocumentInboxKeepsTheLifecycleInTheDatabase(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Document inbox Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC()
	user := uuid.New()
	var c storage.DocumentCapture
	file := func(sha string, status string, dup *uuid.UUID) storage.NewDocumentCapture {
		return storage.NewDocumentCapture{Source: storage.CaptureUpload, FileName: "AU-1182.pdf", MediaType: "application/pdf", Size: 1200, SHA256: sha,
			BlobKey: "capture/" + b.Org.String() + "/" + uuid.NewString(), CapturedBy: "user:" + user.String(), Status: status, DuplicateOf: dup, At: now}
	}
	sha := func(c string) string { return strings.Repeat(c, 64) }
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		c, err = u.DocumentCaptures().Capture(ctx, file(sha("a"), storage.CaptureReceived, nil))
		return err
	}); err != nil || c.Status != storage.CaptureReceived || c.Version != 1 {
		t.Fatalf("capturing: %+v %v", c, err)
	}
	// received -> reading -> read, with the reading and its checks as data
	read := storage.CapturedReading{Kind: "bill", Reading: json.RawMessage(`{"supplier":{"value":"Acme","confidence":0.9}}`), Model: "m", Prompt: "document_capture@1",
		Checks: json.RawMessage(`[]`), At: now}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r := u.DocumentCaptures()
		x, err := r.Status(ctx, c.ID, c.Version, storage.CaptureReading, "")
		if err != nil {
			return err
		}
		if x, err = r.Reading(ctx, c.ID, x.Version, read); err != nil {
			return err
		}
		if x.Status != storage.CaptureRead || x.Readings != 1 || x.Kind != "bill" {
			t.Errorf("after the reading: %+v", x)
		}
		c = x
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A version that moved is refused, not overwritten.
	err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.DocumentCaptures().Dismiss(ctx, c.ID, c.Version-1, user, "stale", now)
		return err
	})
	if e, ok := err.(*storage.Error); !ok || e.Code != storage.CodeVersion {
		t.Errorf("a stale dismissal: %v", err)
	}
	// The same file again is a duplicate of the first, found by its hash.
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, ok, err := u.DocumentCaptures().OfHash(ctx, user, sha("a"))
		if err != nil || !ok || o.ID != c.ID {
			t.Errorf("the capture of the hash: %+v %v %v", o, ok, err)
		}
		d, err := u.DocumentCaptures().Capture(ctx, file(sha("a"), storage.CaptureDuplicate, &c.ID))
		if err != nil || d.Status != storage.CaptureDuplicate || d.DuplicateOf == nil || *d.DuplicateOf != c.ID {
			t.Errorf("the duplicate: %+v %v", d, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A member's own mail is seen by that member alone; the organisation's by everyone.
	other := uuid.New()
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		n := file(sha("b"), storage.CaptureReceived, nil)
		mid, fid := uuid.New(), uuid.New()
		n.Source, n.MessageID, n.InboundFileID, n.OwnerUserID = storage.CaptureEmail, &mid, &fid, &user
		if _, err := u.DocumentCaptures().Capture(ctx, n); err != nil {
			return err
		}
		for who, want := range map[uuid.UUID]int{user: 3, other: 2} { // the first file, its duplicate, and the member's own
			got, err := u.DocumentCaptures().List(ctx, who, "", uuid.Nil, 50)
			if err != nil || len(got) != want {
				t.Errorf("what %s sees: %d captures (want %d) %v", who, len(got), want, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The database's guard: the lifecycle, what the file is, deletion, finality, and who writes.
	drafted := uuid.New()
	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_migrator", "delete a capture", "DELETE FROM fyniq.document_capture WHERE id = $1", "FY950"},
		{"fyniq_migrator", "change the file's hash", "UPDATE fyniq.document_capture SET sha256 = repeat('f', 64) WHERE id = $1", "FY950"},
		{"fyniq_migrator", "change who captured it", "UPDATE fyniq.document_capture SET captured_by = 'user:someone' WHERE id = $1", "FY950"},
		{"fyniq_api", "draft as the API without a status", "UPDATE fyniq.document_capture SET draft_kind = 'bill', draft_id = '" + drafted.String() + "', drafted_by = '" + user.String() + "', drafted_at = now() WHERE id = $1", "FY950"},
		{"fyniq_worker", "write as the worker", "UPDATE fyniq.document_capture SET status = 'dismissed' WHERE id = $1", "42501"},
		{"fyniq_api", "change the blob key", "UPDATE fyniq.document_capture SET blob_key = 'capture/x/y' WHERE id = $1", "42501"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt, c.ID); return err })
		wantState(t, x.what, err, x.state, x.state)
	}
	// read -> drafted, once; a drafted capture is final.
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		x, err := u.DocumentCaptures().Drafted(ctx, c.ID, c.Version, storage.CaptureDraftBill, drafted, user, now)
		if err != nil || x.Status != storage.CaptureDrafted || x.DraftID == nil || *x.DraftID != drafted {
			t.Errorf("drafting: %+v %v", x, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err = asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.document_capture SET status = 'dismissed' WHERE id = $1", c.ID)
		return err
	})
	wantState(t, "dismiss a drafted capture", err, "FY950", "FY950")
}
