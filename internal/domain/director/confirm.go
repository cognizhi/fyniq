package director

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a year-end confirmation (FR-EQ-12; design D13).
const (
	CodeConfirm         = "FOUNDER-24" // a confirmation that does not fit: no financial years set, a day that is not a year's end, or a year not ended
	CodeNotConfirmation = "FOUNDER-25" // the document is not a year-end confirmation
)

// The states of a person's year-end confirmation (design D13): confirmed when their latest confirmation of the year end
// shows the balances the ledger holds at that end now; changed since when it shows others (an entry dated in the year
// posted after it); not confirmed when there is none.
const (
	Confirmed    = "confirmed"
	ChangedSince = "changed_since"
	NotConfirmed = "not_confirmed"
)

// Confirmation is a director's or shareholder's confirmation of their balances at a financial year's end (FR-EQ-12):
// the person, the year's end, and their balance on each director's account then, as the ledger held them when they
// confirmed. It is made by the member who signs in as the person, stepped up with a passkey, and is append-only.
type Confirmation struct {
	ContactID uuid.UUID
	YearEnd   time.Time
	Balances  map[uuid.UUID]money.Money
}

type confirmedBalance struct {
	AccountID uuid.UUID   `json:"account_id"`
	Balance   money.Money `json:"balance"`
}

type confirmationPayload struct {
	Date      string             `json:"date"` // the year's end: what lists sort by
	ContactID uuid.UUID          `json:"contact_id"`
	Balances  []confirmedBalance `json:"balances"`
}

// IsConfirmation says whether a document is a year-end confirmation.
func IsConfirmation(doc storage.Document) bool { return doc.Type == storage.DocumentDirectorConfirmation }

// ReadConfirmation reads a confirmation as it was saved.
func ReadConfirmation(doc storage.Document) (Confirmation, error) {
	if !IsConfirmation(doc) {
		return Confirmation{}, &Error{CodeNotConfirmation, fmt.Sprintf("document %s is a %s, not a year-end confirmation", doc.ID, doc.Type), "Open it with its own module"}
	}
	var p confirmationPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Confirmation{}, &Error{CodeConfirm, "the saved confirmation cannot be read: " + err.Error(), "Confirm again"}
	}
	end, err := time.Parse(time.DateOnly, p.Date)
	if err != nil {
		return Confirmation{}, &Error{CodeConfirm, fmt.Sprintf("the saved confirmation cannot be read: the date %q", p.Date), "Confirm again"}
	}
	c := Confirmation{ContactID: p.ContactID, YearEnd: end, Balances: map[uuid.UUID]money.Money{}}
	for _, b := range p.Balances {
		c.Balances[b.AccountID] = b.Balance
	}
	return c, nil
}

// LastYearEnd is the end of the latest financial year that ended on or before day, and false when none has (or the
// years are not set).
func LastYearEnd(p storage.OrganisationProfile, day time.Time) (time.Time, bool) {
	cal, err := period.CalendarOf(p)
	if err != nil {
		return time.Time{}, false
	}
	n := cal.Number(day)
	if n == 0 {
		return time.Time{}, false
	}
	if end := cal.Year(n).EndsOn; !end.After(day) {
		return end, true
	}
	if n == 1 {
		return time.Time{}, false
	}
	return cal.Year(n - 1).EndsOn, true
}

// IsYearEnd says whether day is the end of one of the organisation's financial years.
func IsYearEnd(p storage.OrganisationProfile, day time.Time) bool {
	cal, err := period.CalendarOf(p)
	if err != nil {
		return false
	}
	n := cal.Number(day)
	return n > 0 && cal.Year(n).EndsOn.Equal(day)
}

