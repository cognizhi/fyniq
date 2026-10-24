// Package evidence keeps the files that prove what happened (PRD-001 §8.16; FR-DOC-01, 02, 05, 07, 08, 13).
//
// An upload is checked before anything is stored: its size, its type from the content, a malware scan of the
// file as received, and its image metadata stripped. A clean file goes to the staging store and is attached
// to its record in one audited unit of work, with the SHA-256 of the file as received and as stored. When
// the record is no longer a draft, the Committer copies the file into the retention-locked store, verifies
// its hash, sets its retain-until and records both in the row (two-phase upload, FR-DOC-13). The sweeper
// deletes staging files that no attached row uses after a day, and reports files in the locked store that no
// row knows; it never deletes them. The database keeps evidence append-only once its record is not a draft.
package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The rules' codes (ENG-23).
const (
	CodeType    = "DOC-01" // the file's content is not a type Fyniq accepts
	CodeSize    = "DOC-02" // the file is empty or larger than 25 MB
	CodeMalware = "DOC-03" // the malware scan flagged the file
	CodeText    = "DOC-04" // the evidence type is blank, or a text is too long
	CodeClass   = "DOC-05" // not a data class, or one this record cannot hold yet
	CodeSource  = "DOC-06" // not a source a person can name
	CodeImage   = "DOC-07" // the image's structure cannot be read
	CodeMacro   = "DOC-08" // an office file with macros or ActiveX controls
	CodeArchive = "DOC-09" // an office file whose archive is unreadable, too large or a compression bomb
)

// Problem is a rule an upload breaks, in plain words (ENG-23).
type Problem struct {
	Code, Field, Message, Fix string
}

// File is an upload that passed every check, ready for the staging store.
type File struct {
	Name           string // as the uploader named it, kept as data
	MediaType      string
	Data           []byte // what is stored: the file with its image metadata stripped
	SHA256         string // of Data
	OriginalSHA256 string // of the file as received
}

