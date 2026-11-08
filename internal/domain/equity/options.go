package equity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The codes of the option pool and its grants (ENG-23).
const (
	CodePool  = "EQUITY-16" // a pool resolution that does not fit: its size, class, resolution or date
	CodeGrant = "EQUITY-17" // a grant that does not fit: its holder, number, price, dates or vesting, or more than the pool leaves
)

// The accelerations of a grant's vesting on a change of control (FR-EQ-19): recorded, never computed (design D9).
var Accelerations = []string{"none", "single", "double"}

// The longest vesting a grant takes, in months.
const MaxVestingMonths = 120

// PoolResolution is a resolution setting the option pool's size (FR-EQ-19, design D9): the shares set aside, the class
// they are of, the resolution and the day it was passed. Recorded on another person's approval with the resolution
// attached; the pool's size on a day is the latest recorded resolution passed on or before it.
type PoolResolution struct {
	Date           time.Time
	Size           string
	ClassAccountID uuid.UUID
	ResolutionRef  string
	ResolvedOn     time.Time
	Note           string
}

// Vesting is how a grant's options vest (FR-EQ-19): none before the cliff, then evenly by whole months over the months
// from the grant date, the acceleration on a change of control recorded.
type Vesting struct {
	CliffMonths  int
	Months       int
	Acceleration string
}

// Grant is a grant of options from the pool (FR-EQ-19, design D9): the holder (a contact marked investor or
// shareholder), the number of options, the exercise price per share in the base currency, the grant date, the vesting
// and the day it expires. Recorded on another person's approval, never the holder's, it never changes; exercises,
// forfeitures and expiries are its events.
type Grant struct {
	Date           time.Time
	HolderID       uuid.UUID
	Number         string
	ExercisePrice  string
	GrantedOn      time.Time
	Vesting        Vesting
	ExpiresOn      time.Time
	ClassAccountID uuid.UUID
	Note           string
}

type poolPayload struct {
	Date           string `json:"date"`
	Size           string `json:"size"`
	ClassAccountID string `json:"class_account_id"`
	ResolutionRef  string `json:"resolution_ref"`
	ResolvedOn     string `json:"resolved_on"`
	Note           string `json:"note,omitempty"`
}

type grantPayload struct {
	Date           string `json:"date"`
	HolderID       string `json:"holder_id"`
	Number         string `json:"number"`
	ExercisePrice  string `json:"exercise_price"`
	Currency       string `json:"currency"`
	GrantedOn      string `json:"granted_on"`
	CliffMonths    int    `json:"cliff_months"`
	VestingMonths  int    `json:"vesting_months"`
	Acceleration   string `json:"acceleration"`
	ExpiresOn      string `json:"expires_on"`
	ClassAccountID string `json:"class_account_id"`
	Note           string `json:"note,omitempty"`
}

// EncodePool is a pool resolution as a document version keeps it.
func EncodePool(p PoolResolution) ([]byte, error) {
	return json.Marshal(poolPayload{Date: dateText(p.Date), Size: p.Size, ClassAccountID: idText(p.ClassAccountID), ResolutionRef: p.ResolutionRef,
		ResolvedOn: dateText(p.ResolvedOn), Note: p.Note})
}

// ReadPool reads a pool resolution's version as it was saved.
func ReadPool(doc storage.Document) (PoolResolution, error) {
	var p poolPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return PoolResolution{}, err
	}
	out := PoolResolution{Size: p.Size, ResolutionRef: p.ResolutionRef, Note: p.Note}
	var err error
	if out.ClassAccountID, err = uuid.Parse(p.ClassAccountID); err != nil {
		return out, err
	}
	if out.ResolvedOn, err = time.Parse(time.DateOnly, p.ResolvedOn); err != nil {
		return out, err
	}
	out.Date, err = time.Parse(time.DateOnly, p.Date)
	return out, err
}

