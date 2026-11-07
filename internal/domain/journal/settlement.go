package journal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// DirectorSettlements settle what the company owes a director or shareholder (FR-EQ-05, FR-EQ-07; p1-founders-money
// design D10): a draft with its loan note, minute or deed attached is sent for another person's approval
// (director_settlement.approve), whose execution keeps the loan on its terms with no entry, or posts the waiver under
// director_settlement@1, in the approval's unit of work (ENG-02).
type DirectorSettlements struct {
	// AfterPosting runs inside the unit of work after a waiver's entry is written and the document posted.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

func checkSettlement(ctx context.Context, u storage.UnitOfWork, s director.Settlement, today time.Time) ([]byte, ledger.Settings, error) {
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return nil, settings, err
	}
	probs, err := director.CheckSettlement(ctx, u, s, settings, today)
	if err != nil {
		return nil, settings, err
	}
	if s.Route == director.RouteSAFE && s.SAFE != nil && s.SAFE.InstrumentID != uuid.Nil {
		more, err := checkSAFERoute(ctx, u, s, settings, today)
		if err != nil {
			return nil, settings, err
		}
		probs = append(probs, more...)
	}
	if len(probs) > 0 {
		return nil, settings, probs
	}
	b, err := director.EncodeSettlement(s)
	return b, settings, err
}

// checkSAFERoute is what is wrong with the SAFE or note a settlement turns a balance into (FR-EQ-05, FR-EQ-18): it is a
// recorded instrument; the person settling holds it; the chart has 2700; and its money, with what 2700 holds for it
// already, is no more than its amount.
func checkSAFERoute(ctx context.Context, u storage.UnitOfWork, s director.Settlement, settings ledger.Settings, today time.Time) (ledger.Problems, error) {
	bad := func(msg, fix string) ledger.Problems {
		return ledger.Problems{{Code: director.CodeSettlement, Field: "safe.instrument_id", Message: msg, Fix: fix}}
	}
	doc, err := u.Documents().Get(ctx, s.SAFE.InstrumentID)
	switch {
	case errors.Is(err, ports.ErrNotFound) || err == nil && (doc.Type != storage.DocumentInstrument || doc.Status != storage.DocumentPosted):
		return bad("is not a recorded SAFE or convertible note", "Record the SAFE or note first: another person approves it"), nil
	case err != nil:
		return nil, err
	}
	i, err := equity.ReadInstrument(doc)
	if err != nil {
		return nil, err
	}
	if i.HolderID != s.ContactID {
		return bad("the SAFE or note is held by someone else", "Choose one the person settling holds"), nil
	}
	holding, ok := instrumentsAccount(settings)
	if !ok {
		return ledger.Problems{{Code: director.CodeSettlement, Field: "route", Message: "the chart has no convertible instruments account",
			Fix: "Give 2700 Convertible notes / SAFE the role convertible-instruments"}}, nil
	}
	held, err := balance.Instrument(ctx, u, settings.Base, holding, doc.ID, today)
	if err != nil {
		return nil, err
	}
	total, err := held.Add(s.Amount)
	if err != nil {
		return nil, err
	}
	if c, err := total.Cmp(i.Amount); err != nil {
		return nil, err
	} else if c > 0 {
		return bad(fmt.Sprintf("2700 holds %s for it already, and %s more is above its amount of %s", held, s.Amount, i.Amount),
			"Settle at most what the SAFE or note has still to be paid"), nil
	}
	return nil, nil
}

// instrumentsAccount is the active account of role convertible-instruments, 2700.
func instrumentsAccount(s ledger.Settings) (uuid.UUID, bool) {
	for id, a := range s.Accounts {
		if a.InstrumentAccount() && a.Active && !a.Group {
			return id, true
		}
	}
	return uuid.Nil, false
}

