package director

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of the organisation's exempt-private-company status (FR-EQ-11; design D9).
const (
	CodeNotExemptStatus = "FOUNDER-20" // the document is not an exempt-private-company status
	CodeExemptStatus    = "FOUNDER-21" // a status that does not fit: its date or note
	CodeNoProfile       = "FOUNDER-22" // a status recorded with no evidence attached, such as the ACRA business profile
	CodeStatusRecorded  = "FOUNDER-23" // a status already recorded, which changes no more
)

// MaxExemptNote is the longest note a status keeps.
const MaxExemptNote = 1000

// ExemptStatus is the organisation's exempt-private-company status from a day on (FR-EQ-11; the owner's decision of 3
// Nov 2026, rev 0.167): exempt or not, with a note. It is recorded with its evidence (the ACRA business profile), is
// append-only, and one recorded later supersedes it from its day; the corporate profile (FR-CS-01) takes the record
// over when it is built. With none recorded, a director's account in debit is warned of as for a company that is not
// exempt.
type ExemptStatus struct {
	From   time.Time
	Exempt bool
	Note   string
}

type exemptPayload struct {
	Date   string `json:"date"` // the day it applies from: what lists sort by
	Exempt bool   `json:"exempt"`
	Note   string `json:"note,omitempty"`
}

// EncodeExemptStatus is a status as a document version stores it.
func EncodeExemptStatus(x ExemptStatus) ([]byte, error) {
	return json.Marshal(exemptPayload{Date: dayOf(x.From), Exempt: x.Exempt, Note: x.Note})
}

// IsExemptStatus says whether a document is an exempt-private-company status.
func IsExemptStatus(doc storage.Document) bool { return doc.Type == storage.DocumentExemptStatus }

// ReadExemptStatus reads a status's version as it was saved.
func ReadExemptStatus(doc storage.Document) (ExemptStatus, error) {
	if !IsExemptStatus(doc) {
		return ExemptStatus{}, &Error{CodeNotExemptStatus, fmt.Sprintf("document %s is a %s, not an exempt-private-company status", doc.ID, doc.Type),
			"Open it with its own module"}
	}
	var p exemptPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return ExemptStatus{}, &Error{CodeExemptStatus, "the saved status cannot be read: " + err.Error(), "Record it again"}
	}
	from, err := time.Parse(time.DateOnly, p.Date)
	if err != nil {
		return ExemptStatus{}, &Error{CodeExemptStatus, fmt.Sprintf("the saved status cannot be read: the date %q", p.Date), "Record it again"}
	}
	return ExemptStatus{From: from, Exempt: p.Exempt, Note: p.Note}, nil
}

// CheckExemptStatus is what is wrong with a status as recorded, every problem at once: no day, or a note too long.
func CheckExemptStatus(x ExemptStatus) ledger.Problems {
	var probs ledger.Problems
	if x.From.IsZero() {
		probs = append(probs, ledger.Problem{Code: CodeExemptStatus, Field: "from", Message: "the status has no day it applies from",
			Fix: "Enter the day it applies from, such as the day the company became, or stopped being, exempt"})
	}
	if n := utf8.RuneCountInString(x.Note); n > MaxExemptNote {
		probs = append(probs, ledger.Problem{Code: CodeExemptStatus, Field: "note", Message: fmt.Sprintf("the note has %d characters; the most is %d", n, MaxExemptNote),
			Fix: "Shorten it"})
	}
	return probs
}

func checkedExemptStatus(x ExemptStatus) ([]byte, error) {
	if probs := CheckExemptStatus(x); len(probs) > 0 {
		return nil, probs
	}
	return EncodeExemptStatus(x)
}

// DraftExemptStatusIn saves a status to record in u, the caller's unit of work: a draft, to attach its evidence to.
func DraftExemptStatusIn(ctx context.Context, u storage.UnitOfWork, x ExemptStatus) (storage.Document, error) {
	b, err := checkedExemptStatus(x)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentExemptStatus, Payload: b})
}

