package director

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a pay choice (FR-EQ-04; design D8).
const (
	CodeNotPayChoice = "FOUNDER-11" // the document is not a pay choice
	CodePayChoice    = "FOUNDER-12" // a pay choice that does not fit: its person, month, choice or note
	CodeNoDecision   = "FOUNDER-13" // a pay choice recorded with no signed decision attached
	CodeRecorded     = "FOUNDER-14" // a pay choice already recorded, which changes no more
)

// The five pay choices of a director or shareholder (FR-EQ-04, §8.22): what the company does about their pay from a
// month on. With no choice recorded, the choice is NoSalary (recommended default 16, FR-ORG-08).
const (
	ChoiceNoSalary       = "no_salary"
	ChoiceSalaryPaid     = "salary_paid"
	ChoiceSalaryDeferred = "salary_deferred"
	ChoiceDirectorFee    = "director_fee"
	ChoiceShares         = "shares_instead_of_pay"
)

// Choices are the pay choices a person may record, in the order the PRD lists them.
var Choices = []string{ChoiceNoSalary, ChoiceSalaryPaid, ChoiceSalaryDeferred, ChoiceDirectorFee, ChoiceShares}

// MaxPayNote is the longest note a pay choice keeps.
const MaxPayNote = 1000

// PayChoice is a director's or shareholder's pay choice from a month on (FR-EQ-04): the person, the first day of the
// month it applies from, the choice, and a note. It is recorded with the signed decision attached as its evidence, is
// append-only, and a choice recorded later supersedes from its month.
type PayChoice struct {
	ContactID uuid.UUID
	From      time.Time
	Choice    string
	Note      string
}

type payChoicePayload struct {
	Date      string    `json:"date"` // the first day of the month it applies from: what lists sort by
	ContactID uuid.UUID `json:"contact_id"`
	Choice    string    `json:"choice"`
	Note      string    `json:"note,omitempty"`
}

// MonthOf is the first day of t's month.
func MonthOf(t time.Time) time.Time {
	y, m, _ := t.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
}

// EncodePayChoice is a pay choice as a document version stores it.
func EncodePayChoice(c PayChoice) ([]byte, error) {
	return json.Marshal(payChoicePayload{Date: dayOf(MonthOf(c.From)), ContactID: c.ContactID, Choice: c.Choice, Note: c.Note})
}

// IsPayChoice says whether a document is a pay choice.
func IsPayChoice(doc storage.Document) bool { return doc.Type == storage.DocumentDirectorPayChoice }

// ReadPayChoice reads a pay choice's version as it was saved.
func ReadPayChoice(doc storage.Document) (PayChoice, error) {
	if !IsPayChoice(doc) {
		return PayChoice{}, &Error{CodeNotPayChoice, fmt.Sprintf("document %s is a %s, not a pay choice", doc.ID, doc.Type), "Open it with its own module"}
	}
	var p payChoicePayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return PayChoice{}, &Error{CodePayChoice, "the saved pay choice cannot be read: " + err.Error(), "Record it again"}
	}
	from, err := time.Parse(time.DateOnly, p.Date)
	if err != nil {
		return PayChoice{}, &Error{CodePayChoice, fmt.Sprintf("the saved pay choice cannot be read: the month %q", p.Date), "Record it again"}
	}
	return PayChoice{ContactID: p.ContactID, From: from, Choice: p.Choice, Note: p.Note}, nil
}

// CheckPayChoice is what is wrong with a pay choice as recorded, every problem at once (ENG-22): a person who is not a
// director or shareholder, no month, a choice Fyniq does not know, or a note too long.
func CheckPayChoice(ctx context.Context, u storage.UnitOfWork, c PayChoice) (ledger.Problems, error) {
	var probs ledger.Problems
	add := func(code, field, msg, fix string) { probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix}) }
	ct, err := u.Contacts().Get(ctx, c.ContactID)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		add(CodeNotPerson, "contact_id", "the contact does not exist in this organisation", "Choose the director or shareholder the choice is for")
	case err != nil:
		return nil, err
	case !ct.Director && !ct.Shareholder:
		add(CodeNotPerson, "contact_id", ct.LegalName+" is not a director or shareholder", "Mark the contact as a director or shareholder in Contacts, or choose another")
	}
	if c.From.IsZero() {
		add(CodePayChoice, "from", "the choice has no month it applies from", "Choose the month it applies from")
	}
	if !slices.Contains(Choices, c.Choice) {
		add(CodePayChoice, "choice", fmt.Sprintf("the choice %q is not one Fyniq knows", c.Choice),
			"Choose no_salary, salary_paid, salary_deferred, director_fee or shares_instead_of_pay")
	}
	if n := utf8.RuneCountInString(c.Note); n > MaxPayNote {
		add(CodePayChoice, "note", fmt.Sprintf("the note has %d characters; the most is %d", n, MaxPayNote), "Shorten it")
	}
	return probs, nil
}

