package equity

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CodeAcra is an ACRA extract that does not fit: its day, its holdings or its note (FR-EQ-26).
const CodeAcra = "EQUITY-22"

// MaxAcraHoldings is the most holdings an extract types in.
const MaxAcraHoldings = 500

// AcraHolding is one holding as ACRA's register of members lists it: a holder's shares of a class, the class named by
// its share-capital account (3010 ordinary, 3020 preference) [accountant to confirm for several preference series].
type AcraHolding struct {
	HolderID  uuid.UUID
	AccountID uuid.UUID
	Shares    string
}

// AcraExtract is ACRA's register of members as the business profile gives it on a day (FR-EQ-26, design D11; the
// owner's decision of 7 Nov 2026): its holdings typed in, holder by holder and class by class, with the business
// profile's PDF as its evidence. It is recorded by whoever may post on the cap table, never changed once recorded; a
// newer extract supersedes it.
type AcraExtract struct {
	Date      time.Time // the day it was drafted
	ProfileOn time.Time // the business profile's day
	Holdings  []AcraHolding
	Note      string
}

type acraHoldingPayload struct {
	HolderID  string `json:"holder_id"`
	AccountID string `json:"class_account_id"`
	Shares    string `json:"shares"`
}

type acraPayload struct {
	Date      string               `json:"date"`
	ProfileOn string               `json:"profile_on"`
	Holdings  []acraHoldingPayload `json:"holdings"`
	Note      string               `json:"note,omitempty"`
}

// EncodeAcra is an extract as a document version keeps it.
func EncodeAcra(x AcraExtract) ([]byte, error) {
	holdings := []acraHoldingPayload{}
	for _, h := range x.Holdings {
		holdings = append(holdings, acraHoldingPayload{HolderID: idText(h.HolderID), AccountID: idText(h.AccountID), Shares: h.Shares})
	}
	return json.Marshal(acraPayload{Date: dateText(x.Date), ProfileOn: dateText(x.ProfileOn), Holdings: holdings, Note: x.Note})
}

// ReadAcra reads an extract's version as it was saved.
func ReadAcra(doc storage.Document) (AcraExtract, error) {
	var p acraPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return AcraExtract{}, err
	}
	out := AcraExtract{Note: p.Note}
	var err error
	for _, h := range p.Holdings {
		x := AcraHolding{Shares: h.Shares}
		if x.HolderID, err = uuid.Parse(h.HolderID); err != nil {
			return out, err
		}
		if x.AccountID, err = uuid.Parse(h.AccountID); err != nil {
			return out, err
		}
		out.Holdings = append(out.Holdings, x)
	}
	if out.ProfileOn, err = time.Parse(time.DateOnly, p.ProfileOn); err != nil {
		return out, err
	}
	out.Date, err = time.Parse(time.DateOnly, p.Date)
	return out, err
}

// CheckAcra is what is wrong with an extract on its own, every problem at once (ENG-22): no day or one after today, no
// holdings or too many, a holding with no holder, a class that is not an active share-capital account, shares that are
// not a whole number above zero, a holder and class typed twice, or a note too long.
func CheckAcra(x AcraExtract, s ledger.Settings, today time.Time) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeAcra, Field: field, Message: msg, Fix: fix})
	}
	switch {
	case x.ProfileOn.IsZero():
		add("profile_on", "the business profile's day is missing", "Enter the day the business profile was generated, as printed on it")
	case !today.IsZero() && x.ProfileOn.After(today):
		add("profile_on", "the business profile is dated after today", "Enter the day printed on it")
	}
	switch {
	case len(x.Holdings) == 0:
		add("holdings", "no holding is typed in", "Type in each holder's shares of each class, as the business profile lists them")
	case len(x.Holdings) > MaxAcraHoldings:
		add("holdings", fmt.Sprintf("%d holdings are more than %d", len(x.Holdings), MaxAcraHoldings), "Ask for help to reconcile a register this large")
	}
	type key struct{ holder, account uuid.UUID }
	seen := map[key]bool{}
	for k, h := range x.Holdings {
		field := fmt.Sprintf("holdings[%d]", k)
		if h.HolderID == uuid.Nil {
			add(field+".holder_id", "the holder is missing", "Choose the contact the business profile names")
		}
		if a, ok := s.Accounts[h.AccountID]; !ok || !a.ShareCapital || a.Group || !a.Active {
			add(field+".class_account_id", "the class is not an active share-capital account", "Choose 3010 Ordinary or 3020 Preference share capital")
		}
		if !wholeShares.MatchString(h.Shares) {
			add(field+".shares", fmt.Sprintf("%q is not a whole number of shares above zero", h.Shares), "Type the shares as the business profile lists them, such as 700000")
		}
		if seen[key{h.HolderID, h.AccountID}] && h.HolderID != uuid.Nil {
			add(field, "the holder's shares of this class are typed twice", "Type each holder's shares of a class once")
		}
		seen[key{h.HolderID, h.AccountID}] = true
	}
	if utf8.RuneCountInString(x.Note) > MaxNote {
		add("note", fmt.Sprintf("the note is longer than %d characters", MaxNote), "Shorten it")
	}
	return probs
}