func hash(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// Check runs FR-DOC-08's checks on an upload: size, type from the content, a malware scan of the file as
// received, and image metadata stripped. A scanner that fails, or none at all, refuses the file: it is
// never attached unscanned. The returned error is a failure of the scanner, not of the file.
func Check(ctx context.Context, scanner ports.Scanner, name string, data []byte) (File, []Problem, error) {
	f := File{Name: SafeName(name)}
	switch {
	case len(data) == 0:
		return f, []Problem{{CodeSize, "file", "The file is empty.", "Choose the file again; it may not have finished saving."}}, nil
	case len(data) > MaxBytes:
		return f, []Problem{{CodeSize, "file", fmt.Sprintf("The file is %.1f MB; the limit is 25 MB.", float64(len(data))/(1<<20)),
			"Compress it, or split it into smaller files."}}, nil
	}
	mt, ok := Sniff(data, f.Name)
	if !ok {
		return f, []Problem{{CodeType, "file", "The file's content is not " + Accepted + ".",
			"Save or export it as PDF, or as a PNG or JPEG picture, and attach that."}}, nil
	}
	if mt == XLSX || mt == DOCX {
		if p := CheckArchive(data); p != nil {
			return f, []Problem{*p}, nil
		}
	}
	if scanner == nil {
		return f, nil, errors.New("no malware scanner is configured, so no file can be attached")
	}
	verdict, err := scanner.Scan(ctx, bytes.NewReader(data))
	if err != nil {
		return f, nil, fmt.Errorf("the malware scan did not finish: %w", err)
	}
	if !verdict.Clean {
		return f, []Problem{{CodeMalware, "file", fmt.Sprintf("The malware scan flagged the file (%s), so it was not stored.", verdict.Signature),
			"Do not open it. Ask the sender for a clean copy, or report it to your administrator."}}, nil
	}
	stored, err := StripMetadata(mt, data)
	if err != nil {
		return f, []Problem{{CodeImage, "file", "The picture's structure cannot be read, so it was not stored.",
			"Open it in an image editor, save it again as PNG or JPEG, and attach that."}}, nil
	}
	f.MediaType, f.Data, f.SHA256, f.OriginalSHA256 = mt, stored, hash(stored), hash(data)
	return f, nil, nil
}

// Details are what the uploader says about a file (FR-DOC-05).
type Details struct {
	EvidenceType string // such as "receipt", "board resolution" or "screenshot"
	Note         string
	RelatesOn    *time.Time
	Source       storage.EvidenceSource
	DataClass    storage.DataClass
}

// CheckDetails checks what the uploader says about a file. Payroll and identity files wait for the payroll
// permissions that guard them (FR-DOC-09, FR-PAY-13); provisional until sign-off.
func CheckDetails(d Details) []Problem {
	var out []Problem
	if n := utf8.RuneCountInString(strings.TrimSpace(d.EvidenceType)); n < 1 || n > 100 {
		out = append(out, Problem{CodeText, "evidence_type", "The evidence type is blank or longer than 100 characters.",
			`Name what the file is, such as "receipt", "board resolution" or "screenshot".`})
	}
	if utf8.RuneCountInString(d.Note) > 1000 {
		out = append(out, Problem{CodeText, "note", "The note is longer than 1,000 characters.", "Shorten it; attach the details as a file."})
	}
	switch d.DataClass {
	case storage.ClassGeneral, storage.ClassPersonal, storage.ClassBank:
	case storage.ClassPayroll, storage.ClassIdentity:
		out = append(out, Problem{CodeClass, "data_class", "Payroll and identity files cannot be attached here yet.",
			"Keep them out of Fyniq until payroll permissions arrive, or attach a copy with the identifiers masked as personal."})
	default:
		out = append(out, Problem{CodeClass, "data_class", fmt.Sprintf("%q is not a data class.", string(d.DataClass)),
			"Send general, personal or bank."})
	}
	switch d.Source {
	case storage.SourceUpload, storage.SourcePaste, storage.SourceAPI:
	default:
		out = append(out, Problem{CodeSource, "source", fmt.Sprintf("%q is not how a file arrives here.", string(d.Source)),
			"Send upload or paste; email arrives through the capture address."})
	}
	return out
}

// StagingKey is where an upload waits in the staging store: never a name the uploader chose.
func StagingKey(org, upload uuid.UUID) string {
	return "staging/" + org.String() + "/" + upload.String()
}

// LockedKey is where a committed file is kept in the retention-locked store.
func LockedKey(org, id uuid.UUID) string { return "evidence/" + org.String() + "/" + id.String() }

// RecordsYears is how many years records are kept after their financial year (§11.9). Changes to the
// setting are refused until the approval engine (FM-1), so its recommended value applies.
func RecordsYears() int {
	d, _ := settings.Lookup("retention.records_years")
	if n, err := strconv.Atoi(d.Recommended); err == nil && n >= 7 {
		return n
	}
	return 7
}

// RetainUntil is a committed file's retain-until (FR-DOC-02): the end of the financial year containing at,
// in Singapore time, plus years. fyStartMonth is the month the organisation's years start (1 when unset).
// at is when the file is committed, never earlier than the record's own date, so the date is never short.
func RetainUntil(fyStartMonth, years int, at time.Time, loc *time.Location) time.Time {
	if fyStartMonth < 1 || fyStartMonth > 12 {
		fyStartMonth = 1
	}
	local := at.In(loc)
	start := time.Date(local.Year(), time.Month(fyStartMonth), 1, 0, 0, 0, 0, loc)
	if start.After(local) {
		start = start.AddDate(-1, 0, 0)
	}
	return start.AddDate(1+years, 0, 0) // the next year's start, years later
}

// CommitKind is the outbox work, on the worker class, that commits an organisation's waiting files.
const CommitKind = "evidence.commit"

// Committer commits attached files of records that are no longer drafts into the locked store (FR-DOC-13
// step 4). It is idempotent: a file already in the locked store with the same hash is committed again in
// its row, and one with another hash stops with an error for a person to look at.
type Committer struct {
	Store   storage.Store
	Staging ports.BlobStore
	Locked  ports.BlobStore
	Clock   ports.Clock
	Zone    *time.Location
}

// singapore is the books' time zone (UTC+8, no daylight saving), when the Committer names none.
var singapore = time.FixedZone("SGT", 8*3600)

// ErrHashMismatch is a stored file whose bytes no longer have the hash its row records (FR-DOC-07).
var ErrHashMismatch = errors.New("a stored file's SHA-256 differs from the one its evidence row records")

func read(ctx context.Context, b ports.BlobStore, key string) ([]byte, error) {
	rc, err := b.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, MaxBytes+1))
}

