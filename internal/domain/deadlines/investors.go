package deadlines

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ObligationInvestorInformation is an information right of an investor (FR-EQ-28; p2-reporting-planning task 3.4, design D8): what the agreements owe them
// and by when, one task for each period the right covers, due the days the right allows after the period's end. It is done by delivery, not recorded here: the
// calendar closes it when the update or the pack goes to the holder, with that update or pack as its evidence.
const ObligationInvestorInformation = "investor_information"

// investorLink is where the updates are sent from.
const investorLink = "/equity/updates"

var informationWords = map[string]string{equity.InfoMonthlyAccounts: "monthly accounts", equity.InfoQuarterlyAccounts: "quarterly accounts",
	equity.InfoAnnualAccounts: "annual accounts", equity.InfoBoardPack: "the board pack"}

// infoPeriod is a period an information right covers: its last day, its last month, and the words for it.
type infoPeriod struct {
	End   time.Time
	Month time.Time // the first day of its last month, as a pack carries it
	Words string
}

// infoPeriods are the periods of one kind of information right that end between from and to: the months for monthly accounts and the board pack, each quarter
// of the financial year for quarterly accounts, each financial year for annual accounts.
func infoPeriods(cal period.Calendar, kind string, from, to time.Time) []infoPeriod {
	var out []infoPeriod
	add := func(end time.Time, words string) {
		if end.Before(from) || end.After(to) {
			return
		}
		out = append(out, infoPeriod{End: end, Month: time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC), Words: words})
	}
	for n := 1; ; n++ {
		y := cal.Year(n)
		if y.Span.StartsOn.After(to) {
			break
		}
		switch kind {
		case equity.InfoMonthlyAccounts, equity.InfoBoardPack:
			for _, m := range y.Months {
				add(m.EndsOn, "the month ended "+m.EndsOn.Format("2 Jan 2006"))
			}
		case equity.InfoQuarterlyAccounts:
			for i := 0; i < len(y.Months); i += 3 {
				last := y.Months[min(i+2, len(y.Months)-1)]
				add(last.EndsOn, "the quarter ended "+last.EndsOn.Format("2 Jan 2006"))
			}
		case equity.InfoAnnualAccounts:
			add(y.Span.EndsOn, "the year ended "+y.Span.EndsOn.Format("2 Jan 2006"))
		}
	}
	return out
}

// Delivery is the update or pack that closed an information task: what went to the investor, and when.
type Delivery struct {
	Kind   string // update or pack
	ID     uuid.UUID
	Title  string
	SentOn time.Time
}