// DraftedExemptStatus reads a status that is not recorded yet, its row locked until commit (ENG-47): CodeStatusRecorded
// when it is.
func DraftedExemptStatus(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, ExemptStatus, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, ExemptStatus{}, err
	}
	x, err := ReadExemptStatus(doc)
	if err != nil {
		return doc, x, err
	}
	if doc.Status != storage.DocumentDraft {
		return doc, x, &Error{CodeStatusRecorded, "the status is recorded, so it changes no more",
			"Record a new status from the day it changes; the later one supersedes it"}
	}
	return doc, x, nil
}

// ReviseExemptStatusIn saves a new version of a status not recorded yet, in u; the caller locked it with
// DraftedExemptStatus.
func ReviseExemptStatusIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, x ExemptStatus) (storage.Document, error) {
	b, err := checkedExemptStatus(x)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// RecordExemptStatusIn records a drafted status in u: append-only from then on, in force from its day until a later
// one. It needs its evidence attached (FOUNDER-22), and posts no entry.
func RecordExemptStatusIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, _, err := DraftedExemptStatus(ctx, u, id)
	if err != nil {
		return doc, err
	}
	n, err := Attached(ctx, u, id)
	if err != nil {
		return doc, err
	}
	if n == 0 {
		return doc, &Error{CodeNoProfile, "the status has no evidence attached", "Attach the ACRA business profile that shows it, then record it"}
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return doc, err
	}
	return u.Documents().Get(ctx, id)
}

// RecordedStatus is a recorded status with when it was recorded and by whom.
type RecordedStatus struct {
	ID         uuid.UUID
	Status     ExemptStatus
	RecordedAt time.Time
	RecordedBy string
}

// ExemptStatuses are every recorded status, oldest recording first.
func ExemptStatuses(ctx context.Context, u storage.UnitOfWork) ([]RecordedStatus, error) {
	var out []RecordedStatus
	var afterDate string
	var afterID *uuid.UUID
	for {
		docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentExemptStatus, Status: storage.DocumentPosted,
			AfterDate: afterDate, AfterID: afterID, Limit: 500})
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			afterDate, afterID = doc.Date, &doc.ID
			x, err := ReadExemptStatus(doc)
			if err != nil {
				return nil, err
			}
			r := RecordedStatus{ID: doc.ID, Status: x, RecordedBy: doc.PostedBy}
			if doc.PostedAt != nil {
				r.RecordedAt = *doc.PostedAt
			}
			out = append(out, r)
		}
		if len(docs) < 500 {
			break
		}
	}
	slices.SortStableFunc(out, func(a, b RecordedStatus) int {
		if c := a.RecordedAt.Compare(b.RecordedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID.String(), b.ID.String())
	})
	return out, nil
}

// ExemptOn is the status in force on a day (design D9): the one with the latest day on or before it, and among those of
// one day the one recorded last; nil when none applies.
func ExemptOn(statuses []RecordedStatus, day time.Time) *RecordedStatus {
	var best *RecordedStatus
	for i := range statuses {
		s := &statuses[i]
		if s.Status.From.After(day) {
			continue
		}
		if best == nil || s.Status.From.After(best.Status.From) || s.Status.From.Equal(best.Status.From) && !s.RecordedAt.Before(best.RecordedAt) {
			best = s
		}
	}
	return best
}

// DebitWarning is what a director's account in debit says (FR-EQ-11, design D9): for an exempt private company, that
// the loan is allowed but disclosed; for one that is not exempt, or whose status nobody recorded, the Companies Act's
// s162 ban and its exception for advances for company expenses.
func DebitWarning(s *RecordedStatus) string {
	if s != nil && s.Status.Exempt {
		return "The company is an exempt private company, so a loan to a director is allowed; it is disclosed in the financial " +
			"statements and to investors. If the company stops being exempt, for example when a corporate investor joins, section " +
			"162 of the Companies Act applies."
	}
	lead := "The company is not an exempt private company"
	if s == nil {
		lead = "No exempt-private-company status is recorded, so the company is treated as not exempt"
	}
	return lead + ": section 162 of the Companies Act bans loans to directors. Advances for company expenses are allowed when " +
		"approved in general meeting, or repaid within 6 months after the next AGM. Repay it, or ask your accountant."
}
