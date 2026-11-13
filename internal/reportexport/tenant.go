package reportexport

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/domain/paymentbatch"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// Tenant is the full tenant export (FR-MIG-04, design D10): the organisation whole, for exit and portability, as one ZIP
// file. Each table the catalogue exports is tables/<name>.csv (RFC 4180, decimal strings, ISO dates, no cell a
// spreadsheet reads as a formula); each document's versions are a line of documents/versions.jsonl; the evidence and
// the released payment files are under files/; the audit trail is audit/events.jsonl, its hashes verifiable outside
// Fyniq (contracts/audit); manifest.json names each file with its rows, size and SHA-256, the platform version, the
// ledger revision and the audit chain's head; README.md says how to read and verify it. Sealed values are in clear:
// the export is the organisation's own data, under dual control (design D10 [counsel to confirm]).
const Tenant = "tenant"

// TenantSchema is the export's format, which its manifest names.
const TenantSchema = "fyniq-tenant-export@1"

// TenantSources are what the tenant export reads besides the database: the evidence store and the staging store that
// keep the organisation's files, and the cipher that opens its sealed fields. A nil store's files are listed as missing.
type TenantSources struct {
	Evidence, Staging ports.BlobStore
	Fields            ports.Cipher
}

// Manifest is the export's manifest.json.
type Manifest struct {
	Schema         string         `json:"schema"`
	OrganisationID string         `json:"organisation_id"`
	CreatedAt      string         `json:"created_at"`
	Platform       string         `json:"platform"`
	LedgerRevision int64          `json:"ledger_revision"`
	AuditHead      ManifestHead   `json:"audit_head"`
	Files          []ManifestFile `json:"files"`
	Excluded       []ManifestOut  `json:"excluded"`
	// Missing are the files the records name that the stores no longer hold, such as evidence removed under retention.
	Missing []string `json:"missing"`
}

// ManifestHead is the audit chain's head when the export read it: its sequence number and hash, in hex.
type ManifestHead struct {
	Seq  int64  `json:"seq"`
	Hash string `json:"hash"`
}

