package nemura

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
	"github.com/cognizhi/fyniq/internal/ports"
)

// PayrollService is pay records behind the API: only the Payroll administrator and the Owner reach it, and no audit event holds an amount (p1-nemura-payroll D8).
type PayrollService struct {
	Store *Store
	Clock ports.Clock
}

var _ api.Payroll = PayrollService{}

func (p PayrollService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return p.Store.InOrg(ctx, s.Organisation, fn)
}

func (p PayrollService) Groups(ctx context.Context, s api.Session) ([]api.PayGroup, error) {
	out := []api.PayGroup{}
	err := p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := PayGroups(ctx, tx)
		for _, g := range rows {
			out = append(out, api.PayGroup{ID: g.ID.String(), Name: g.Name, PayDay: g.PayDay, CutoffDays: g.CutoffDays, ApprovalDays: g.ApprovalDays})
		}
		return err
	})
	return out, err
}

func (p PayrollService) CreateGroup(ctx context.Context, s api.Session, in api.PayGroupInput) (string, error) {
	var id uuid.UUID
	err := p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = CreatePayGroup(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, payroll.PayGroup{Name: in.Name, PayDay: in.PayDay, CutoffDays: in.CutoffDays, ApprovalDays: in.ApprovalDays})
		return err
	})
	return id.String(), err
}

func (p PayrollService) Pay(ctx context.Context, s api.Session, person uuid.UUID, on time.Time) (api.PayView, bool, error) {
	var out api.PayView
	var ok bool
	err := p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := PersonOn(ctx, tx, person, on); err != nil {
			return err
		}
		r, found, err := PayOn(ctx, tx, person, on)
		if err != nil || !found {
			return err
		}
		ok = true
		out = api.PayView{From: day(r.From), To: dayStr(r.To), GroupID: r.Group.String(), Group: r.GroupName, BasicMonthly: r.Basic, SHGFund: r.SHGFund, SHGOptOut: r.SHGOptOut, JointElection: r.Election, Items: []api.PayItem{}}
		for _, it := range r.Items {
			out.Items = append(out.Items, api.PayItem{Key: it.Key, Name: it.Name, Kind: it.Kind, Nature: it.Nature, Amount: it.Amount, CPFWages: it.CPFWages})
		}
		for _, yr := range []int{on.Year()} {
			if y, has, err := YTDOf(ctx, tx, person, yr); err != nil {
				return err
			} else if has {
				out.YearToDate = &api.YearToDate{Year: y.Year, OW: y.OW, AWSubject: y.AWSubject, AWPaid: y.AWPaid, Through: day(y.Through), Source: y.Source}
			}
		}
		return nil
	})
	return out, ok, err
}

func (p PayrollService) SetPay(ctx context.Context, s api.Session, person uuid.UUID, from time.Time, in api.PersonPayInput) error {
	rec := payroll.PayRecord{Group: in.GroupID, Basic: in.BasicMonthly, SHGFund: in.SHGFund, SHGOptOut: in.SHGOptOut}
	for _, it := range in.Items {
		rec.Items = append(rec.Items, payroll.RecurringItem{Key: it.Key, Name: it.Name, Kind: it.Kind, Nature: it.Nature, Amount: it.Amount, CPFWages: it.CPFWages})
	}
	return p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		_, err := SetPay(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, person, from, rec)
		return err
	})
}

func (p PayrollService) SetElection(ctx context.Context, s api.Session, person uuid.UUID, from time.Time, evidence uuid.UUID) error {
	return p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		_, err := SetElection(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, person, from, evidence)
		return err
	})
}

func (p PayrollService) ImportYTD(ctx context.Context, s api.Session, person uuid.UUID, in api.YTDInput) error {
	through, err := time.Parse(time.DateOnly, in.Through)
	if err != nil {
		return &payroll.Refusal{Code: payroll.CodeInvalid, Message: "through is not a date: write the first day of the last month counted, such as 2026-05-01."}
	}
	return p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return ImportYTD(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, person, payroll.YTDImport{Year: in.Year, OW: in.OW, AWSubject: in.AWSubject, AWPaid: in.AWPaid, Through: through})
	})
}

func (p PayrollService) OneOffs(ctx context.Context, s api.Session, month time.Time) ([]api.OneOff, error) {
	out := []api.OneOff{}
	err := p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := OneOffs(ctx, tx, month)
		for _, r := range rows {
			out = append(out, api.OneOff{ID: r.ID.String(), PersonID: r.Person.String(), Person: r.PersonName, Month: day(r.Month), Key: r.Key, Name: r.Name, Nature: r.Nature, Amount: r.Amount, CPFWages: r.CPFWages,
				ForMonth: dayStr(r.ForMonth), PayableOn: dayStr(r.PayableOn), Note: r.Note, Used: r.Used})
		}
		return err
	})
	return out, err
}

func (p PayrollService) AddOneOff(ctx context.Context, s api.Session, person uuid.UUID, month time.Time, in api.OneOffInput) (string, error) {
	o := payroll.OneOff{Key: in.Key, Name: in.Name, Nature: in.Nature, Amount: in.Amount, CPFWages: in.CPFWages, Note: in.Note}
	for _, f := range []struct {
		name, v string
		to      **time.Time
	}{{"for_month", in.ForMonth, &o.ForMonth}, {"payable_on", in.PayableOn, &o.PayableOn}} {
		if f.v == "" {
			continue
		}
		t, err := time.Parse(time.DateOnly, f.v)
		if err != nil {
			return "", &payroll.Refusal{Code: payroll.CodeInvalid, Message: f.name + " is not a date: write it as 2026-06-01."}
		}
		*f.to = &t
	}
	var id uuid.UUID
	err := p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = AddOneOff(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, person, month, o)
		return err
	})
	return id.String(), err
}