// CreateIn saves a settlement as a draft in u, the caller's unit of work, dated today. One that does not fit writes
// nothing and answers every problem.
func (DirectorSettlements) CreateIn(ctx context.Context, u storage.UnitOfWork, s director.Settlement, today time.Time) (storage.Document, error) {
	s.Date, s.DeductedEarlier, s.ApprovedOn = today, false, nil
	b, _, err := checkSettlement(ctx, u, s, today)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentDirectorSettlement, Payload: b})
}

// DraftSettlement reads a settlement that is still a draft, its row locked until commit (ENG-47): CodeSettled when it is
// decided.
func DraftSettlement(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, director.Settlement, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, director.Settlement{}, err
	}
	s, err := director.ReadSettlement(doc)
	if err != nil {
		return doc, s, err
	}
	if doc.Status != storage.DocumentDraft {
		return doc, s, &director.Error{Code: director.CodeSettled, Message: "the settlement is decided, so it changes no more",
			Fix: "Draft a new settlement for what is still owed"}
	}
	return doc, s, nil
}

// ReviseIn saves a new version of a draft settlement in u, dated today; the caller locked it with DraftSettlement.
func (DirectorSettlements) ReviseIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, s director.Settlement, today time.Time) (storage.Document, error) {
	s.Date, s.DeductedEarlier, s.ApprovedOn = today, false, nil
	b, _, err := checkSettlement(ctx, u, s, today)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Revise(ctx, id, b)
}

// CheckedSettlement is a draft settlement as approving it would decide it: a waiver's entry, or a conversion's as it
// would post once effective, as a dry run (FR-APR-02); none for a loan kept or a repayment.
type CheckedSettlement struct {
	Settlement director.Settlement
	Entry      *ledger.Entry
}

// Impact runs a draft settlement through its checks, without its agreement, and a waiver through the rule as it would
// post today: the ledger impact a draft shows (FR-APR-02); none for a loan kept.
func (DirectorSettlements) Impact(ctx context.Context, u storage.UnitOfWork, doc storage.Document, today time.Time) (CheckedSettlement, error) {
	s, err := director.ReadSettlement(doc)
	if err != nil {
		return CheckedSettlement{}, err
	}
	_, settings, err := checkSettlement(ctx, u, s, today)
	if err != nil {
		return CheckedSettlement{}, err
	}
	out := CheckedSettlement{Settlement: s}
	switch s.Route {
	case director.RouteWaive:
		e, err := ledger.PostDirectorWaiver(s.Rule(doc, today), settings)
		if err != nil {
			return CheckedSettlement{}, err
		}
		out.Entry = &e
	case director.RouteSAFE:
		holding, _ := instrumentsAccount(settings)
		e, err := ledger.PostDirectorSAFE(s.SAFERule(doc, today, holding), settings)
		if err != nil {
			return CheckedSettlement{}, err
		}
		out.Entry = &e
	case director.RouteConvert: // as it would post if ACRA registered it today
		c, _, err := s.ConversionRule(doc.ID, today, doc.Number, settings.Base)
		if err != nil {
			return CheckedSettlement{}, err
		}
		e, err := ledger.PostConversion(c, settings)
		if err != nil {
			return CheckedSettlement{}, err
		}
		out.Entry = &e
	}
	return out, nil
}

// Check runs a draft settlement through what asking for its approval needs: Impact, and its agreement attached (a loan
// note or minute for a loan kept, a deed or minute for a waiver; for a conversion, each of its four documents:
// FOUNDER-30).
func (s DirectorSettlements) Check(ctx context.Context, u storage.UnitOfWork, doc storage.Document, today time.Time) (CheckedSettlement, error) {
	c, err := s.Impact(ctx, u, doc, today)
	if err != nil {
		return CheckedSettlement{}, err
	}
	if c.Settlement.Route == director.RouteConvert {
		missing, err := director.MissingDocuments(ctx, u, doc.ID)
		if err != nil {
			return CheckedSettlement{}, err
		}
		if len(missing) > 0 {
			return CheckedSettlement{}, &director.Error{Code: director.CodeNoAgreement, Message: "the conversion has no " + strings.Join(missing, ", no ") + " attached",
				Fix: "Attach each of the shareholders' approval, the board resolution, the conversion agreement and the balance confirmation, as its evidence type"}
		}
		return c, nil
	}
	n, err := director.Attached(ctx, u, doc.ID)
	if err != nil {
		return CheckedSettlement{}, err
	}
	if n == 0 {
		return CheckedSettlement{}, &director.Error{Code: director.CodeNoAgreement, Message: "the settlement has no agreement attached",
			Fix: "Attach the loan note or board minute for a loan kept, or the deed or minute of the waiver"}
	}
	return c, nil
}