func checkedAcra(ctx context.Context, u storage.UnitOfWork, x AcraExtract, s ledger.Settings, today time.Time) error {
	probs := CheckAcra(x, s, today)
	for k, h := range x.Holdings {
		if err := holderOf(ctx, u, h.HolderID, fmt.Sprintf("holdings[%d].holder_id", k), CodeAcra); err != nil {
			more, ok := err.(ledger.Problems)
			if !ok {
				return err
			}
			probs = append(probs, more...)
		}
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

// DraftAcraIn saves an extract to record in u: a draft, to attach the business profile to.
func DraftAcraIn(ctx context.Context, u storage.UnitOfWork, x AcraExtract, s ledger.Settings, today time.Time) (storage.Document, error) {
	x.Date = today
	if err := checkedAcra(ctx, u, x, s, today); err != nil {
		return storage.Document{}, err
	}
	b, err := EncodeAcra(x)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentAcraExtract, Payload: b})
}

// ReviseAcraIn saves a new version of an extract not recorded yet, in u.
func ReviseAcraIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, x AcraExtract, s ledger.Settings, today time.Time) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentAcraExtract, id)
	if err != nil {
		return doc, err
	}
	was, err := ReadAcra(doc)
	if err != nil {
		return doc, err
	}
	x.Date = was.Date
	if err := checkedAcra(ctx, u, x, s, today); err != nil {
		return doc, err
	}
	b, err := EncodeAcra(x)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// RecordAcraIn records a drafted extract in u: checked again, then recorded with the business profile attached
// (RecordIn). From then on it changes no more.
func RecordAcraIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, s ledger.Settings) (storage.Document, error) {
	doc, err := Drafted(ctx, u, storage.DocumentAcraExtract, id)
	if err != nil {
		return doc, err
	}
	x, err := ReadAcra(doc)
	if err != nil {
		return doc, err
	}
	if err := checkedAcra(ctx, u, x, s, time.Time{}); err != nil {
		return doc, err
	}
	return RecordIn(ctx, u, storage.DocumentAcraExtract, id)
}

// AcraExtracts are every extract, drafts and recorded, the drafts last and the recorded ones in the order they were
// recorded.
func AcraExtracts(ctx context.Context, u storage.UnitOfWork) ([]Recorded[AcraExtract], error) {
	docs, err := all(ctx, u, storage.DocumentAcraExtract)
	if err != nil {
		return nil, err
	}
	var out []Recorded[AcraExtract]
	for _, d := range docs {
		x, err := ReadAcra(d)
		if err != nil {
			return nil, err
		}
		out = append(out, Recorded[AcraExtract]{Doc: d, Value: x, RecordedAt: recordedAt(d)})
	}
	slices.SortStableFunc(out, byRecording[AcraExtract])
	return out, nil
}

// LatestAcra is the extract in force: the recorded one of the latest business profile, the one recorded later among
// those of one day; nil when none is recorded.
func LatestAcra(extracts []Recorded[AcraExtract]) *Recorded[AcraExtract] {
	var best *Recorded[AcraExtract]
	for i := range extracts {
		x := &extracts[i]
		if x.Doc.Status != storage.DocumentPosted {
			continue
		}
		if best == nil || !x.Value.ProfileOn.Before(best.Value.ProfileOn) {
			best = x
		}
	}
	return best
}

// AcraDifference is a holder's shares of a class where the register and ACRA's extract differ on the extract's day
// (C.2 #26): Register and Acra are the shares each holds, "0" where one lists none.
type AcraDifference struct {
	HolderID, AccountID uuid.UUID
	Register, Acra      string
}

// Reconcile compares the register's cap table at the extract's day with the extract, holder by holder and class by
// class (by share-capital account): every holding where the shares differ, one listed by only one side included, in
// holder then class order.
func Reconcile(x AcraExtract, t CapTable) ([]AcraDifference, error) {
	type key struct{ holder, account uuid.UUID }
	register := map[key]*apd.Decimal{}
	for _, l := range t.Lines {
		if l.Issued.Sign() == 0 {
			continue
		}
		k := key{l.Key.Holder, l.Key.Account}
		if register[k] == nil {
			register[k] = new(apd.Decimal)
		}
		if _, err := exact.Add(register[k], register[k], &l.Issued); err != nil {
			return nil, err
		}
	}
	acra := map[key]*apd.Decimal{}
	for _, h := range x.Holdings {
		v := new(apd.Decimal)
		if _, _, err := v.SetString(h.Shares); err != nil {
			return nil, err
		}
		acra[key{h.HolderID, h.AccountID}] = v
	}
	keys := map[key]bool{}
	for k := range register {
		keys[k] = true
	}
	for k := range acra {
		keys[k] = true
	}
	var out []AcraDifference
	zero := apd.New(0, 0)
	for k := range keys {
		r, a := register[k], acra[k]
		if r == nil {
			r = zero
		}
		if a == nil {
			a = zero
		}
		if r.Cmp(a) != 0 {
			out = append(out, AcraDifference{HolderID: k.holder, AccountID: k.account, Register: r.Text('f'), Acra: a.Text('f')})
		}
	}
	slices.SortFunc(out, func(a, b AcraDifference) int {
		if c := strings.Compare(a.HolderID.String(), b.HolderID.String()); c != 0 {
			return c
		}
		return strings.Compare(a.AccountID.String(), b.AccountID.String())
	})
	return out, nil
}

// ReconcileIn is the extract in force in u and where the register differs from it on its day; nil and none while no
// extract is recorded.
func ReconcileIn(ctx context.Context, u storage.UnitOfWork) (*Recorded[AcraExtract], []AcraDifference, error) {
	extracts, err := AcraExtracts(ctx, u)
	if err != nil {
		return nil, nil, err
	}
	latest := LatestAcra(extracts)
	if latest == nil {
		return nil, nil, nil
	}
	t, err := TableIn(ctx, u, latest.Value.ProfileOn)
	if err != nil {
		return nil, nil, err
	}
	diffs, err := Reconcile(latest.Value, t)
	return latest, diffs, err
}
