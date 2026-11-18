//go:build local

package postgres_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/fieldseal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/reportexport"
)

// The full tenant export's catalogue (FR-MIG-04, design D10): every table of the schema that holds an organisation_id
// is exported or excluded, never both, and the catalogue names no table the schema lacks; each column it drops exists.
// The worker reads every exported table through tenant_rows, under its grants and row-level security: only its
// organisation's rows, and no table outside the organisation's. The API's role cannot call it.
// req: FR-MIG-04
// req: QA-ENG-05
// asvs: V8.2.2
func TestTheTenantExportCataloguesEveryTable(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: uuid.NewString()}})
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	a, other := books(t, "Catalogue A Pte Ltd"), books(t, "Catalogue B Pte Ltd")
	var exported []string
	if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		tables, err := u.TenantData().Tables(ctx)
		if err != nil {
			return err
		}
		names := map[string][]string{}
		var read []string
		defer func() { exported = read }()
		for _, tb := range tables {
			names[tb.Name] = tb.Columns
			_, excluded := reportexport.Excluded[tb.Name]
			exported := slices.Contains(reportexport.Exported, tb.Name)
			if exported == excluded {
				t.Errorf("the table %s is %s", tb.Name, map[bool]string{true: "both exported and excluded", false: "neither exported nor excluded"}[exported])
			}
			if exported {
				read = append(read, tb.Name)
			}
		}
		for _, n := range reportexport.Exported {
			if names[n] == nil {
				t.Errorf("the catalogue exports %s, which is not a table of the organisation", n)
			}
		}
		for n := range reportexport.Excluded {
			if names[n] == nil {
				t.Errorf("the catalogue excludes %s, which is not a table of the organisation", n)
			}
		}
		for n, cols := range reportexport.Dropped {
			for c := range cols {
				if !slices.Contains(names[n], c) {
					t.Errorf("the catalogue drops %s.%s, which is not a column", n, c)
				}
			}
		}
		rows, err := u.TenantData().Rows(ctx, "account", nil, 100)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !strings.Contains(r, a.Org.String()) || strings.Contains(r, other.Org.String()) {
				t.Errorf("a row of another organisation's: %s", r)
			}
		}
		if len(rows) == 0 {
			t.Error("the organisation's accounts read none")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range exported { // each in its own transaction, so one refusal hides no other
		if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.TenantData().Rows(ctx, name, nil, 1)
			return err
		}); err != nil {
			t.Errorf("the worker reads %s: %v", name, err)
		}
	}
	if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.TenantData().Rows(ctx, "app_user", nil, 1)
		return err
	}); err == nil {
		t.Error("tenant_rows read app_user, a table outside the organisation's")
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.TenantData().Rows(ctx, "account", nil, 1)
		return err
	}); err == nil {
		t.Error("the API's role called tenant_rows")
	}
}