// RecordedChoice is a recorded pay choice with when it was recorded and by whom.
type RecordedChoice struct {
	ID         uuid.UUID
	Choice     PayChoice
	RecordedAt time.Time
	RecordedBy string
}

// PayChoicesOf are every recorded pay choice, oldest recording first, by person.
func PayChoicesOf(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID][]RecordedChoice, error) {
	out := map[uuid.UUID][]RecordedChoice{}
	var afterDate string
	var afterID *uuid.UUID
	for {
		docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentDirectorPayChoice, Status: storage.DocumentPosted,
			AfterDate: afterDate, AfterID: afterID, Limit: 500})
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			afterDate, afterID = doc.Date, &doc.ID
			c, err := ReadPayChoice(doc)
			if err != nil {
				return nil, err
			}
			r := RecordedChoice{ID: doc.ID, Choice: c, RecordedBy: doc.PostedBy}
			if doc.PostedAt != nil {
				r.RecordedAt = *doc.PostedAt
			}
			out[c.ContactID] = append(out[c.ContactID], r)
		}
		if len(docs) < 500 {
			break
		}
	}
	for _, rs := range out {
		slices.SortStableFunc(rs, func(a, b RecordedChoice) int {
			if c := a.RecordedAt.Compare(b.RecordedAt); c != 0 {
				return c
			}
			return strings.Compare(a.ID.String(), b.ID.String())
		})
	}
	return out, nil
}

// InForce is the choice in force for a month among a person's recorded choices (design D8): the one with the latest
// month on or before it, and among those of one month the one recorded last; nil when none applies, which means
// ChoiceNoSalary by default.
func InForce(choices []RecordedChoice, month time.Time) *RecordedChoice {
	month = MonthOf(month)
	var best *RecordedChoice
	for i := range choices {
		c := &choices[i]
		if c.Choice.From.After(month) {
			continue
		}
		if best == nil || c.Choice.From.After(best.Choice.From) || c.Choice.From.Equal(best.Choice.From) && !c.RecordedAt.Before(best.RecordedAt) {
			best = c
		}
	}
	return best
}

// Attached is how many files are attached to a document and not removed.
func Attached(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (int, error) {
	files, err := u.Evidence().OfDocument(ctx, id, uuid.Nil, 100)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, f := range files {
		if f.State != storage.EvidenceRemoved {
			n++
		}
	}
	return n, nil
}

func checkedPayChoice(ctx context.Context, u storage.UnitOfWork, c PayChoice) ([]byte, error) {
	c.From = MonthOf(c.From)
	probs, err := CheckPayChoice(ctx, u, c)
	if err != nil {
		return nil, err
	}
	if len(probs) > 0 {
		return nil, probs
	}
	return EncodePayChoice(c)
}

// DraftPayChoiceIn saves a pay choice to record in u, the caller's unit of work: a draft, to attach the signed decision
// to. One that does not fit writes nothing and answers every problem.
func DraftPayChoiceIn(ctx context.Context, u storage.UnitOfWork, c PayChoice) (storage.Document, error) {
	b, err := checkedPayChoice(ctx, u, c)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentDirectorPayChoice, Payload: b})
}

// DraftedPayChoice reads a pay choice that is not recorded yet, its row locked until commit (ENG-47): CodeRecorded when
// it is.
func DraftedPayChoice(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, PayChoice, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, PayChoice{}, err
	}
	c, err := ReadPayChoice(doc)
	if err != nil {
		return doc, c, err
	}
	if doc.Status != storage.DocumentDraft {
		return doc, c, &Error{CodeRecorded, "the pay choice is recorded, so it changes no more",
			"Record a new choice from the month it changes; the later one supersedes it"}
	}
	return doc, c, nil
}

// RevisePayChoiceIn saves a new version of a pay choice not recorded yet, in u; the caller locked it with
// DraftedPayChoice.
func RevisePayChoiceIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, c PayChoice) (storage.Document, error) {
	b, err := checkedPayChoice(ctx, u, c)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// RecordPayChoiceIn records a drafted pay choice in u: append-only from then on, in force from its month until a later
// one (design D8). It needs the signed decision attached (FOUNDER-13), and posts no entry.
func RecordPayChoiceIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, c, err := DraftedPayChoice(ctx, u, id)
	if err != nil {
		return doc, err
	}
	probs, err := CheckPayChoice(ctx, u, c) // the person may have changed since
	if err != nil {
		return doc, err
	}
	if len(probs) > 0 {
		return doc, probs
	}
	n, err := Attached(ctx, u, id)
	if err != nil {
		return doc, err
	}
	if n == 0 {
		return doc, &Error{CodeNoDecision, "the pay choice has no signed decision attached",
			"Attach the signed board decision or minute, then record it"}
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return doc, err
	}
	return u.Documents().Get(ctx, id)
}
