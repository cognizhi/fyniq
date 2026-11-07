package equity

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A grant's events (FR-EQ-19, design D9: "exercises, forfeitures and expiries update the pool"): a forfeiture gives up
// options not vested, when the holder leaves; an expiry, options that lapse, at the grant's expiry or once a leaver's
// window to exercise closes. Each is drafted by the Accountant with its evidence and recorded on another person's
// approval, never the grant's holder; recorded, it never changes, and its options return to the pool from its day. An
// exercise is an equity transaction, an issue of shares.
const (
	EventForfeiture = "forfeiture"
	EventExpiry     = "expiry"
)

// EventKinds are the kinds a grant's event may be.
var EventKinds = []string{EventForfeiture, EventExpiry}

// CodeEvent is the code of a grant's event that does not fit: its kind, number or day, or more than the grant leaves.
const CodeEvent = "EQUITY-18"

// GrantEvent is a forfeiture or expiry of a recorded grant's options, on a day.
type GrantEvent struct {
	Date    time.Time // the day it was drafted
	GrantID uuid.UUID
	Kind    string
	Number  string
	On      time.Time
	Note    string
}

type eventPayload struct {
	Date    string `json:"date"`
	GrantID string `json:"grant_id"`
	Kind    string `json:"kind"`
	Number  string `json:"number"`
	On      string `json:"on"`
	Note    string `json:"note,omitempty"`
}

// EncodeEvent is a grant's event as a document version keeps it.
func EncodeEvent(e GrantEvent) ([]byte, error) {
	return json.Marshal(eventPayload{Date: dateText(e.Date), GrantID: idText(e.GrantID), Kind: e.Kind, Number: e.Number, On: dateText(e.On),
		Note: e.Note})
}

// ReadEvent reads a grant's event as it was saved.
func ReadEvent(doc storage.Document) (GrantEvent, error) {
	var p eventPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return GrantEvent{}, err
	}
	out := GrantEvent{Kind: p.Kind, Number: p.Number, Note: p.Note}
	var err error
	if out.GrantID, err = uuid.Parse(p.GrantID); err != nil {
		return out, err
	}
	if out.On, err = time.Parse(time.DateOnly, p.On); err != nil {
		return out, err
	}
	out.Date, err = time.Parse(time.DateOnly, p.Date)
	return out, err
}

// Standing is a grant's options on a day, by its recorded events: those forfeited and expired, those outstanding (not
// exercised, forfeited or expired), and those vested, never more than the grant keeps (vested and not forfeited or
// expired) [accountant to confirm: a leaver's vesting stops at the forfeiture].
type Standing struct {
	Forfeited, Expired, Outstanding, Vested apd.Decimal
}

// StandingOf is a grant's standing at the end of day, from its recorded events.
func StandingOf(g Grant, events []GrantEvent, day time.Time) (Standing, error) {
	var out Standing
	var n apd.Decimal
	if _, _, err := n.SetString(g.Number); err != nil {
		return out, err
	}
	for _, e := range events {
		if e.On.After(day) {
			continue
		}
		var x apd.Decimal
		if _, _, err := x.SetString(e.Number); err != nil {
			return out, err
		}
		to := &out.Expired
		if e.Kind == EventForfeiture {
			to = &out.Forfeited
		}
		if _, err := exact.Add(to, to, &x); err != nil {
			return out, err
		}
	}
	if _, err := exact.Sub(&out.Outstanding, &n, &out.Forfeited); err != nil {
		return out, err
	}
	if _, err := exact.Sub(&out.Outstanding, &out.Outstanding, &out.Expired); err != nil {
		return out, err
	}
	vested, err := Vested(g, day)
	if err != nil {
		return out, err
	}
	if vested.Cmp(&out.Outstanding) > 0 {
		vested.Set(&out.Outstanding)
	}
	out.Vested = vested
	return out, nil
}

// CheckEvent is what is wrong with a grant's event on its own, every problem at once (ENG-22): a kind not known, a
// number that is not a whole number of options above zero, no day or one after today, or a note too long.
func CheckEvent(e GrantEvent, today time.Time) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeEvent, Field: field, Message: msg, Fix: fix})
	}
	if !slices.Contains(EventKinds, e.Kind) {
		add("kind", fmt.Sprintf("%q is not a kind of event", e.Kind), "Choose forfeiture or expiry")
	}
	if !wholeShares.MatchString(e.Number) {
		add("number", fmt.Sprintf("%q is not a whole number of options above zero", e.Number), "Enter the options it takes, such as 36000")
	}
	switch {
	case e.On.IsZero():
		add("on", "the day it happens is missing", "Enter the day the options are forfeited or lapse")
	case !today.IsZero() && e.On.After(today):
		add("on", "the day is after today", "Record it on or after the day it happens")
	}
	if utf8.RuneCountInString(e.Note) > MaxNote {
		add("note", fmt.Sprintf("the note is longer than %d characters", MaxNote), "Shorten it")
	}
	return probs
}