// EncodeGrant is a grant as a document version keeps it.
func EncodeGrant(g Grant, base money.Currency) ([]byte, error) {
	return json.Marshal(grantPayload{Date: dateText(g.Date), HolderID: idText(g.HolderID), Number: g.Number, ExercisePrice: g.ExercisePrice,
		Currency: base.Code, GrantedOn: dateText(g.GrantedOn), CliffMonths: g.Vesting.CliffMonths, VestingMonths: g.Vesting.Months,
		Acceleration: g.Vesting.Acceleration, ExpiresOn: dateText(g.ExpiresOn), ClassAccountID: idText(g.ClassAccountID), Note: g.Note})
}

// ReadGrant reads a grant's version as it was saved.
func ReadGrant(doc storage.Document) (Grant, error) {
	var p grantPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Grant{}, err
	}
	out := Grant{Number: p.Number, ExercisePrice: p.ExercisePrice, Note: p.Note,
		Vesting: Vesting{CliffMonths: p.CliffMonths, Months: p.VestingMonths, Acceleration: p.Acceleration}}
	var err error
	if out.HolderID, err = uuid.Parse(p.HolderID); err != nil {
		return out, err
	}
	if out.ClassAccountID, err = uuid.Parse(p.ClassAccountID); err != nil {
		return out, err
	}
	for _, x := range []struct {
		to   *time.Time
		text string
	}{{&out.GrantedOn, p.GrantedOn}, {&out.ExpiresOn, p.ExpiresOn}, {&out.Date, p.Date}} {
		if *x.to, err = time.Parse(time.DateOnly, x.text); err != nil {
			return out, err
		}
	}
	return out, nil
}

// CheckPool is what is wrong with a pool resolution, every problem at once (ENG-22): a size that is not a whole number
// of shares above zero, a class that is not an active share-capital account, no resolution or no day it was passed,
// a day after today, or a note too long.
func CheckPool(p PoolResolution, s ledger.Settings, today time.Time) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodePool, Field: field, Message: msg, Fix: fix})
	}
	if !wholeShares.MatchString(p.Size) {
		add("size", fmt.Sprintf("%q is not a whole number of shares above zero", p.Size), "Enter the shares the resolution sets aside, such as 1000000")
	}
	if a, ok := s.Accounts[p.ClassAccountID]; !ok || !a.ShareCapital || a.Group || !a.Active {
		add("class_account_id", "the class is not an active share-capital account", "Choose 3010 Ordinary share capital, or the class the options are over")
	}
	if strings.TrimSpace(p.ResolutionRef) == "" {
		add("resolution_ref", "the resolution is missing", "Name the resolution that sets the pool, such as Ordinary resolution 3 Mar 2027")
	} else if utf8.RuneCountInString(p.ResolutionRef) > MaxNote {
		add("resolution_ref", fmt.Sprintf("the resolution is longer than %d characters", MaxNote), "Shorten it")
	}
	switch {
	case p.ResolvedOn.IsZero():
		add("resolved_on", "the day it was resolved is missing", "Enter the day the resolution was passed")
	case !today.IsZero() && p.ResolvedOn.After(today):
		add("resolved_on", "the resolution is dated after today", "Enter the day it was passed")
	}
	if utf8.RuneCountInString(p.Note) > MaxNote {
		add("note", fmt.Sprintf("the note is longer than %d characters", MaxNote), "Shorten it")
	}
	return probs
}

