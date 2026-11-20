package equity

import (
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// A forecast's funding plan, taken through the round model (PRD-001 FR-PLN-07; p2-reporting-planning task 6.2, design D12). Each planned equity round is a priced round
// of ModelRound over the cap table the plan leaves the day before it, starting from the register's cap table of today: its pre-money valuation divided by the shares
// fully diluted before it gives the price, the new money buys whole shares of it (RND-SHARE), and each holder's part fully diluted before and after gives the dilution.
// The rounds chain, so the second dilutes the holders as the first left them. It is a sandbox: nothing is posted or saved.

// PlanPriceDecimals is the precision a planned round's price is cut down to (the round model's), four decimals.
const PlanPriceDecimals = 4

// NewInvestors stands for the investors a planned round has not named: the holder of the shares its money buys.
var NewInvestors = uuid.Nil

// PlannedRound is a funding row of the forecast that is an equity round: the month its money arrives (its first day is the round's day), the money, and the
// pre-money valuation it is planned at, nil when the plan does not say.
type PlannedRound struct {
	Row      int // its row in the forecast's funding plan
	On       time.Time
	Amount   money.Money
	PreMoney *money.Money
}

// PlannedResult is a planned round as the model works it out. Modelled says whether it was; when not, Why says why, and the cap table passes through it
// unchanged.
type PlannedResult struct {
	Round    PlannedRound
	Modelled bool
	Why      string
	Outcome  RoundOutcome
	Raised   money.Money // what the shares it buys cost: at most the planned amount, the rest buying no whole share
}

// PlanFunding works the planned rounds out over before, the cap table fully diluted today, in the order of their days (a stable order for rounds of one day).
// A round with no pre-money valuation, or one the model refuses (a price of nothing, an amount buying no whole share), is not modelled and says why: the
// plan shows it and goes on, since a plan is a draft of intentions.
func PlanFunding(before CapTable, rounds []PlannedRound) ([]PlannedResult, error) {
	sorted := slices.Clone(rounds)
	slices.SortStableFunc(sorted, func(a, b PlannedRound) int { return a.On.Compare(b.On) })
	table := before
	var out []PlannedResult
	for _, r := range sorted {
		res := PlannedResult{Round: r}
		if r.PreMoney == nil {
			res.Why = "No pre-money valuation is planned, so the round has no price to model: add one to the funding row"
			out = append(out, res)
			continue
		}
		o, err := ModelRound(RoundModel{On: r.On, PreMoney: r.PreMoney, PriceDecimals: PlanPriceDecimals,
			Investors: []RoundInvestor{{HolderID: NewInvestors, Amount: r.Amount}}}, table, nil, func(string) bool { return false })
		if err != nil {
			if e, ok := err.(*Error); ok && e.Code == CodeRound {
				res.Why = fmt.Sprintf("%s: %s", e.Message, e.Fix)
				out = append(out, res)
				continue
			}
			return nil, err
		}
		res.Modelled, res.Outcome = true, o
		if res.Raised, err = o.Raised(); err != nil {
			return nil, err
		}
		out = append(out, res)
		table = afterRound(table, o)
	}
	return out, nil
}

// afterRound is the cap table fully diluted that a round leaves: each holder's shares issued and fully diluted after it, and the pool's unallocated shares.
func afterRound(before CapTable, o RoundOutcome) CapTable {
	t := CapTable{At: before.At}
	t.Issued.Set(&o.IssuedAfter)
	t.FullyDiluted.Set(&o.DilutedAfter)
	t.Unallocated.Set(&o.UnallocatedAfter)
	for _, h := range o.Holders {
		x := CapHolder{Holder: h.HolderID}
		x.Issued.Set(&h.IssuedAfter)
		x.FullyDiluted.Set(&h.DilutedAfter)
		t.Holders = append(t.Holders, x)
	}
	return t
}
