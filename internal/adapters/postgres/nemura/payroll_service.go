package nemura

import (
	"context"
	"encoding/hex"
	"sync"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	nemurapacks "github.com/cognizhi/fyniq/internal/nemura/packs"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
	"github.com/cognizhi/fyniq/internal/ports"
)

// PayrollService is pay records behind the API: only the Payroll administrator and the Owner reach it, and no audit event holds an amount (p1-nemura-payroll D8).
type PayrollService struct {
	Store *Store
	Clock ports.Clock
	Packs *PacksService // the people pack, for the overtime multiple; nil falls back to the Employment Act's 1.5
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

var (
	statOnce  sync.Once
	statPacks []pack.Version
	statErr   error
)

// statutory is the CPF, SDL and SHG packs Nemura carries, loaded once: a pack that does not validate is an error to the run that needs it, never a guess.
func statutory() ([]pack.Version, error) {
	statOnce.Do(func() { statPacks, statErr = pack.LoadAll(nemurapacks.StatutoryFiles()) })
	return statPacks, statErr
}

func (p PayrollService) peoplePack(month time.Time) (pack.Version, bool) {
	if p.Packs == nil {
		return pack.Version{}, false
	}
	return p.Packs.inForce("sg-people", month)
}

func versionOf(v VersionRow) api.RunVersion {
	out := api.RunVersion{ID: v.ID.String(), Number: v.Number, State: v.State, Digest: hex.EncodeToString(v.Digest), PreparedBy: v.PreparedBy.String(), PreparedAt: v.PreparedAt.UTC().Format(time.RFC3339),
		Reason: v.Reason, People: v.People, Failing: v.Failing, Warning: v.Warning, Packs: v.Packs,
		Totals: api.RunTotals{Gross: v.Totals.Gross, CPFEmployee: v.Totals.CPFEmployee, CPFEmployer: v.Totals.CPFEmployer, SDLPayable: v.Totals.SDLPayable, SHG: v.Totals.SHG, Deductions: v.Totals.Deductions,
			Reimbursements: v.Totals.Reimbursements, Net: v.Totals.Net, Disburse: v.Totals.Disburse, EmployerCost: v.Totals.EmployerCost}}
	if v.ApprovedBy != nil {
		out.ApprovedBy = v.ApprovedBy.String()
	}
	if v.ApprovedAt != nil {
		out.ApprovedAt = v.ApprovedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func (p PayrollService) runOf(ctx context.Context, tx pgx.Tx, id uuid.UUID) (api.Run, error) {
	r, vs, err := RunOf(ctx, tx, id)
	if err != nil {
		return api.Run{}, err
	}
	out := api.Run{ID: r.ID.String(), Month: day(r.Month), GroupID: r.Group.String(), Group: r.GroupName, PayDay: day(r.PayDate)}
	if len(vs) > 0 {
		out.Latest = versionOf(vs[0])
	}
	return out, nil
}

func (p PayrollService) StartRun(ctx context.Context, s api.Session, month time.Time, group uuid.UUID) (api.Run, error) {
	var out api.Run
	stat, err := statutory()
	if err != nil {
		return out, err
	}
	err = p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		now := p.Clock.Now()
		id, err := StartRun(ctx, tx, s.Organisation, now, s.Member, group, month)
		if err != nil {
			return err
		}
		pv, has := p.peoplePack(month)
		if _, _, _, err := CalculateRun(ctx, tx, s.Organisation, now, s.Member, id, "", stat, pv, has); err != nil {
			return err
		}
		out, err = p.runOf(ctx, tx, id)
		return err
	})
	return out, err
}

func (p PayrollService) Runs(ctx context.Context, s api.Session, month time.Time) ([]api.Run, error) {
	out := []api.Run{}
	err := p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		ids, err := RunIDs(ctx, tx, month)
		if err != nil {
			return err
		}
		for _, id := range ids {
			r, err := p.runOf(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return nil
	})
	return out, err
}

func (p PayrollService) RunDetail(ctx context.Context, s api.Session, id uuid.UUID) (api.RunDetail, error) {
	var out api.RunDetail
	err := p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		r, vs, err := RunOf(ctx, tx, id)
		if err != nil {
			return err
		}
		out.Run = api.Run{ID: r.ID.String(), Month: day(r.Month), GroupID: r.Group.String(), Group: r.GroupName, PayDay: day(r.PayDate)}
		for _, v := range vs {
			out.Versions = append(out.Versions, versionOf(v))
		}
		if len(vs) > 0 {
			out.Latest = out.Versions[0]
			lines, err := RunLines(ctx, tx, vs[0].ID)
			if err != nil {
				return err
			}
			for _, l := range lines {
				al := api.RunLine{PersonID: l.Person.String(), Person: l.PersonName, Gross: l.Gross, OWSubject: l.OWSubject, AWSubject: l.AWSubject, CPFTotal: l.CPFTotal, CPFEmployee: l.CPFEmployee, CPFEmployer: l.CPFEmployer,
					SDL: l.SDL, SHG: l.SHG, Deductions: l.Deductions, Reimbursements: l.Reimbursements, Held: l.Held, Net: l.Net, Disburse: l.Disburse, EmployerCost: l.EmployerCost,
					Overridden: l.Overridden, Computed: l.Computed, Items: []api.RunItem{}, Working: []api.RunStep{}, Flags: []api.RunFlag{}}
				for _, it := range l.Items {
					al.Items = append(al.Items, api.RunItem{Key: it.Key, Name: it.Name, Class: it.Class, Amount: it.Amount, Why: it.Why, SourceType: it.SourceType, SourceID: it.SourceID, Mismatch: it.Mismatch})
				}
				for _, st := range l.Working {
					al.Working = append(al.Working, api.RunStep{Rule: st.Rule, Text: st.Text})
				}
				for _, f := range l.Flags {
					al.Flags = append(al.Flags, api.RunFlag{Code: f.Code, Fail: f.Fail, Message: f.Message})
				}
				out.Lines = append(out.Lines, al)
			}
		}
		overs, err := activeOverrides(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, list := range overs {
			for _, o := range list {
				out.Overrides = append(out.Overrides, api.RunOverride{ID: o.ID.String(), PersonID: o.Person.String(), Person: o.PersonName, Field: o.Field, Value: o.Value, Reason: o.Reason})
			}
		}
		return nil
	})
	return out, err
}