// ManifestFile is a file of the export: its path, its rows (a table or a JSON lines file), its size and SHA-256 in hex.
type ManifestFile struct {
	Path   string `json:"path"`
	Rows   *int   `json:"rows,omitempty"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// ManifestOut is a table, or a column of one, the export leaves out, and why.
type ManifestOut struct {
	Table  string `json:"table"`
	Column string `json:"column,omitempty"`
	Why    string `json:"why"`
}

// tenantPage is how many rows the export reads at a time.
const tenantPage = 1000

// WriteTenant writes the organisation's full export to w as it reads it, and is its file: its name, its rows (the
// tables' rows) and the ledger revision it read at. stamp is the platform version and build that prepare it (REL-05).
func WriteTenant(ctx context.Context, u storage.UnitOfWork, src TenantSources, created time.Time, stamp string, w io.Writer) (File, error) {
	org, err := u.Organisation(ctx)
	if err != nil {
		return File{}, err
	}
	f := File{Name: "fyniq-export-" + created.UTC().Format(time.DateOnly) + ".zip", MediaType: "application/zip"}
	if f.Revision, err = u.Ledger().Revision(ctx); err != nil {
		return File{}, err
	}
	head, err := u.Audit().Head(ctx)
	if err != nil {
		return File{}, err
	}
	z := &zipper{zw: zip.NewWriter(w), created: created, m: Manifest{Schema: TenantSchema, OrganisationID: org.ID.String(),
		CreatedAt: created.UTC().Format(time.RFC3339), Platform: stamp, LedgerRevision: f.Revision,
		AuditHead: ManifestHead{Seq: head.Seq, Hash: hex.EncodeToString(head.Hash)}, Files: []ManifestFile{}, Excluded: []ManifestOut{},
		Missing: []string{}}}
	tables, err := u.TenantData().Tables(ctx)
	if err != nil {
		return File{}, err
	}
	t := tenantWriter{u: u, src: src, org: org.ID, z: z}
	for _, tb := range tables {
		if why, ok := Excluded[tb.Name]; ok {
			z.m.Excluded = append(z.m.Excluded, ManifestOut{Table: tb.Name, Why: why})
			continue
		}
		if !slices.Contains(Exported, tb.Name) {
			return File{}, fmt.Errorf("reportexport: the tenant export's catalogue neither exports nor excludes the table %s", tb.Name)
		}
		n, err := t.table(ctx, tb)
		if err != nil {
			return File{}, err
		}
		f.Rows += n
	}
	for table, cols := range Dropped {
		for col, why := range cols {
			z.m.Excluded = append(z.m.Excluded, ManifestOut{Table: table, Column: col, Why: why})
		}
	}
	slices.SortFunc(z.m.Excluded, func(a, b ManifestOut) int { return strings.Compare(a.Table+"."+a.Column, b.Table+"."+b.Column) })
	if err := t.versions(ctx); err != nil {
		return File{}, err
	}
	if err := t.audit(ctx); err != nil {
		return File{}, err
	}
	if err := z.text("README.md", tenantReadme(z.m)); err != nil {
		return File{}, err
	}
	slices.Sort(z.m.Missing)
	manifest, err := json.MarshalIndent(z.m, "", "  ")
	if err != nil {
		return File{}, err
	}
	fw, err := z.zw.CreateHeader(&zip.FileHeader{Name: "manifest.json", Method: zip.Deflate, Modified: created})
	if err != nil {
		return File{}, err
	}
	if _, err := fw.Write(append(manifest, '\n')); err != nil {
		return File{}, err
	}
	return f, z.zw.Close()
}

// zipper writes the export's files one at a time, each counted and hashed into the manifest.
type zipper struct {
	zw      *zip.Writer
	created time.Time
	m       Manifest
}

// entry is a file being written: its bytes go to the archive and to its hash.
type entry struct {
	path string
	w    io.Writer
	h    hash.Hash
	n    int64
}

func (e *entry) Write(p []byte) (int, error) {
	n, err := e.w.Write(p)
	e.h.Write(p[:n])
	e.n += int64(n)
	return n, err
}

func (z *zipper) create(name string) (*entry, error) {
	fw, err := z.zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: z.created})
	if err != nil {
		return nil, err
	}
	return &entry{path: name, w: fw, h: sha256.New()}, nil
}

// done records e in the manifest, with its rows when rows is not negative.
func (z *zipper) done(e *entry, rows int) {
	f := ManifestFile{Path: e.path, Bytes: e.n, SHA256: hex.EncodeToString(e.h.Sum(nil))}
	if rows >= 0 {
		f.Rows = &rows
	}
	z.m.Files = append(z.m.Files, f)
}

func (z *zipper) text(name, s string) error {
	e, err := z.create(name)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(e, s); err != nil {
		return err
	}
	z.done(e, -1)
	return nil
}

// copyFile copies a blob into the archive as name; one the store does not hold is listed as missing.
func (z *zipper) copyFile(ctx context.Context, store ports.BlobStore, key, name string) error {
	if store == nil || key == "" {
		z.m.Missing = append(z.m.Missing, name)
		return nil
	}
	r, err := store.Get(ctx, key)
	if errors.Is(err, ports.ErrNotFound) {
		z.m.Missing = append(z.m.Missing, name)
		return nil
	}
	if err != nil {
		return err
	}
	defer r.Close()
	e, err := z.create(name)
	if err != nil {
		return err
	}
	if _, err := io.Copy(e, r); err != nil {
		return err
	}
	z.done(e, -1)
	return nil
}

// tenantWriter reads the organisation's tables into the archive.
type tenantWriter struct {
	u   storage.UnitOfWork
	src TenantSources
	org uuid.UUID
	z   *zipper
}

// pages calls each with every row of table as JSON, a page at a time.
func (t tenantWriter) pages(ctx context.Context, table string, each func(raw string) error) error {
	var after *uuid.UUID
	for {
		rows, err := t.u.TenantData().Rows(ctx, table, after, tenantPage)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if err := each(r); err != nil {
				return err
			}
		}
		if len(rows) < tenantPage {
			return nil
		}
		var last struct {
			ID *uuid.UUID `json:"id"`
		}
		if err := json.Unmarshal([]byte(rows[len(rows)-1]), &last); err != nil || last.ID == nil {
			return nil // a table without an id comes whole
		}
		after = last.ID
	}
}

// released is a released payment file to copy once its table is written.
type released struct {
	id, batch uuid.UUID
	name      string
}

// table writes tables/<name>.csv, its columns but the dropped ones, and is its rows. A payee's bank account number is
// opened into its column number; the evidence's files and the released payment files follow the table.
func (t tenantWriter) table(ctx context.Context, tb storage.TenantTable) (int, error) {
	dropped := Dropped[tb.Name]
	var cols []string
	for _, c := range tb.Columns {
		if _, gone := dropped[c]; !gone {
			cols = append(cols, c)
		}
	}
	header := slices.Clone(cols)
	if tb.Name == "payee_bank_account" {
		header = append(header, "number")
	}
	e, err := t.z.create("tables/" + tb.Name + ".csv")
	if err != nil {
		return 0, err
	}
	sw := sheet.NewCSV(e)
	if err := sw.Begin(tb.Name, header); err != nil {
		return 0, err
	}
	rows := 0
	type file struct{ key, name string }
	var evidence []file
	var releases []released
	err = t.pages(ctx, tb.Name, func(raw string) error {
		var row map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			return fmt.Errorf("reportexport: a row of %s: %w", tb.Name, err)
		}
		cells := make([]sheet.Cell, 0, len(header))
		for _, c := range cols {
			cells = append(cells, cellOf(row[c]))
		}
		switch tb.Name {
		case "payee_bank_account":
			number, err := t.number(ctx, row)
			if err != nil {
				return err
			}
			cells = append(cells, sheet.Text(number))
		case "evidence":
			id, name := text(row["id"]), path.Base(text(row["file_name"]))
			key, store := text(row["locked_key"]), "evidence"
			if key == "" {
				key, store = text(row["staging_key"]), "staging"
			}
			evidence = append(evidence, file{store + ":" + key, "files/evidence/" + id + "/" + name})
		case "payment_batch_release":
			id, _ := uuid.Parse(text(row["id"]))
			batch, _ := uuid.Parse(text(row["batch_id"]))
			releases = append(releases, released{id: id, batch: batch, name: path.Base(text(row["file_name"]))})
		}
		rows++
		return sw.Row(sheet.Row{Cells: cells})
	})
	if err != nil {
		return 0, err
	}
	if err := sw.End(); err != nil {
		return 0, err
	}
	if err := sw.Close(); err != nil {
		return 0, err
	}
	t.z.done(e, rows)
	for _, x := range evidence {
		store, key, _ := strings.Cut(x.key, ":")
		s := t.src.Evidence
		if store == "staging" {
			s = t.src.Staging
		}
		if err := t.z.copyFile(ctx, s, key, x.name); err != nil {
			return 0, err
		}
	}
	for _, x := range releases {
		if err := t.release(ctx, x); err != nil {
			return 0, err
		}
	}
	return rows, nil
}

// number is a payee bank account's number in clear.
func (t tenantWriter) number(ctx context.Context, row map[string]json.RawMessage) (string, error) {
	sealed, err := bytesOf(row["number_sealed"])
	if err != nil {
		return "", err
	}
	a := storage.PayeeBankAccount{Currency: text(row["currency"]), Sealed: sealed}
	for k, to := range map[string]*uuid.UUID{"change_id": &a.ChangeID, "contact_id": &a.ContactID, "member_user_id": &a.MemberUserID} {
		if s := text(row[k]); s != "" {
			if *to, err = uuid.Parse(s); err != nil {
				return "", err
			}
		}
	}
	if t.src.Fields == nil {
		return "", Refusal{"the sealed fields' keys are not configured, so the bank account numbers cannot be exported"}
	}
	return payee.Open(ctx, t.src.Fields, t.org, a)
}

// release copies a released payment file, opened, to files/payment-batch-releases/<release>/<name>.
func (t tenantWriter) release(ctx context.Context, x released) error {
	if t.src.Fields == nil {
		return Refusal{"the sealed fields' keys are not configured, so the released payment files cannot be exported"}
	}
	rel, err := t.u.PaymentBatches().ReleaseOf(ctx, x.batch)
	if err != nil {
		return err
	}
	if rel == nil || rel.ID != x.id {
		return fmt.Errorf("reportexport: the release %s of batch %s is not its batch's", x.id, x.batch)
	}
	data, err := t.src.Fields.Open(ctx, rel.Sealed, paymentbatch.ReleaseAAD(t.org, x.batch))
	if err != nil {
		return err
	}
	e, err := t.z.create("files/payment-batch-releases/" + x.id.String() + "/" + x.name)
	if err != nil {
		return err
	}
	if _, err := e.Write(data); err != nil {
		return err
	}
	t.z.done(e, -1)
	return nil
}

// versions writes each document's versions as a line of documents/versions.jsonl, as the database keeps them.
func (t tenantWriter) versions(ctx context.Context) error {
	e, err := t.z.create("documents/versions.jsonl")
	if err != nil {
		return err
	}
	rows := 0
	err = t.pages(ctx, "document_version", func(raw string) error {
		rows++
		_, err := io.WriteString(e, raw+"\n")
		return err
	})
	if err != nil {
		return err
	}
	t.z.done(e, rows)
	return nil
}

// auditLine is an audit event as audit/events.jsonl writes it: contracts/audit verifies the chain from these.
type auditLine struct {
	Seq      int64  `json:"seq"`
	PrevHash string `json:"prev_hash"`
	Hash     string `json:"hash"`
	Body     string `json:"body"`
}

// audit writes the audit trail as JSON lines, in sequence order, with each event's hashes in hex.
func (t tenantWriter) audit(ctx context.Context) error {
	e, err := t.z.create("audit/events.jsonl")
	if err != nil {
		return err
	}
	rows := 0
	for after := int64(0); ; {
		events, err := t.u.Audit().Events(ctx, after, tenantPage)
		if err != nil {
			return err
		}
		for _, ev := range events {
			b, err := json.Marshal(auditLine{Seq: ev.Seq, PrevHash: hex.EncodeToString(ev.PrevHash), Hash: hex.EncodeToString(ev.Hash), Body: ev.Body})
			if err != nil {
				return err
			}
			if _, err := e.Write(append(b, '\n')); err != nil {
				return err
			}
			rows++
			after = ev.Seq
		}
		if len(events) < tenantPage {
			break
		}
	}
	t.z.done(e, rows)
	return nil
}

// cellOf is a JSON value as a CSV cell: a number as its digits, null as empty, text as itself, an array or an object
// as its JSON.
func cellOf(raw json.RawMessage) sheet.Cell {
	s := strings.TrimSpace(string(raw))
	switch {
	case s == "" || s == "null":
		return sheet.Text("")
	case s[0] == '"':
		return sheet.Text(text(raw))
	case s[0] == '{' || s[0] == '[' || s == "true" || s == "false":
		return sheet.Text(s)
	}
	decimals := 0
	if _, frac, ok := strings.Cut(s, "."); ok {
		decimals = len(frac)
	}
	return sheet.Number(s, decimals)
}

// text is a JSON string's text; empty for anything else.
func text(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// bytesOf is a bytea as row_to_json writes it, "\x" and hex.
func bytesOf(raw json.RawMessage) ([]byte, error) {
	s := text(raw)
	if !strings.HasPrefix(s, `\x`) {
		return nil, fmt.Errorf("reportexport: %q is not a bytea", s)
	}
	return hex.DecodeString(s[2:])
}

// tenantReadme is the export's README.md.
func tenantReadme(m Manifest) string {
	var b strings.Builder
	b.WriteString("# The organisation's full export\n\n")
	fmt.Fprintf(&b, "Prepared by Fyniq %s at %s for the organisation %s, read at ledger revision %d. Its format is %s.\n\n",
		m.Platform, m.CreatedAt, m.OrganisationID, m.LedgerRevision, m.Schema)
	b.WriteString(`## What it holds

- tables/<name>.csv: each table of the organisation's records, one row per record, as RFC 4180 CSV in UTF-8. Amounts
  and other numbers are plain decimals with an ASCII minus, dates are YYYY-MM-DD and times RFC 3339 in UTC. A text
  that a spreadsheet would read as a formula starts with an apostrophe. Lists and structured values are JSON.
- documents/versions.jsonl: every version of every document (invoices, bills, journals and the others), one JSON
  object per line, as Fyniq keeps it.
- files/evidence/<id>/<name>: each piece of evidence and attachment; files/payment-batch-releases/<id>/<name>: each
  payment file released to the bank.
- audit/events.jsonl: the audit trail, one event per line in sequence order, with its hashes in hex.
- manifest.json: every file above with its rows, size and SHA-256; the tables and columns left out and why; the files
  the records name that were no longer held; the audit chain's head when the export was read.

## Handle it with care

It holds the organisation's bank account numbers in clear, with everything else the organisation keeps in Fyniq:
store it encrypted and share it only with whoever must have it.

## How to check it

1. For each file in manifest.json, compute its SHA-256 (sha256sum on Linux, shasum -a 256 on macOS) and compare.
2. Verify the audit trail: start with 32 zero bytes; for each line of audit/events.jsonl in order, check that
   prev_hash is the running value, that SHA-256(running value followed by the body's UTF-8 bytes) is hash, and carry
   it forward. The last hash is the audit_head in manifest.json. The format is described in Fyniq's contracts/audit.

## What it leaves out

`)
	for _, x := range m.Excluded {
		if x.Column == "" {
			fmt.Fprintf(&b, "- the table %s: %s\n", x.Table, x.Why)
		} else {
			fmt.Fprintf(&b, "- the column %s of %s: %s\n", x.Column, x.Table, x.Why)
		}
	}
	return b.String()
}