// ApproveIn decides an approved draft settlement in u, the approval's unit of work, on today: numbered DS- (gap-free); a
// loan kept is posted with no entry, its terms kept; a waiver is tagged deducted earlier when it is of 2120 (pay or fees
// the company deducted), saved as its next version, and posted under director_settlement@1.
func (s DirectorSettlements) ApproveIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, today time.Time) (storage.Document, *storage.Entry, error) {
	doc, st, err := DraftSettlement(ctx, u, id)
	if err != nil {
		return doc, nil, err
	}
	if _, err := s.Check(ctx, u, doc, today); err != nil {
		return doc, nil, err
	}
	if doc.Number, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentDirectorSettlement); err != nil {
		return doc, nil, err
	}
	var stored *storage.Entry
	if st.Route == director.RouteConvert { // the day it was approved: the effective date and the lapse count from it (D11, D12)
		st.ApprovedOn = &today
		b, err := director.EncodeSettlement(st)
		if err != nil {
			return doc, nil, err
		}
		number := doc.Number
		if doc, err = u.Documents().Revise(ctx, id, b); err != nil {
			return doc, nil, err
		}
		doc.Number = number
	}
	if st.Route == director.RouteSAFE { // its money is on 2700 from the day it is approved
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return doc, nil, err
		}
		holding, _ := instrumentsAccount(settings)
		entry, err := ledger.PostDirectorSAFE(st.SAFERule(doc, today, holding), settings)
		if err != nil {
			return doc, nil, err
		}
		e, err := u.Ledger().Append(ctx, toStorage(entry, nil))
		if err != nil {
			return doc, nil, err
		}
		stored = &e
	}
	if st.Route == director.RouteWaive {
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return doc, nil, err
		}
		st.DeductedEarlier = settings.Accounts[st.AccountID].Role == ledger.RoleDeferredRemuneration
		b, err := director.EncodeSettlement(st)
		if err != nil {
			return doc, nil, err
		}
		number := doc.Number
		if doc, err = u.Documents().Revise(ctx, id, b); err != nil {
			return doc, nil, err
		}
		doc.Number = number
		entry, err := ledger.PostDirectorWaiver(st.Rule(doc, today), settings)
		if err != nil {
			return doc, nil, err
		}
		e, err := u.Ledger().Append(ctx, toStorage(entry, nil))
		if err != nil {
			return doc, nil, err
		}
		stored = &e
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return doc, nil, err
	}
	doc.Status = storage.DocumentPosted
	if stored == nil || s.AfterPosting == nil {
		return doc, stored, nil
	}
	return doc, stored, s.AfterPosting(ctx, u, doc, *stored)
}

// replayDirectorWaiver posts a waiver's version again as its entry posted it (ENG-06), on the day it posted; and a
// settlement into a SAFE or note the same way, on the 2700 its entry posted to.
func replayDirectorWaiver(doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	st, err := director.ReadSettlement(doc)
	if err != nil {
		return ledger.Entry{}, err
	}
	if st.Route == director.RouteSAFE {
		var holding uuid.UUID
		for _, l := range e.Lines {
			if l.InstrumentID != nil {
				holding = l.AccountID
			}
		}
		return ledger.ReplayDirectorSAFE(e.RuleVersion, st.SAFERule(doc, e.PostingDate, holding), s)
	}
	return ledger.ReplayDirectorWaiver(e.RuleVersion, st.Rule(doc, e.PostingDate), s)
}