// CheckGrant is what is wrong with a grant's own terms, every problem at once (ENG-22): no holder, a number that is not
// a whole number of options above zero, an exercise price that is not a price from zero in the base currency, no grant
// date or one after today, vesting outside 0 to 120 months with the cliff inside it, an acceleration not known, an
// expiry not after the grant date, a class that is not an active share-capital account, or a note too long.
func CheckGrant(g Grant, s ledger.Settings, today time.Time) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeGrant, Field: field, Message: msg, Fix: fix})
	}
	if g.HolderID == uuid.Nil {
		add("holder_id", "the holder is missing", "Choose the person the options are granted to")
	}
	if !wholeShares.MatchString(g.Number) {
		add("number", fmt.Sprintf("%q is not a whole number of options above zero", g.Number), "Enter the options granted, such as 20000")
	}
	if p, err := money.ParseUnitPrice(g.ExercisePrice, s.Base, "exercise_price"); err != nil || p.Sign() < 0 {
		add("exercise_price", fmt.Sprintf("%q is not a price per share from zero in %s", g.ExercisePrice, s.Base), "Enter the exercise price, such as 0.10")
	}
	switch {
	case g.GrantedOn.IsZero():
		add("granted_on", "the grant date is missing", "Enter the day the options were granted")
	case !today.IsZero() && g.GrantedOn.After(today):
		add("granted_on", "the grant is dated after today", "Enter the day the options were granted")
	}
	v := g.Vesting
	if v.Months < 0 || v.Months > MaxVestingMonths {
		add("vesting_months", fmt.Sprintf("the vesting lasts %d months; it is 0 to %d", v.Months, MaxVestingMonths), "Enter the months it vests over, such as 48")
	}
	if v.CliffMonths < 0 || v.CliffMonths > v.Months {
		add("cliff_months", fmt.Sprintf("the cliff of %d months is not within the vesting", v.CliffMonths), "Enter a cliff from 0 to the vesting months, such as 12")
	}
	if !slices.Contains(Accelerations, v.Acceleration) {
		add("acceleration", fmt.Sprintf("%q is not an acceleration", v.Acceleration), "Choose none, single or double")
	}
	if g.ExpiresOn.IsZero() || !g.GrantedOn.IsZero() && !g.ExpiresOn.After(g.GrantedOn) {
		add("expires_on", "the expiry is not after the grant date", "Enter the day the options lapse, such as ten years after the grant")
	}
	if a, ok := s.Accounts[g.ClassAccountID]; !ok || !a.ShareCapital || a.Group || !a.Active {
		add("class_account_id", "the class is not an active share-capital account", "Choose the class the pool's options are over")
	}
	if utf8.RuneCountInString(g.Note) > MaxNote {
		add("note", fmt.Sprintf("the note is longer than %d characters", MaxNote), "Shorten it")
	}
	return probs
}

// Vested is what of a grant has vested at the end of day (FR-EQ-19, design D9: computed, never stored): nothing before
// the grant date or the cliff; then number × the whole months since the grant ÷ the vesting months, rounded down to
// whole options, all of it once the vesting months have passed or when there are none. A month passes on the grant's
// day of the month, or on a shorter month's last day [accountant to confirm both].
func Vested(g Grant, day time.Time) (apd.Decimal, error) {
	var n apd.Decimal
	if _, _, err := n.SetString(g.Number); err != nil {
		return n, err
	}
	if day.Before(g.GrantedOn) {
		return apd.Decimal{}, nil
	}
	months := (day.Year()-g.GrantedOn.Year())*12 + int(day.Month()) - int(g.GrantedOn.Month())
	lastOfMonth := day.AddDate(0, 0, 1).Day() == 1 // a grant on the 31st vests on the last day of a shorter month
	if day.Day() < g.GrantedOn.Day() && !lastOfMonth {
		months--
	}
	if g.Vesting.Months == 0 || months >= g.Vesting.Months {
		return n, nil
	}
	if months < g.Vesting.CliffMonths {
		return apd.Decimal{}, nil
	}
	var out apd.Decimal
	if _, err := exact.Mul(&out, &n, apd.New(int64(months), 0)); err != nil {
		return out, err
	}
	if _, err := exact.Quo(&out, &out, apd.New(int64(g.Vesting.Months), 0)); err != nil {
		return out, err
	}
	down := exact.WithPrecision(60)
	down.Rounding = apd.RoundDown
	_, err := down.Quantize(&out, &out, 0)
	return out, err
}

// Pool is the option pool on a day (FR-EQ-19, design D9): its size by the latest recorded resolution passed on or before
// it (and its class), the options granted by the grants recorded on or before it, and what is left. Exercises,
// forfeitures and expiries join it with the grants' events.
type Pool struct {
	Day         time.Time
	Size        apd.Decimal
	ClassID     uuid.UUID // the class account of the resolution in force; uuid.Nil when none is
	Resolution  *storage.Document
	Granted     apd.Decimal // every option the grants made by the day granted
	Returned    apd.Decimal // and those forfeited or expired since, back in the pool
	Unallocated apd.Decimal // size − granted + returned
}

