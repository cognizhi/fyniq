package deadlines

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The calendar's codes.
const (
	CodeFiling    = "CAL-01" // a filing record that does not fit: its key, outcome, day, reference or reason
	CodeRecorded  = "CAL-02" // the item has a live record already: withdraw it first
	CodeWithdrawn = "CAL-03" // the record is withdrawn already, or is a withdrawal
	CodeWindow    = "CAL-04" // the window is longer than 18 months, or ends before it starts
)

// The outcomes of a filing record.
const (
	OutcomeFiled       = "filed"
	OutcomeNotRequired = "not_required"
	OutcomeWithdrawn   = "withdrawn"
)

// The longest texts a record keeps.
const (
	MaxReference = 100
	MaxReason    = 500
	MaxNote      = 1000
)

// Obligations are the obligations a record may name: the sg-deadlines pack's.
var Obligations = []string{"gst_f5", "cpf", "ir8a_ais", "eci", "form_cs", "annual_return", "agm", "statements_to_members",
	"gst_register_retrospective", "gst_register_prospective"}

var keyFormat = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}:[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// Error is a refusal in plain words (ENG-23).
type Error struct {
	Code, Message, Fix string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Filing is what a person recorded of an item (design D6): filed on a day with its reference, such as IRAS's
// acknowledgement number, or not required with the reason; or the withdrawal of an earlier record.
type Filing struct {
	DocumentID uuid.UUID
	Date       time.Time // the day it was recorded
	Key        string
	Outcome    string
	FiledOn    *time.Time
	Reference  string
	Reason     string
	Note       string
	Withdraws  *uuid.UUID // a withdrawal's record
	RecordedBy string
	RecordedAt *time.Time
}

type filingPayload struct {
	Date      string `json:"date"`
	Key       string `json:"key"`
	Outcome   string `json:"outcome"`
	FiledOn   string `json:"filed_on,omitempty"`
	Reference string `json:"reference,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Note      string `json:"note,omitempty"`
	Withdraws string `json:"withdraws,omitempty"`
}

// encode is a record as a document version keeps it.
func encode(f Filing) ([]byte, error) {
	p := filingPayload{Date: f.Date.Format(time.DateOnly), Key: f.Key, Outcome: f.Outcome, Reference: f.Reference, Reason: f.Reason, Note: f.Note}
	if f.FiledOn != nil {
		p.FiledOn = f.FiledOn.Format(time.DateOnly)
	}
	if f.Withdraws != nil {
		p.Withdraws = f.Withdraws.String()
	}
	return json.Marshal(p)
}

// ReadFiling reads a record's version as it was saved.
func ReadFiling(doc storage.Document) (Filing, error) {
	var p filingPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Filing{}, err
	}
	out := Filing{DocumentID: doc.ID, Key: p.Key, Outcome: p.Outcome, Reference: p.Reference, Reason: p.Reason, Note: p.Note,
		RecordedBy: doc.PostedBy, RecordedAt: doc.PostedAt}
	var err error
	if out.Date, err = time.Parse(time.DateOnly, p.Date); err != nil {
		return out, err
	}
	if p.FiledOn != "" {
		d, err := time.Parse(time.DateOnly, p.FiledOn)
		if err != nil {
			return out, err
		}
		out.FiledOn = &d
	}
	if p.Withdraws != "" {
		id, err := uuid.Parse(p.Withdraws)
		if err != nil {
			return out, err
		}
		out.Withdraws = &id
	}
	return out, nil
}

// Filings are the live records by key: recorded, filed or not required, and not withdrawn.
func Filings(ctx context.Context, u storage.UnitOfWork) (map[string]Filing, error) {
	out := map[string]Filing{}
	f := storage.DocumentFilter{Type: storage.DocumentStatutoryFiling, Status: storage.DocumentPosted, Limit: 200}
	for {
		page, err := u.Documents().Page(ctx, f)
		if err != nil {
			return nil, err
		}
		for _, doc := range page {
			x, err := ReadFiling(doc)
			if err != nil {
				return nil, err
			}
			if x.Outcome == OutcomeFiled || x.Outcome == OutcomeNotRequired {
				out[x.Key] = x
			}
		}
		if len(page) < f.Limit {
			return out, nil
		}
		last := page[len(page)-1]
		f.AfterDate, f.AfterID = last.Date, &last.ID
	}
}

// Check is what is wrong with a record on its own, every problem at once (ENG-22): a key that names no obligation of the
// pack, an outcome that is neither, a filed record without its day (today or earlier) or reference, a record not
// required without its reason, or a text too long.
func Check(f Filing, today time.Time) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeFiling, Field: field, Message: msg, Fix: fix})
	}
	obligation, _, ok := ParseKey(f.Key)
	switch {
	case !keyFormat.MatchString(f.Key) || !ok:
		add("key", fmt.Sprintf("%q is not an item's key", f.Key), "Send the key the calendar gives the item, such as eci:2026-12-31")
	case !slices.Contains(Obligations, obligation):
		add("key", fmt.Sprintf("%q is not an obligation of the calendar", obligation), "Record an item the calendar lists; a register's item is done on its own record")
	}
	switch f.Outcome {
	case OutcomeFiled:
		switch {
		case f.FiledOn == nil:
			add("filed_on", "the day it was filed is missing", "Enter the day it was filed or paid, as the acknowledgement gives it")
		case f.FiledOn.After(today):
			add("filed_on", "the day it was filed is after today", "Enter the day it was filed, today or earlier")
		}
		if n := utf8.RuneCountInString(f.Reference); n == 0 || n > MaxReference {
			add("reference", fmt.Sprintf("the reference is blank or longer than %d characters", MaxReference), "Enter the acknowledgement or transaction number")
		}
		if f.Reason != "" {
			add("reason", "a filed record takes no reason", "Leave the reason out")
		}
	case OutcomeNotRequired:
		if n := utf8.RuneCountInString(f.Reason); n == 0 || n > MaxReason {
			add("reason", fmt.Sprintf("the reason is blank or longer than %d characters", MaxReason), "Say why it is not required, such as the ECI waiver's conditions")
		}
		if f.FiledOn != nil || f.Reference != "" {
			add("filed_on", "a record that is not required takes no day or reference", "Leave them out")
		}
	default:
		add("outcome", fmt.Sprintf("%q is not filed or not_required", f.Outcome), "Record it as filed or as not required")
	}
	if utf8.RuneCountInString(f.Note) > MaxNote {
		add("note", fmt.Sprintf("the note is longer than %d characters", MaxNote), "Shorten it")
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

// DraftIn saves an item's record to record in u: a draft, to attach the acknowledgement to.
func DraftIn(ctx context.Context, u storage.UnitOfWork, f Filing, today time.Time) (storage.Document, error) {
	f = tidy(f)
	f.Date, f.Withdraws = today, nil
	if probs := Check(f, today); probs != nil {
		return storage.Document{}, probs
	}
	b, err := encode(f)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentStatutoryFiling, Payload: b})
}

func tidy(f Filing) Filing {
	f.Key, f.Reference, f.Reason, f.Note = strings.TrimSpace(f.Key), strings.TrimSpace(f.Reference), strings.TrimSpace(f.Reason), strings.TrimSpace(f.Note)
	return f
}

// Drafted reads a record not recorded yet, its row locked: not found for any other document.
func Drafted(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, err
	}
	if doc.Type != storage.DocumentStatutoryFiling {
		return doc, ports.ErrNotFound
	}
	if doc.Status != storage.DocumentDraft {
		return doc, &Error{CodeRecorded, "the record is recorded, so it changes no more", "Withdraw it with the reason, then record the item again"}
	}
	return doc, nil
}

// ReviseIn saves a new version of a record not recorded yet, in u.
func ReviseIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, f Filing, today time.Time) (storage.Document, error) {
	doc, err := Drafted(ctx, u, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadFiling(doc)
	if err != nil {
		return doc, err
	}
	f = tidy(f)
	f.Date, f.Withdraws = was.Date, nil
	if probs := Check(f, today); probs != nil {
		return doc, probs
	}
	b, err := encode(f)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// RecordIn records a drafted record in u, live from now on: refused while another record of the item is live (the
// database refuses it too, FY299). The caller holds it to its evidence rule first (FR-DOC-06).
func RecordIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, today time.Time) (storage.Document, error) {
	doc, err := Drafted(ctx, u, id)
	if err != nil {
		return doc, err
	}
	f, err := ReadFiling(doc)
	if err != nil {
		return doc, err
	}
	if probs := Check(f, today); probs != nil {
		return doc, probs
	}
	live, err := Filings(ctx, u)
	if err != nil {
		return doc, err
	}
	if x, ok := live[f.Key]; ok {
		return doc, &Error{CodeRecorded, fmt.Sprintf("the item is recorded already, as %s on %s", strings.ReplaceAll(x.Outcome, "_", " "), x.Date.Format("2 Jan 2006")),
			"Withdraw that record first, with the reason, then record the item again"}
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return doc, err
	}
	return u.Documents().Get(ctx, id)
}

// WithdrawIn withdraws a live record in u, with the reason (design D6): the record is reversed and a withdrawal recorded
// beside it, so the item is open again and both stay in its history.
func WithdrawIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, reason string, today time.Time) (storage.Document, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, err
	}
	if doc.Type != storage.DocumentStatutoryFiling {
		return doc, ports.ErrNotFound
	}
	x, err := ReadFiling(doc)
	if err != nil {
		return doc, err
	}
	if doc.Status != storage.DocumentPosted || x.Outcome == OutcomeWithdrawn {
		return doc, &Error{CodeWithdrawn, "the record is withdrawn already, or is a withdrawal", "Record the item again instead"}
	}
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n == 0 || n > MaxReason {
		return doc, ledger.Problems{{Code: CodeFiling, Field: "reason", Message: fmt.Sprintf("the reason is blank or longer than %d characters", MaxReason),
			Fix: "Say why the record was wrong"}}
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
		return doc, err
	}
	b, err := encode(Filing{Date: today, Key: x.Key, Outcome: OutcomeWithdrawn, Reason: reason, Withdraws: &id})
	if err != nil {
		return doc, err
	}
	w, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentStatutoryFiling, Payload: b})
	if err != nil {
		return w, err
	}
	if err := u.Documents().SetStatus(ctx, w.ID, storage.DocumentPosted); err != nil {
		return w, err
	}
	return u.Documents().Get(ctx, w.ID)
}

// History is every record of an item, the latest first: records, withdrawn ones and withdrawals.
func History(ctx context.Context, u storage.UnitOfWork, key string) ([]Filing, []storage.Document, error) {
	var out []Filing
	var docs []storage.Document
	for _, status := range []storage.DocumentStatus{storage.DocumentPosted, storage.DocumentReversed} {
		f := storage.DocumentFilter{Type: storage.DocumentStatutoryFiling, Status: status, Limit: 200}
		for {
			page, err := u.Documents().Page(ctx, f)
			if err != nil {
				return nil, nil, err
			}
			for _, doc := range page {
				x, err := ReadFiling(doc)
				if err != nil {
					return nil, nil, err
				}
				if x.Key == key {
					out, docs = append(out, x), append(docs, doc)
				}
			}
			if len(page) < f.Limit {
				break
			}
			last := page[len(page)-1]
			f.AfterDate, f.AfterID = last.Date, &last.ID
		}
	}
	return out, docs, nil
}