// Effective is what makes an approved conversion effective (FR-EQ-06, FR-EQ-09; design D11): ACRA's lodgement reference,
// the resolutions it rests on, the effective date (the day ACRA's register of members shows the allotment), the
// organisation's today, the day the approval lapses (design D12), and who records it.
type Effective struct {
	LodgementRef   string
	ResolutionRefs string
	EffectiveOn    time.Time
	Today          time.Time
	LapsesOn       time.Time
	RecordedBy     uuid.UUID
}

// EffectiveIn makes an approved conversion effective in u, the caller's unit of work: the effective date no later than
// today, no earlier than the approval and no later than the day it lapses (CodeEffective), what is owed on the account
// on that day at least the amount converted, then the conversion posted from the register on that date (share_issue@1)
// and the issue recorded with its entry, never backdated (FY255). A period that is closed refuses it (INV-03): reopening
// the period is the fix, never a later date.
func (s DirectorSettlements) EffectiveIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, e Effective) (storage.ShareIssue, storage.Entry, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return storage.ShareIssue{}, storage.Entry{}, err
	}
	st, err := director.ReadSettlement(doc)
	if err != nil {
		return storage.ShareIssue{}, storage.Entry{}, err
	}
	if doc.Status != storage.DocumentPosted || st.Route != director.RouteConvert {
		return storage.ShareIssue{}, storage.Entry{}, &director.Error{Code: director.CodeNotApproved, Message: "only an approved conversion becomes effective",
			Fix: "Ask for the conversion's approval first"}
	}
	if done, err := u.ShareIssues().OfSettlement(ctx, id); err != nil {
		return storage.ShareIssue{}, storage.Entry{}, err
	} else if done != nil {
		return storage.ShareIssue{}, storage.Entry{}, &director.Error{Code: director.CodeNotApproved, Message: "the conversion is effective already",
			Fix: "Nothing to record: its shares are in the register"}
	}
	day := func(t time.Time) string { return t.Format(time.DateOnly) }
	approved := e.Today
	if st.ApprovedOn != nil {
		approved = *st.ApprovedOn
	}
	switch {
	case e.EffectiveOn.After(e.Today):
		return storage.ShareIssue{}, storage.Entry{}, &director.Error{Code: director.CodeEffective, Message: fmt.Sprintf("the effective date %s is after today", day(e.EffectiveOn)),
			Fix: "Enter the day ACRA's register of members shows the allotment, once it does"}
	case e.EffectiveOn.Before(approved):
		return storage.ShareIssue{}, storage.Entry{}, &director.Error{Code: director.CodeEffective, Message: fmt.Sprintf("the effective date %s is before the approval on %s", day(e.EffectiveOn), day(approved)),
			Fix: "Enter the day ACRA's register shows the allotment: never earlier than its approval"}
	case !e.LapsesOn.IsZero() && e.EffectiveOn.After(e.LapsesOn):
		return storage.ShareIssue{}, storage.Entry{}, &director.Error{Code: director.CodeEffective, Message: fmt.Sprintf("the shareholders' approval lapsed on %s, the date by which the next AGM must be held", day(e.LapsesOn)),
			Fix: "Draft the conversion again for a new approval"}
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return storage.ShareIssue{}, storage.Entry{}, err
	}
	issueID, err := uuid.NewV7()
	if err != nil {
		return storage.ShareIssue{}, storage.Entry{}, err
	}
	rule, shares, err := st.ConversionRule(issueID, e.EffectiveOn, strings.TrimSpace(doc.Number+" "+shares(st)), settings.Base)
	if err != nil {
		return storage.ShareIssue{}, storage.Entry{}, err
	}
	owed, err := director.Owed(ctx, u, settings.Base, st.ContactID, st.AccountID, e.EffectiveOn)
	if err != nil {
		return storage.ShareIssue{}, storage.Entry{}, err
	}
	if c, _ := shares.Converted.Cmp(owed); c > 0 {
		return storage.ShareIssue{}, storage.Entry{}, &director.Error{Code: director.CodeSettlement,
			Message: fmt.Sprintf("the company owes %s on the account on %s, less than the %s converted", owed, day(e.EffectiveOn), shares.Converted),
			Fix:     "Draft the conversion again for what is owed"}
	}
	entry, err := ledger.PostConversion(rule, settings)
	if err != nil {
		return storage.ShareIssue{}, storage.Entry{}, err
	}
	stored, err := u.Ledger().Append(ctx, toStorage(entry, nil))
	if err != nil {
		return storage.ShareIssue{}, storage.Entry{}, err
	}
	class, err := equity.DefaultClass(ctx, u, rule.Class) // the account's first recorded class, if it has one (FR-EQ-16, FY287)
	if err != nil {
		return storage.ShareIssue{}, storage.Entry{}, err
	}
	issue, err := equity.RecordIssue(ctx, u, storage.ShareIssue{ID: issueID, HolderID: st.ContactID, ClassAccountID: rule.Class, ClassID: class,
		Shares: storage.Decimal(shares.Count.Text('f')), Price: storage.Decimal(st.Conversion.Price), Currency: settings.Base.Code,
		Amount: storage.Decimal(rule.Equity.String()), Consideration: storage.ConsiderationDebtCapitalised, ResolutionRefs: e.ResolutionRefs,
		LodgementRef: e.LodgementRef, EffectiveOn: e.EffectiveOn, SettlementID: &id, EntryID: &stored.ID, RecordedBy: e.RecordedBy, RecordedAt: e.Today}, nil)
	if err != nil {
		return storage.ShareIssue{}, storage.Entry{}, err
	}
	if s.AfterPosting != nil {
		if err := s.AfterPosting(ctx, u, doc, stored); err != nil {
			return storage.ShareIssue{}, storage.Entry{}, err
		}
	}
	return issue, stored, nil
}