// The full tenant export's file (FR-MIG-04, design D10), written by the worker: each exported table as CSV with its
// rows, nothing of an excluded table, a vendor's bank account number in clear in place of its sealed bytes, the
// evidence's file, the documents' versions, and the audit trail as JSON lines whose chain verifies to the head the
// manifest names; every file's SHA-256 and size in the manifest match the archive's.
// req: FR-MIG-04
// asvs: V14.2.1
func TestTheTenantExportWritesTheOrganisation(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: uuid.NewString()}})
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	b := newFinancialBooks(t, ctx, api, "Tenant Export Pte Ltd")
	b.post(t, ctx, api, ledger.SourceOpeningBalance, 10, 1, "1010", "3010", "10000.00")
	b.post(t, ctx, api, ledger.SourceManualJournal, 10, 3, "1100", "4010", "5000.00")
	sealer := fieldseal.Sealer{KEK: fieldseal.AESKEK{Name: "test", Key: bytes.Repeat([]byte{7}, 32)},
		MAC: func(context.Context, []byte) ([]byte, error) { return make([]byte, 32), nil }}
	files, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var evidence storage.Evidence
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		vendor, err := u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Northwind Pte Ltd", Vendor: true})
		if err != nil {
			return err
		}
		fp := bytes.Repeat([]byte{1}, 32)
		payload, _ := json.Marshal(map[string]string{"contact_id": vendor.ID.String(), "currency": "SGD", "number_last4": "6789",
			"number_fingerprint": hex.EncodeToString(fp)})
		change, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentPayeeBankChange, Payload: payload})
		if err != nil {
			return err
		}
		sealed, err := sealer.Seal(ctx, []byte("0123456789"), payee.AAD(b.Org, change.ID, vendor.ID, "SGD"))
		if err != nil {
			return err
		}
		if _, err := u.PayeeBankAccounts().Add(ctx, storage.PayeeBankAccount{ChangeID: change.ID, ChangeVersion: 1, ContactID: vendor.ID,
			Currency: "SGD", Sealed: sealed, Last4: "6789", Fingerprint: fp}); err != nil {
			return err
		}
		ev := newEvidence(b.Org, change.ID, "invoice")
		if err := files.Put(ctx, ev.StagingKey, strings.NewReader("the receipt's bytes")); err != nil {
			return err
		}
		evidence, err = u.Evidence().Attach(ctx, ev)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	var f reportexport.File
	created := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		f, err = reportexport.WriteTenant(ctx, u, reportexport.TenantSources{Staging: files, Evidence: files, Fields: sealer}, created, "v0.1.0 (test)", &buf)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if f.Name != "fyniq-export-2026-11-13.zip" || f.MediaType != "application/zip" || f.Rows == 0 {
		t.Errorf("the file: %+v", f)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		t.Helper()
		for _, zf := range zr.File {
			if zf.Name == name {
				r, err := zf.Open()
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				b, err := io.ReadAll(r)
				if err != nil {
					t.Fatal(err)
				}
				return string(b)
			}
		}
		t.Fatalf("the export has no %s", name)
		return ""
	}
	var m reportexport.Manifest
	if err := json.Unmarshal([]byte(read("manifest.json")), &m); err != nil {
		t.Fatal(err)
	}
	if m.Schema != reportexport.TenantSchema || m.OrganisationID != b.Org.String() || m.Platform != "v0.1.0 (test)" || m.LedgerRevision != f.Revision ||
		m.AuditHead.Seq == 0 {
		t.Errorf("the manifest: %+v", m)
	}
	listed := map[string]bool{"manifest.json": true}
	for _, mf := range m.Files {
		listed[mf.Path] = true
		data := read(mf.Path)
		sum := sha256.Sum256([]byte(data))
		if hex.EncodeToString(sum[:]) != mf.SHA256 || int64(len(data)) != mf.Bytes {
			t.Errorf("%s: the manifest says %s, %d bytes; the archive holds %x, %d bytes", mf.Path, mf.SHA256, mf.Bytes, sum, len(data))
		}
	}
	for _, zf := range zr.File {
		if !listed[zf.Name] {
			t.Errorf("%s is in the archive but not the manifest", zf.Name)
		}
	}
	for name := range reportexport.Excluded {
		if listed["tables/"+name+".csv"] {
			t.Errorf("the excluded table %s is exported", name)
		}
	}
	if accounts := read("tables/account.csv"); !strings.Contains(accounts, `"1010"`) || !strings.Contains(accounts, `"DBS current"`) {
		t.Errorf("tables/account.csv: %s", accounts)
	}
	payees := read("tables/payee_bank_account.csv")
	if !strings.Contains(payees, `"0123456789"`) || strings.Contains(payees, "number_sealed") || strings.Contains(payees, "number_fingerprint") {
		t.Errorf("tables/payee_bank_account.csv: %s", payees)
	}
	if got := read("files/evidence/" + evidence.ID.String() + "/receipt.pdf"); got != "the receipt's bytes" {
		t.Errorf("the evidence's file: %q", got)
	}
	if !strings.Contains(read("documents/versions.jsonl"), `"payload"`) || !strings.Contains(read("README.md"), "bank account numbers in clear") {
		t.Error("the documents' versions or the README")
	}
	// The audit trail verifies as contracts/audit says, to the head the manifest names.
	running := make([]byte, 32)
	lines := strings.Split(strings.TrimSpace(read("audit/events.jsonl")), "\n")
	for i, l := range lines {
		var e struct {
			Seq      int64  `json:"seq"`
			PrevHash string `json:"prev_hash"`
			Hash     string `json:"hash"`
			Body     string `json:"body"`
		}
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatal(err)
		}
		if e.PrevHash != hex.EncodeToString(running) || e.Seq != int64(i+1) {
			t.Fatalf("event %d's link: %s", i+1, l)
		}
		sum := sha256.Sum256(append(slices.Clone(running), []byte(e.Body)...))
		if hex.EncodeToString(sum[:]) != e.Hash {
			t.Fatalf("event %d's hash: %s", e.Seq, l)
		}
		running = sum[:]
	}
	if hex.EncodeToString(running) != m.AuditHead.Hash || int64(len(lines)) != m.AuditHead.Seq {
		t.Errorf("the chain ends at %x after %d events; the manifest says %s at %d", running, len(lines), m.AuditHead.Hash, m.AuditHead.Seq)
	}
}