// investorItems are the tasks of the investors' information rights in force: for each holder whose rights record is in force, each information right, each
// period it covers from the day the rights apply (and not before the books start) to the window's end. A task is closed by the delivery that satisfies it:
// a sent update (monthly accounts: of the month, either kind; quarterly accounts and annual accounts: the quarterly update of the period's last month) or, for
// the board pack, the month's published pack, to a holder whose member signs in as them; it is Filed with that as its evidence. A holder no member signs in
// as cannot be sent an update through Fyniq, which the task says.
func investorItems(ctx context.Context, u storage.UnitOfWork, today, starts, to time.Time) ([]Item, error) {
	rights, err := equity.RightsOf(ctx, u)
	if err != nil {
		return nil, err
	}
	var live []equity.Recorded[equity.InvestorRights]
	for _, versions := range rights {
		if cur := equity.InForce(versions); cur != nil && len(cur.Value.InformationRights) > 0 {
			live = append(live, *cur)
		}
	}
	if len(live) == 0 {
		return nil, nil
	}
	slices.SortStableFunc(live, func(a, b equity.Recorded[equity.InvestorRights]) int {
		return strings.Compare(a.Value.HolderID.String(), b.Value.HolderID.String())
	})
	o, err := u.Organisation(ctx)
	if err != nil {
		return nil, err
	}
	cal, err := period.CalendarOf(o.Profile)
	if err != nil {
		return []Item{{Key: ObligationInvestorInformation + ":-", Obligation: ObligationInvestorInformation, Area: pack.AreaCorporate, Title: "The investors' information rights",
			Status: NotAssessed, Register: true, Link: investorLink, Reason: "The books start and the first financial year's end are not set: set them in Settings › Organisation"}}, nil
	}
	updates, err := u.InvestorUpdates().List(ctx, true)
	if err != nil {
		return nil, err
	}
	packs := map[uuid.UUID]storage.BoardPack{}
	packOf := func(id uuid.UUID) (storage.BoardPack, error) {
		if p, ok := packs[id]; ok {
			return p, nil
		}
		p, err := u.BoardPacks().Get(ctx, id)
		if err != nil {
			return p, err
		}
		packs[id] = p
		return p, nil
	}
	published, err := u.BoardPacks().List(ctx, true)
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, r := range live {
		holder, err := u.Contacts().Get(ctx, r.Value.HolderID)
		if err != nil {
			if err == ports.ErrNotFound {
				continue
			}
			return nil, err
		}
		if holder.MemberUserID == nil {
			// An update cannot reach them through Fyniq, so their tasks could never close: one item says so, not assessed and never overdue, until a member signs in as the holder.
			out = append(out, Item{Key: ObligationInvestorInformation + ":" + holder.ID.String() + ":-", Obligation: ObligationInvestorInformation, Area: pack.AreaCorporate,
				Title: "Send " + holder.LegalName + " the information their rights give", Status: NotAssessed, Register: true, Link: investorLink,
				Reason: "No member signs in as this holder, so an update cannot reach them through Fyniq: link a member in Contacts"})
			continue
		}
		for _, right := range r.Value.InformationRights {
			earliest := r.Value.From
			if cal.BooksStart.After(earliest) {
				earliest = cal.BooksStart
			}
			for _, per := range infoPeriods(cal, right.Kind, earliest, to) {
				due := per.End.AddDate(0, 0, right.Days)
				if due.After(to) || due.Before(starts) { // nothing due before the day the calendar's rules start is in it, as for every other item
					continue
				}
				it := Item{Key: fmt.Sprintf("%s:%s:%s:%s", ObligationInvestorInformation, holder.ID, right.Kind, per.End.Format(time.DateOnly)), Obligation: ObligationInvestorInformation,
					Area: pack.AreaCorporate, Anchor: per.End, Due: due, Link: investorLink, Register: true,
					Title: fmt.Sprintf("Send %s %s for %s", holder.LegalName, informationWords[right.Kind], per.Words),
					Why:   fmt.Sprintf("%s within %d days of the period's end, as the agreements give (FR-EQ-28)", informationWords[right.Kind], right.Days)}
				if d, err := delivered(updates, published, packOf, holder, right.Kind, per, today); err != nil {
					return nil, err
				} else if d != nil {
					it.Delivery, it.Status = d, Filed
				} else {
					it.Status = statusOn(due, today)
				}
				out = append(out, it)
			}
		}
	}
	return out, nil
}

// delivered is the update or pack that satisfies an information right for a period, sent on or before today: the first one sent.
func delivered(updates []storage.InvestorUpdate, published []storage.BoardPack, packOf func(uuid.UUID) (storage.BoardPack, error), holder storage.Contact, kind string, per infoPeriod, today time.Time) (*Delivery, error) {
	if holder.MemberUserID == nil {
		return nil, nil
	}
	if kind == equity.InfoBoardPack {
		var best *storage.BoardPack
		for i := range published {
			p := published[i]
			if p.Month.Equal(per.Month) && p.PublishedAt != nil && !dayOf(*p.PublishedAt).After(today) && (best == nil || p.PublishedAt.Before(*best.PublishedAt)) {
				best = &p
			}
		}
		if best == nil {
			return nil, nil
		}
		return &Delivery{Kind: "pack", ID: best.ID, Title: "Board pack for " + per.Month.Format("January 2006"), SentOn: dayOf(*best.PublishedAt)}, nil
	}
	var best *storage.InvestorUpdate
	for i := range updates {
		x := updates[i]
		if x.SentAt == nil || dayOf(*x.SentAt).After(today) || !slices.Contains(x.Recipients, *holder.MemberUserID) {
			continue
		}
		if kind != equity.InfoMonthlyAccounts && x.Kind != "quarterly" {
			continue
		}
		p, err := packOf(x.PackID)
		if err != nil {
			return nil, err
		}
		if p.Month.Equal(per.Month) && (best == nil || x.SentAt.Before(*best.SentAt)) {
			best = &x
		}
	}
	if best == nil {
		return nil, nil
	}
	return &Delivery{Kind: "update", ID: best.ID, Title: best.Title, SentOn: dayOf(*best.SentAt)}, nil
}