// shares says how many shares a conversion allots at its price, for the entry's memo.
func shares(s director.Settlement) string {
	if s.Conversion == nil {
		return ""
	}
	return "converted at " + s.Conversion.Price + " a share"
}

// replayConversion posts a share issue's entry again as it posted (ENG-06): a founder's conversion from the issue and the
// settlement it decided, a cash issue from the issue.
func replayConversion(ctx context.Context, u storage.UnitOfWork, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	id, err := uuid.Parse(e.Source.ID)
	if err != nil {
		return ledger.Entry{}, err
	}
	all, err := u.ShareIssues().List(ctx)
	if err != nil {
		return ledger.Entry{}, err
	}
	for _, x := range all {
		if x.ID != id {
			continue
		}
		if x.SettlementID == nil && x.Consideration == storage.ConsiderationSAFE { // a SAFE's or note's conversion (instrument_conversion@1)
			return replayInstrumentConversion(s, e, x)
		}
		if x.SettlementID == nil { // a cash issue: Dr share application monies · Cr the class (share_issue_cash@1)
			return replayCashIssue(s, e, x)
		}
		doc, err := u.Documents().Get(ctx, *x.SettlementID)
		if err != nil {
			return ledger.Entry{}, err
		}
		st, err := director.ReadSettlement(doc)
		if err != nil {
			return ledger.Entry{}, err
		}
		c, _, err := st.ConversionRule(id, e.PostingDate, e.Memo, s.Base)
		if err != nil {
			return ledger.Entry{}, err
		}
		return ledger.ReplayConversion(e.RuleVersion, c, s)
	}
	return ledger.Entry{}, fmt.Errorf("share issue %s: no conversion in the register names it", id)
}