// OptionBook is the recorded pool resolutions and grants, each in the order recorded: what the pool reads (FR-EQ-19,
// design D9). It reads nothing else, so the product and the differential harness share its rules.
type OptionBook struct {
	Pools  []PoolResolution
	Grants []Grant
	// Returns are the options recorded forfeitures and expiries gave back to the pool, each from its day (FR-EQ-19:
	// "forfeitures and expiries update the pool") [accountant to confirm against the plan's rules].
	Returns []Return
}

// Return is options given back to the pool on a day.
type Return struct {
	On     time.Time
	Number string
}

// At is the pool at the end of day: the size of the latest resolution passed on or before it (of two passed on one day,
// the later recorded) and its index in Pools (-1 when none is), the options the grants made on or before it granted,
// and what is left.
func (b OptionBook) At(day time.Time) (Pool, int, error) {
	out, in := Pool{Day: day}, -1
	for i, p := range b.Pools {
		if p.ResolvedOn.After(day) || in >= 0 && p.ResolvedOn.Before(b.Pools[in].ResolvedOn) {
			continue
		}
		in = i
	}
	if in >= 0 {
		if _, _, err := out.Size.SetString(b.Pools[in].Size); err != nil {
			return out, in, err
		}
		out.ClassID = b.Pools[in].ClassAccountID
	}
	for _, g := range b.Grants {
		if g.GrantedOn.After(day) {
			continue
		}
		var n apd.Decimal
		if _, _, err := n.SetString(g.Number); err != nil {
			return out, in, err
		}
		if _, err := exact.Add(&out.Granted, &out.Granted, &n); err != nil {
			return out, in, err
		}
	}
	for _, r := range b.Returns {
		if r.On.After(day) {
			continue
		}
		var n apd.Decimal
		if _, _, err := n.SetString(r.Number); err != nil {
			return out, in, err
		}
		if _, err := exact.Add(&out.Returned, &out.Returned, &n); err != nil {
			return out, in, err
		}
	}
	if _, err := exact.Sub(&out.Unallocated, &out.Size, &out.Granted); err != nil {
		return out, in, err
	}
	_, err := exact.Add(&out.Unallocated, &out.Unallocated, &out.Returned)
	return out, in, err
}

// ShortFrom is the pool on the first day from since on that is short (FY293): the options granted by then are more
// than the size of the resolution in force then. The days checked are since and each day from it on that a resolution
// passed or a grant was made; nil when none is short.
func (b OptionBook) ShortFrom(since time.Time) (*Pool, error) {
	days := []time.Time{since}
	for _, p := range b.Pools {
		if !p.ResolvedOn.Before(since) {
			days = append(days, p.ResolvedOn)
		}
	}
	for _, g := range b.Grants {
		if !g.GrantedOn.Before(since) {
			days = append(days, g.GrantedOn)
		}
	}
	slices.SortFunc(days, func(a, b time.Time) int { return a.Compare(b) })
	days = slices.CompactFunc(days, func(a, b time.Time) bool { return a.Equal(b) })
	for _, day := range days {
		p, _, err := b.At(day)
		if err != nil {
			return nil, err
		}
		if p.Unallocated.Sign() < 0 {
			return &p, nil
		}
	}
	return nil, nil
}

// recordedOptions is the option book in u, with the documents of its pool resolutions and grants in the same order,
// and the recorded events of each grant by its document's ID.
func recordedOptions(ctx context.Context, u storage.UnitOfWork) (OptionBook, []storage.Document, map[uuid.UUID][]GrantEvent, error) {
	var b OptionBook
	pools, err := all(ctx, u, storage.DocumentOptionPool)
	if err != nil {
		return b, nil, nil, err
	}
	var docs []storage.Document
	for _, d := range pools {
		if d.Status != storage.DocumentPosted {
			continue
		}
		p, err := ReadPool(d)
		if err != nil {
			return b, nil, nil, err
		}
		b.Pools, docs = append(b.Pools, p), append(docs, d)
	}
	grants, err := all(ctx, u, storage.DocumentOptionGrant)
	if err != nil {
		return b, nil, nil, err
	}
	for _, d := range grants {
		if d.Status != storage.DocumentPosted {
			continue
		}
		g, err := ReadGrant(d)
		if err != nil {
			return b, nil, nil, err
		}
		b.Grants = append(b.Grants, g)
	}
	events, err := recordedEvents(ctx, u)
	if err != nil {
		return b, nil, nil, err
	}
	for _, es := range events {
		for _, e := range es {
			b.Returns = append(b.Returns, Return{On: e.On, Number: e.Number})
		}
	}
	return b, docs, events, nil
}