// Commit commits up to 100 of the organisation's waiting files and returns how many it committed.
func (c Committer) Commit(ctx context.Context, org uuid.UUID) (int, error) {
	var waiting []storage.Evidence
	var fyStart int
	if err := c.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		fyStart = o.Profile.FYStartMonth
		waiting, err = u.Evidence().Unfinalised(ctx, 100)
		return err
	}); err != nil {
		return 0, err
	}
	zone := c.Zone
	if zone == nil {
		zone = singapore
	}
	until := RetainUntil(fyStart, RecordsYears(), c.Clock.Now(), zone)
	n := 0
	var errs []error
	for _, e := range waiting {
		if err := c.commit(ctx, org, e, until); err != nil {
			errs = append(errs, fmt.Errorf("evidence %s: %w", e.ID, err))
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}

func (c Committer) commit(ctx context.Context, org uuid.UUID, e storage.Evidence, until time.Time) error {
	key := LockedKey(org, e.ID)
	data, err := read(ctx, c.Staging, e.StagingKey)
	switch {
	case errors.Is(err, ports.ErrNotFound): // committed before, its staging copy gone: the locked copy decides
		data, err = read(ctx, c.Locked, key)
		if err != nil {
			return err
		}
	case err != nil:
		return err
	}
	if hash(data) != e.SHA256 {
		return ErrHashMismatch
	}
	if err := c.Locked.Create(ctx, key, bytes.NewReader(data)); err != nil && !errors.Is(err, ports.ErrExists) {
		return err
	}
	locked, err := read(ctx, c.Locked, key)
	if err != nil {
		return err
	}
	if hash(locked) != e.SHA256 {
		return ErrHashMismatch
	}
	if err := c.Locked.SetRetainUntil(ctx, key, until); err != nil && !errors.Is(err, ports.ErrRetentionShortened) {
		return err
	}
	if err := c.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Evidence().Finalise(ctx, e.ID, key, until)
		return err
	}); err != nil {
		return err
	}
	_ = c.Staging.Delete(ctx, e.StagingKey) // best effort: the sweeper deletes what is left
	return nil
}

// CommitAll commits every organisation's waiting files.
func (c Committer) CommitAll(ctx context.Context) (int, error) {
	orgs, err := c.Store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	var errs []error
	for _, org := range orgs {
		n, err := c.Commit(ctx, org)
		total += n
		if err != nil {
			errs = append(errs, err)
		}
	}
	return total, errors.Join(errs...)
}

// VerifyReport is what one re-verification found.
type VerifyReport struct {
	Checked    int
	Mismatched []uuid.UUID // a locked copy missing, or with another hash: C.2 #31 ✖
}

// Verify re-hashes every committed file of every organisation in the locked store and compares it with the
// SHA-256 its row records (FR-DOC-07, CTL-AUD-06's nightly check). A mismatch fails the run, so the job's
// failure and its missing heartbeat raise the alert; nothing is changed.
func Verify(ctx context.Context, store storage.Store, locked ports.BlobStore) (VerifyReport, error) {
	var r VerifyReport
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return r, err
	}
	var errs []error
	for _, org := range orgs {
		for after := uuid.Nil; ; {
			var page []storage.Evidence
			if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				var err error
				page, err = u.Evidence().Final(ctx, after, 500)
				return err
			}); err != nil {
				errs = append(errs, err)
				break
			}
			for _, e := range page {
				r.Checked++
				data, err := read(ctx, locked, e.LockedKey)
				if err != nil || hash(data) != e.SHA256 {
					r.Mismatched = append(r.Mismatched, e.ID)
				}
			}
			if len(page) < 500 {
				break
			}
			after = page[len(page)-1].ID
		}
	}
	if len(r.Mismatched) > 0 {
		errs = append(errs, fmt.Errorf("%d committed evidence file(s) are missing or changed in the locked store (%w)", len(r.Mismatched), ErrHashMismatch))
	}
	return r, errors.Join(errs...)
}