// CheckEventOf is what is wrong with an event against its grant and the grant's other recorded events (the database's
// FY293 holds the total too): a day before the grant, more than the grant leaves outstanding by its other events, and
// for a forfeiture more than is not vested on its day.
func CheckEventOf(e GrantEvent, g Grant, others []GrantEvent) (ledger.Problems, error) {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeEvent, Field: field, Message: msg, Fix: fix})
	}
	if !e.On.IsZero() && e.On.Before(g.GrantedOn) {
		add("on", "the day is before the grant date, "+g.GrantedOn.Format(time.DateOnly), "Enter a day on or after the grant")
	}
	if e.On.IsZero() || !wholeShares.MatchString(e.Number) {
		return probs, nil
	}
	var n apd.Decimal
	if _, _, err := n.SetString(e.Number); err != nil {
		return probs, err
	}
	every, err := StandingOf(g, others, farFuture) // what every other event leaves, whatever its day
	if err != nil {
		return probs, err
	}
	if n.Cmp(&every.Outstanding) > 0 {
		add("number", fmt.Sprintf("the grant's other events leave %s options, fewer than the %s it takes", every.Outstanding.Text('f'), e.Number),
			"Take at most what the grant leaves")
		return probs, nil
	}
	if e.Kind == EventForfeiture {
		vested, err := Vested(g, e.On)
		if err != nil {
			return probs, err
		}
		var unvested apd.Decimal
		if _, err := exact.Sub(&unvested, &every.Outstanding, &vested); err != nil {
			return probs, err
		}
		if n.Cmp(&unvested) > 0 {
			if unvested.Sign() < 0 {
				unvested.SetInt64(0)
			}
			add("number", fmt.Sprintf("%s options are not vested on %s, fewer than the %s it forfeits", unvested.Text('f'), e.On.Format(time.DateOnly), e.Number),
				"Forfeit only the options not vested; record those that lapse later as an expiry")
		}
	}
	return probs, nil
}

// farFuture is a day after every event: what a grant's events leave once all have happened.
var farFuture = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

// checkedEvent checks an event on its own and against its recorded grant and the grant's recorded events (a draft is
// never among them).
func checkedEvent(ctx context.Context, u storage.UnitOfWork, e GrantEvent, today time.Time) error {
	probs := CheckEvent(e, today)
	doc, err := u.Documents().Get(ctx, e.GrantID)
	if err != nil {
		return err
	}
	if doc.Type != storage.DocumentOptionGrant || doc.Status != storage.DocumentPosted {
		return append(probs, ledger.Problem{Code: CodeEvent, Field: "grant_id", Message: "the grant is not recorded",
			Fix: "Record the grant first: only a recorded grant's options are forfeited or lapse"})
	}
	g, err := ReadGrant(doc)
	if err != nil {
		return err
	}
	events, err := recordedEvents(ctx, u)
	if err != nil {
		return err
	}
	more, err := CheckEventOf(e, g, events[e.GrantID])
	if err != nil {
		return err
	}
	if probs = append(probs, more...); len(probs) > 0 {
		return probs
	}
	return nil
}

// recordedEvents are every recorded grant event, by the grant's document ID, in the order recorded.
func recordedEvents(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID][]GrantEvent, error) {
	docs, err := all(ctx, u, storage.DocumentOptionEvent)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]GrantEvent{}
	for _, d := range docs {
		if d.Status != storage.DocumentPosted {
			continue
		}
		e, err := ReadEvent(d)
		if err != nil {
			return nil, err
		}
		out[e.GrantID] = append(out[e.GrantID], e)
	}
	return out, nil
}

// GrantEventsOf are a grant's recorded events, in the order recorded.
func GrantEventsOf(ctx context.Context, u storage.UnitOfWork, grant uuid.UUID) ([]GrantEvent, error) {
	events, err := recordedEvents(ctx, u)
	return events[grant], err
}

// DraftEventIn saves a grant's event to record in u: a draft, to attach its evidence to.
func DraftEventIn(ctx context.Context, u storage.UnitOfWork, e GrantEvent, today time.Time) (storage.Document, error) {
	e.Date = today
	if err := checkedEvent(ctx, u, e, today); err != nil {
		return storage.Document{}, err
	}
	b, err := EncodeEvent(e)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentOptionEvent, Payload: b})
}

// ReviseEventIn saves a new version of an event not recorded yet, in u; its grant stays.
func ReviseEventIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, e GrantEvent, today time.Time) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentOptionEvent, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadEvent(doc)
	if err != nil {
		return doc, err
	}
	e.GrantID, e.Date = was.GrantID, was.Date
	if err := checkedEvent(ctx, u, e, today); err != nil {
		return doc, err
	}
	b, err := EncodeEvent(e)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// RecordEventIn records an approved event in u: checked again against its grant's recorded events, then recorded with
// its evidence (RecordIn).
func RecordEventIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentOptionEvent, id)
	if err != nil {
		return doc, err
	}
	e, err := ReadEvent(doc)
	if err != nil {
		return doc, err
	}
	if err := checkedEvent(ctx, u, e, time.Time{}); err != nil {
		return doc, err
	}
	return RecordIn(ctx, u, storage.DocumentOptionEvent, id)
}

// OptionEvents are every grant event, drafts and recorded, oldest first.
func OptionEvents(ctx context.Context, u storage.UnitOfWork) ([]storage.Document, error) {
	return all(ctx, u, storage.DocumentOptionEvent)
}