// PoolOn is the pool at the end of day, read from the recorded resolutions and grants.
func PoolOn(ctx context.Context, u storage.UnitOfWork, day time.Time) (Pool, error) {
	b, docs, _, err := recordedOptions(ctx, u)
	if err != nil {
		return Pool{Day: day}, err
	}
	out, in, err := b.At(day)
	if in >= 0 {
		out.Resolution = &docs[in]
	}
	return out, err
}

// checkedGrant checks a grant's terms and, against the books, its holder (a contact marked investor or shareholder) and
// the pool on its grant date: a resolution in force, of the grant's class, that leaves at least its number (the
// database's FY293 holds it too).
func checkedGrant(ctx context.Context, u storage.UnitOfWork, g Grant, s ledger.Settings, today time.Time) error {
	probs := CheckGrant(g, s, today)
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeGrant, Field: field, Message: msg, Fix: fix})
	}
	if g.HolderID != uuid.Nil {
		c, err := u.Contacts().Get(ctx, g.HolderID)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			add("holder_id", "the holder does not exist in this organisation", "Choose a contact marked investor or shareholder")
		case err != nil:
			return err
		case !c.Investor && !c.Shareholder:
			add("holder_id", c.LegalName+" is not marked investor or shareholder", "Mark the contact an investor in Contacts first")
		}
	}
	if !g.GrantedOn.IsZero() && wholeShares.MatchString(g.Number) {
		pool, err := PoolOn(ctx, u, g.GrantedOn)
		if err != nil {
			return err
		}
		var n apd.Decimal
		if _, _, err := n.SetString(g.Number); err != nil {
			return err
		}
		badClass := slices.ContainsFunc(probs, func(p ledger.Problem) bool { return p.Field == "class_account_id" })
		switch {
		case pool.Resolution == nil:
			add("granted_on", "no recorded resolution sets an option pool on the grant date", "Record the pool's resolution first")
		case n.Cmp(&pool.Unallocated) > 0:
			add("number", fmt.Sprintf("the pool leaves %s options on the grant date, fewer than the %s granted", pool.Unallocated.Text('f'), g.Number),
				"Grant at most what the pool leaves, or record a resolution that enlarges it first")
		default:
			short, err := shortFrom(ctx, u, g.GrantedOn, &n, nil)
			if err != nil {
				return err
			}
			if short != nil {
				add("number", fmt.Sprintf("with it, the %s options granted by %s are more than the pool of %s then", short.Granted.Text('f'),
					short.Day.Format(time.DateOnly), short.Size.Text('f')), "Grant fewer, or record a resolution that enlarges the pool first")
			}
		}
		if pool.Resolution != nil && pool.ClassID != g.ClassAccountID && !badClass {
			add("class_account_id", "the pool's options are over another class", "Choose the class the pool's resolution names")
		}
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

// shortFrom is the pool on the first day from since on that is short (OptionBook.ShortFrom), with a grant of extra on
// since and the resolution res counted as the latest recorded when they are not nil.
func shortFrom(ctx context.Context, u storage.UnitOfWork, since time.Time, extra *apd.Decimal, res *PoolResolution) (*Pool, error) {
	b, _, _, err := recordedOptions(ctx, u)
	if err != nil {
		return nil, err
	}
	if extra != nil {
		b.Grants = append(b.Grants, Grant{GrantedOn: since, Number: extra.Text('f')})
	}
	if res != nil {
		b.Pools = append(b.Pools, *res)
	}
	return b.ShortFrom(since)
}

// checkedPool checks a pool resolution's terms and, against the books, that with it recorded the pool covers the
// grants made by each day from its own (the database's FY293 holds it too).
func checkedPool(ctx context.Context, u storage.UnitOfWork, p PoolResolution, s ledger.Settings, today time.Time) error {
	if probs := CheckPool(p, s, today); len(probs) > 0 {
		return probs
	}
	short, err := shortFrom(ctx, u, p.ResolvedOn, nil, &p)
	if err != nil || short == nil {
		return err
	}
	return ledger.Problems{{Code: CodePool, Field: "size", Message: fmt.Sprintf("the %s options granted by %s are more than the %s shares it sets aside",
		short.Granted.Text('f'), short.Day.Format(time.DateOnly), short.Size.Text('f')), Fix: "Set aside at least the options granted, or date it after them"}}
}

// DraftPoolIn saves a pool resolution to record in u: a draft, to attach the resolution to.
func DraftPoolIn(ctx context.Context, u storage.UnitOfWork, p PoolResolution, s ledger.Settings, today time.Time) (storage.Document, error) {
	p.Date = today
	if err := checkedPool(ctx, u, p, s, today); err != nil {
		return storage.Document{}, err
	}
	b, err := EncodePool(p)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentOptionPool, Payload: b})
}