// StagingAge is how long an unattached staging file is kept before the sweeper deletes it (FR-DOC-13).
const StagingAge = 24 * time.Hour

// uploadedAt reads when a staging file was uploaded from its key's UUIDv7; ok is false for any other key.
func uploadedAt(key string) (time.Time, bool) {
	i := strings.LastIndexByte(key, '/')
	id, err := uuid.Parse(key[i+1:])
	if err != nil || id.Version() != 7 {
		return time.Time{}, false
	}
	sec, nsec := id.Time().UnixTime()
	return time.Unix(sec, nsec), true
}

// SweepReport is what one sweep did.
type SweepReport struct {
	Deleted int      // staging files no attached row uses, older than a day
	Orphans []string // locked files no row knows: reported, never deleted
}

// Sweep deletes every organisation's staging files that no attached row uses and that are older than a
// day, and lists the locked store's files that no row knows (FR-DOC-13 step 5).
func Sweep(ctx context.Context, store storage.Store, staging, locked ports.BlobStore, now time.Time) (SweepReport, error) {
	var r SweepReport
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return r, err
	}
	var errs []error
	for _, org := range orgs {
		keys, err := staging.List(ctx, "staging/"+org.String()+"/")
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var old []string
		for _, k := range keys {
			if at, ok := uploadedAt(k); ok && now.Sub(at) >= StagingAge {
				old = append(old, k)
			}
		}
		lockedKeys, err := locked.List(ctx, "evidence/"+org.String()+"/")
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var live, known map[string]bool
		if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			if live, err = u.Evidence().LiveStagingKeys(ctx, old); err != nil {
				return err
			}
			known, err = u.Evidence().Known(ctx, lockedKeys)
			return err
		}); err != nil {
			errs = append(errs, err)
			continue
		}
		for _, k := range old {
			if live[k] {
				continue
			}
			if err := staging.Delete(ctx, k); err != nil {
				errs = append(errs, err)
				continue
			}
			r.Deleted++
		}
		for _, k := range lockedKeys {
			if !known[k] {
				r.Orphans = append(r.Orphans, k)
			}
		}
	}
	if len(r.Orphans) > 0 {
		errs = append(errs, fmt.Errorf("%d file(s) in the locked evidence store have no evidence row: review them (FR-DOC-13)", len(r.Orphans)))
	}
	return r, errors.Join(errs...)
}

// Files reads evidence files whole, for the email worker to attach to a message (FR-AR-06): from the locked store once
// a file is final, else from staging, or the locked store when it was committed meanwhile; each checked against the
// SHA-256 its row records.
type Files struct {
	Store   storage.Store
	Staging ports.BlobStore
	Locked  ports.BlobStore
}

// Read returns the organisation's files with ids, in order, as a message carries them. A removed file is refused.
func (f Files) Read(ctx context.Context, org uuid.UUID, ids []uuid.UUID) ([]ports.EmailAttachment, error) {
	rows := make([]storage.Evidence, len(ids))
	if err := f.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		for i, id := range ids {
			e, err := u.Evidence().Get(ctx, id)
			if err != nil {
				return err
			}
			rows[i] = e
		}
		return nil
	}); err != nil {
		return nil, err
	}
	out := make([]ports.EmailAttachment, 0, len(rows))
	for _, e := range rows {
		if e.State == storage.EvidenceRemoved {
			return nil, fmt.Errorf("evidence %s was removed", e.ID)
		}
		var data []byte
		var err error
		if e.LockedKey != "" {
			data, err = read(ctx, f.Locked, e.LockedKey)
		} else if data, err = read(ctx, f.Staging, e.StagingKey); errors.Is(err, ports.ErrNotFound) {
			data, err = read(ctx, f.Locked, LockedKey(org, e.ID)) // committed since the row was read
		}
		if err != nil {
			return nil, err
		}
		if hash(data) != e.SHA256 {
			return nil, ErrHashMismatch
		}
		out = append(out, ports.EmailAttachment{Name: e.FileName, ContentType: e.MediaType, Content: data})
	}
	return out, nil
}