// ConfirmIn records the person's confirmation of their balances at a year's end in u, the caller's unit of work: the
// balances the ledger holds at that end now, kept with it, and the document posted, append-only. The year must have
// ended by today. The caller checked that the member who signs in as the person asked, stepped up with a passkey.
func ConfirmIn(ctx context.Context, u storage.UnitOfWork, base money.Currency, contact uuid.UUID, yearEnd, today time.Time) (storage.Document, Confirmation, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return storage.Document{}, Confirmation{}, err
	}
	switch {
	case o.Profile.BooksStart == nil || o.Profile.FirstYearEnd == nil:
		return storage.Document{}, Confirmation{}, &Error{CodeConfirm, "the organisation's financial years are not set, so no year has ended",
			"Ask an Owner to set the books start and the first year's end in Settings › Organisation"}
	case !IsYearEnd(o.Profile, yearEnd):
		return storage.Document{}, Confirmation{}, &Error{CodeConfirm, fmt.Sprintf("%s is not the end of a financial year", dayOf(yearEnd)),
			"Choose the last day of a financial year"}
	case yearEnd.After(today):
		return storage.Document{}, Confirmation{}, &Error{CodeConfirm, fmt.Sprintf("the year ending %s has not ended yet", dayOf(yearEnd)),
			"Confirm the balances once the year has ended"}
	}
	person, accounts, err := Get(ctx, u, base, contact, yearEnd)
	if err != nil {
		return storage.Document{}, Confirmation{}, err
	}
	c := Confirmation{ContactID: contact, YearEnd: yearEnd, Balances: map[uuid.UUID]money.Money{}}
	p := confirmationPayload{Date: dayOf(yearEnd), ContactID: contact}
	for _, a := range accounts {
		b, ok := person.Balances[a.ID]
		if !ok {
			b = money.Zero(base)
		}
		c.Balances[a.ID] = b
		p.Balances = append(p.Balances, confirmedBalance{AccountID: a.ID, Balance: b})
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return storage.Document{}, c, err
	}
	doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentDirectorConfirmation, Payload: payload})
	if err != nil {
		return doc, c, err
	}
	if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
		return doc, c, err
	}
	doc, err = u.Documents().Get(ctx, doc.ID)
	return doc, c, err
}

// RecordedConfirmation is a confirmation with when it was made and by whom.
type RecordedConfirmation struct {
	ID          uuid.UUID
	Confirmed   Confirmation
	ConfirmedAt time.Time
	ConfirmedBy string
}

// ConfirmationsOf are every confirmation, by person, the latest made last.
func ConfirmationsOf(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID][]RecordedConfirmation, error) {
	out := map[uuid.UUID][]RecordedConfirmation{}
	var afterDate string
	var afterID *uuid.UUID
	for {
		docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentDirectorConfirmation, Status: storage.DocumentPosted,
			AfterDate: afterDate, AfterID: afterID, Limit: 500})
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			afterDate, afterID = doc.Date, &doc.ID
			c, err := ReadConfirmation(doc)
			if err != nil {
				return nil, err
			}
			r := RecordedConfirmation{ID: doc.ID, Confirmed: c, ConfirmedBy: doc.PostedBy}
			if doc.PostedAt != nil {
				r.ConfirmedAt = *doc.PostedAt
			}
			out[c.ContactID] = append(out[c.ContactID], r)
		}
		if len(docs) < 500 {
			break
		}
	}
	for _, rs := range out {
		slices.SortStableFunc(rs, func(a, b RecordedConfirmation) int {
			if c := a.ConfirmedAt.Compare(b.ConfirmedAt); c != 0 {
				return c
			}
			return strings.Compare(a.ID.String(), b.ID.String())
		})
	}
	return out, nil
}

// LatestFor is a person's latest confirmation of a year's end, or nil.
func LatestFor(confirmations []RecordedConfirmation, yearEnd time.Time) *RecordedConfirmation {
	var latest *RecordedConfirmation
	for i := range confirmations {
		if c := &confirmations[i]; c.Confirmed.YearEnd.Equal(yearEnd) {
			latest = c
		}
	}
	return latest
}

// StateOf is where a person's confirmation of a year's end stands (design D13): confirmed when the latest shows the
// balances held at that end now (a balance it does not show counts as zero), changed since when it shows others, not
// confirmed when there is none.
func StateOf(latest *RecordedConfirmation, now map[uuid.UUID]money.Money) string {
	if latest == nil {
		return NotConfirmed
	}
	same := func(a, b map[uuid.UUID]money.Money) bool {
		for id, x := range a {
			y, ok := b[id]
			if !ok && !x.IsZero() {
				return false
			}
			if ok {
				if c, err := x.Cmp(y); err != nil || c != 0 {
					return false
				}
			}
		}
		return true
	}
	if same(latest.Confirmed.Balances, now) && same(now, latest.Confirmed.Balances) {
		return Confirmed
	}
	return ChangedSince
}