// RevisePoolIn saves a new version of a pool resolution not recorded yet, in u.
func RevisePoolIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, p PoolResolution, s ledger.Settings, today time.Time) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentOptionPool, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadPool(doc)
	if err != nil {
		return doc, err
	}
	p.Date = was.Date
	if err := checkedPool(ctx, u, p, s, today); err != nil {
		return doc, err
	}
	b, err := EncodePool(p)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// DraftGrantIn saves a grant to record in u: a draft, to attach the grant letter to.
func DraftGrantIn(ctx context.Context, u storage.UnitOfWork, g Grant, s ledger.Settings, today time.Time) (storage.Document, error) {
	g.Date = today
	if err := checkedGrant(ctx, u, g, s, today); err != nil {
		return storage.Document{}, err
	}
	b, err := EncodeGrant(g, s.Base)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentOptionGrant, Payload: b})
}

// ReviseGrantIn saves a new version of a grant not recorded yet, in u; its holder stays.
func ReviseGrantIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, g Grant, s ledger.Settings, today time.Time) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentOptionGrant, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadGrant(doc)
	if err != nil {
		return doc, err
	}
	g.HolderID, g.Date = was.HolderID, was.Date
	if err := checkedGrant(ctx, u, g, s, today); err != nil {
		return doc, err
	}
	b, err := EncodeGrant(g, s.Base)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// RecordPoolIn records an approved pool resolution in u: its terms and the grants it must cover checked again, then
// recorded with its evidence (RecordIn).
func RecordPoolIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, s ledger.Settings) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentOptionPool, id)
	if err != nil {
		return doc, err
	}
	p, err := ReadPool(doc)
	if err != nil {
		return doc, err
	}
	if err := checkedPool(ctx, u, p, s, time.Time{}); err != nil {
		return doc, err
	}
	return RecordIn(ctx, u, storage.DocumentOptionPool, id)
}

// RecordGrantIn records an approved grant in u: its terms and the pool checked again on its grant date, then recorded
// with its evidence (RecordIn).
func RecordGrantIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, s ledger.Settings) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentOptionGrant, id)
	if err != nil {
		return doc, err
	}
	g, err := ReadGrant(doc)
	if err != nil {
		return doc, err
	}
	if err := checkedGrant(ctx, u, g, s, time.Time{}); err != nil {
		return doc, err
	}
	return RecordIn(ctx, u, storage.DocumentOptionGrant, id)
}

// PoolResolutions are every pool resolution, drafts and recorded, oldest first.
func PoolResolutions(ctx context.Context, u storage.UnitOfWork) ([]storage.Document, error) {
	return all(ctx, u, storage.DocumentOptionPool)
}

// Grants are every grant, drafts and recorded, oldest first.
func Grants(ctx context.Context, u storage.UnitOfWork) ([]storage.Document, error) {
	return all(ctx, u, storage.DocumentOptionGrant)
}
