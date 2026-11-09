package deadlines

import (
	"context"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The registers' obligations (design D7): done on their own records, and checked there (C.2 #22, C.2 #27).
const (
	ObligationControllerEntry   = "register_controller_entry"   // enter a controller in the company's register within 7 days
	ObligationControllerCentral = "register_controller_central" // then lodge them in ACRA's central register within 2 business days
	ObligationControllerNotice  = "register_controller_notice"  // and send each a notice at least once a year
	ObligationShareLodgement    = "register_share_lodgement"    // lodge an approval, a transfer's notice or a conversion with ACRA
)

// registerItems are the deadlines the registers already compute, on today (design D7): each controller to enter, to
// lodge or to send their yearly notice, and each lodgement an approved conversion of a founder's balance or an equity
// transaction waits for. Each links to its record; the calendar never records them.
func registerItems(ctx context.Context, u storage.UnitOfWork, today time.Time) ([]Item, error) {
	var out []Item
	names := map[uuid.UUID]string{}
	name := func(id uuid.UUID) (string, error) {
		if n, ok := names[id]; ok {
			return n, nil
		}
		c, err := u.Contacts().Get(ctx, id)
		if err != nil {
			return "", err
		}
		names[id] = c.LegalName
		return c.LegalName, nil
	}
	add := func(obligation, id string, due time.Time, title, link string) {
		out = append(out, Item{Key: obligation + ":" + id + ":" + due.Format(time.DateOnly), Obligation: obligation, Area: pack.AreaCorporate,
			Title: title, Due: due, Status: statusOn(due, today), Link: link, Register: true})
	}

	tests, err := equity.StatutoryTestsIn(ctx, u, today)
	if err != nil {
		return nil, err
	}
	for _, s := range tests.Controllers {
		n, err := name(s.HolderID)
		if err != nil {
			return nil, err
		}
		link := "/equity/holders/" + s.HolderID.String()
		switch {
		case s.CompanyRegisterDue != nil:
			add(ObligationControllerEntry, s.HolderID.String(), *s.CompanyRegisterDue, "Enter "+n+" in the register of registrable controllers", link)
		case s.CentralRegisterDue != nil:
			add(ObligationControllerCentral, s.HolderID.String(), *s.CentralRegisterDue, "Lodge "+n+" in ACRA's central register of controllers", link)
		case s.NoticeDue != nil:
			add(ObligationControllerNotice, s.HolderID.String(), *s.NoticeDue, "Send "+n+" the yearly notice to confirm their particulars as a controller", link)
		}
	}

	o, err := u.Organisation(ctx)
	if err != nil {
		return nil, err
	}
	days, err := director.RegistrationDays(ctx, u, today)
	if err != nil {
		return nil, err
	}
	// A conversion of a founder's balance, approved and not yet issued.
	f := storage.DocumentFilter{Type: storage.DocumentDirectorSettlement, Status: storage.DocumentPosted, Limit: 200}
	for {
		page, err := u.Documents().Page(ctx, f)
		if err != nil {
			return nil, err
		}
		for _, doc := range page {
			s, err := director.ReadSettlement(doc)
			if err != nil || s.Route != director.RouteConvert || s.ApprovedOn == nil {
				if err != nil {
					return nil, err
				}
				continue
			}
			issue, err := u.ShareIssues().OfSettlement(ctx, doc.ID)
			if err != nil {
				return nil, err
			}
			if issue != nil {
				continue
			}
			all, err := u.ConversionSteps().Of(ctx, doc.ID)
			if err != nil {
				return nil, err
			}
			steps := director.StepsOf(all)
			lapses, _ := director.LapsesOnFor(steps, o.Profile, *s.ApprovedOn)
			n, err := name(s.ContactID)
			if err != nil {
				return nil, err
			}
			for _, fl := range director.Flags(steps, *s.ApprovedOn, lapses, today, days) {
				if fl.Kind == "due" && fl.DueOn != nil {
					add(ObligationShareLodgement, doc.ID.String(), *fl.DueOn, "Lodge the shareholders' approval to convert "+n+"'s balance ("+doc.Number+") with ACRA",
						"/accounting/directors/"+s.ContactID.String()+"/settle")
				}
			}
		}
		if len(page) < f.Limit {
			break
		}
		last := page[len(page)-1]
		f.AfterDate, f.AfterID = last.Date, &last.ID
	}

	// An equity transaction, approved and not yet effective.
	moves, err := u.Holdings().List(ctx)
	if err != nil {
		return nil, err
	}
	effective := map[uuid.UUID]bool{}
	for _, m := range moves {
		if m.TransactionID != nil {
			effective[*m.TransactionID] = true
		}
	}
	docs, err := equity.Transactions(ctx, u)
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		if doc.Status != storage.DocumentPosted || effective[doc.ID] {
			continue
		}
		t, err := equity.ReadTransaction(doc)
		if err != nil {
			return nil, err
		}
		if t.Kind == equity.KindReversal || t.ApprovedOn == nil {
			continue
		}
		all, err := u.EquitySteps().Of(ctx, doc.ID)
		if err != nil {
			return nil, err
		}
		steps := equity.StepsOf(all)
		var lapses time.Time
		if t.Kind == equity.KindIssueCash || t.Kind == equity.KindInstrumentConversion || t.Kind == equity.KindExercise {
			if s, ok := steps[storage.StepAGMDue]; ok {
				lapses = s.On
			} else if on, ok := director.LapsesOn(o.Profile, *t.ApprovedOn); ok {
				lapses = on
			}
		}
		for _, fl := range equity.Flags(t.Kind, steps, *t.ApprovedOn, lapses, today, days) {
			if fl.Kind == "due" && fl.DueOn != nil {
				add(ObligationShareLodgement, doc.ID.String(), *fl.DueOn, upperFirst(fl.Message), "/equity/transactions/"+doc.ID.String())
			}
		}
	}
	return out, nil
}

func upperFirst(s string) string {
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}