func (p PayrollService) Recalculate(ctx context.Context, s api.Session, id uuid.UUID, reason string) (api.Run, error) {
	var out api.Run
	stat, err := statutory()
	if err != nil {
		return out, err
	}
	err = p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var month time.Time
		if err := tx.QueryRow(ctx, "SELECT month FROM nemura.pay_run WHERE id = $1", id).Scan(&month); err != nil {
			return &payroll.Refusal{Code: payroll.CodeNotFound, Message: "There is no such pay run."}
		}
		pv, has := p.peoplePack(month)
		if _, _, _, err := CalculateRun(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, id, reason, stat, pv, has); err != nil {
			return err
		}
		out, err = p.runOf(ctx, tx, id)
		return err
	})
	return out, err
}

func (p PayrollService) AddOverride(ctx context.Context, s api.Session, run, person uuid.UUID, field, value, reason string) (string, error) {
	v, err := payroll.Money("The overriding figure", value, true)
	if err != nil {
		return "", err
	}
	var id uuid.UUID
	err = p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		id, err = AddOverride(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, run, person, payroll.Override{Field: field, Value: v, Reason: reason})
		return err
	})
	return id.String(), err
}

func (p PayrollService) WithdrawOverride(ctx context.Context, s api.Session, id uuid.UUID) error {
	return p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return WithdrawOverride(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, id)
	})
}

// Approve approves the newest version of a run with the approver's fresh passkey confirmation over its digest (FR-PAY-07, NM-ROLE-04).
func (p PayrollService) Approve(ctx context.Context, s api.Session, id uuid.UUID, version int, digest string) error {
	raw, err := hex.DecodeString(digest)
	if err != nil || len(raw) != 32 {
		return &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The digest is 64 hexadecimal characters: copy it from the run as it was calculated."}
	}
	var confirmed [32]byte
	copy(confirmed[:], raw)
	stepUp := p.Clock.Now()
	if s.StepUpAt != nil {
		stepUp = *s.StepUpAt
	}
	return p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return ApproveRun(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, id, version, confirmed, stepUp)
	})
}

// Changes is the run against last month's (NM-PAY-11).
func (p PayrollService) Changes(ctx context.Context, s api.Session, id uuid.UUID) (api.RunChanges, error) {
	var out api.RunChanges
	err := p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		c, has, err := ChangesFor(ctx, tx, id)
		if err != nil || !has {
			out.Changes, out.Unexplained = []api.RunChange{}, []api.RunUnexplained{}
			return err
		}
		out.HasPrevious = true
		out.GrossNow, out.GrossPrevious, out.NetNow, out.NetPrevious, out.CostNow, out.CostPrevious = cents2(c.GrossNow), cents2(c.GrossPrev), cents2(c.NetNow), cents2(c.NetPrev), cents2(c.CostNow), cents2(c.CostPrev)
		out.Changes, out.Unexplained = []api.RunChange{}, []api.RunUnexplained{}
		for _, ch := range c.Changes {
			out.Changes = append(out.Changes, api.RunChange{PersonID: ch.Person, Person: ch.Name, Cause: ch.Cause, Item: ch.Key, SourceType: ch.SourceType, Amount: cents2(ch.Amount)})
		}
		for _, u := range c.Unexplained {
			out.Unexplained = append(out.Unexplained, api.RunUnexplained{PersonID: u.Person, Person: u.Name, Why: u.Why})
		}
		return nil
	})
	return out, err
}

func cents2(d *apd.Decimal) string {
	out := new(apd.Decimal)
	c := apd.BaseContext.WithPrecision(60)
	c.Rounding = apd.RoundHalfUp
	if _, err := c.Quantize(out, d, -2); err != nil {
		panic(err)
	}
	return out.Text('f')
}
