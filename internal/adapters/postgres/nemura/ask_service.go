package nemura

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/assistant"
	"github.com/cognizhi/fyniq/internal/nemura/features"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
	"github.com/cognizhi/fyniq/internal/ports"
)

// AskService answers a person's question with the assistant's read tools, as that person (NM-AI-02, 03, 06). Without a model route it answers that the assistant is not available.
type AskService struct {
	Store *Store
	Clock ports.Clock
	Packs *PacksService
	// Model is the route; local mode gives the scripted one and the release build none, until processing in Singapore is evidenced for a real one.
	Model assistant.Model
}

var _ api.Asker = AskService{}

// payslipRules say what a line of a payslip is and how it is worked out, as words and with no figure of anyone's pay (NM-AI-06).
var payslipRules = map[string][2]string{
	"basic":     {"Basic salary is the fixed monthly pay in your contract, shown before any deduction. In the month you join or leave it is the part of the month you worked.", "PRD-002 NM-PAY-12"},
	"cpf":       {"Your CPF contribution is the share of your wages that goes to your own CPF account. The CPF Board sets its rate by your age and residency, it is worked out on your ordinary wages up to the ceiling, and your employer pays its own share on top of your pay.", "sg-cpf pack (CPF Board)"},
	"overtime":  {"Overtime pay is paid for the hours you worked beyond your normal hours, at the rate of the Employment Act, which is a multiple of your hourly rate. The hourly rate is worked out from your basic monthly pay and your normal weekly hours.", "sg-people pack part_iv (MOM)"},
	"deduction": {"A deduction is an amount your employer takes from your pay for a reason the Employment Act allows, such as unpaid leave, absence or accommodation you agreed to. Your payslip names each one.", "Employment Act (MOM)"},
	"allowance": {"An allowance is a fixed amount your contract pays on top of your basic salary, such as transport. A one-off allowance is shown as its own line.", "PRD-002 NM-PAY-04"},
	"bonus":     {"A bonus is a payment on top of your salary. It is shown as an additional payment and may count for CPF in a different way from your monthly wages.", "sg-cpf pack (CPF Board)"},
	"sdl":       {"The Skills Development Levy is paid by your employer on your wages. It is not taken from your pay, so it is not on your payslip.", "sg-sdl pack (SkillsFuture Singapore)"},
	"net pay":   {"Your net pay is what is left of your gross pay after your CPF contribution and your deductions. It is the amount paid to your bank account.", "PRD-002 NM-PAY-04"},
	"gross":     {"Gross pay is your basic salary and everything paid on top of it before any deduction.", "PRD-002 NM-PAY-04"},
}

// toolFeature is the Nemura feature a tool reads: the tool is absent while that feature is switched off for the organisation, or held by another system (NM-FEAT-01, NM-EXT-01).
var toolFeature = map[string]string{assistant.ToolLeaveBalance: "leave", assistant.ToolPayDay: "payroll", assistant.ToolPayslipLine: "payroll"}

type toolRunner struct {
	switches features.Switches
	tx       pgx.Tx
	person   uuid.UUID
	now      time.Time
	packs    *PacksService
}

// Enabled says whether the feature a tool reads is on.
func (r toolRunner) Enabled(tool string) bool {
	f, ok := toolFeature[tool]
	return !ok || r.switches[f].State == features.On
}

func (r toolRunner) Run(ctx context.Context, c assistant.Call) (assistant.Result, error) {
	switch c.Tool {
	case assistant.ToolLeaveBalance:
		year := r.now.Year()
		b, err := LeaveBalance(ctx, r.tx, r.person, AnnualLeave, year)
		if err != nil {
			return assistant.Result{}, err
		}
		return assistant.Result{Tool: c.Tool, Facts: map[string]string{"year": fmt.Sprint(year), "entitled": b.Entitled.String(), "taken": b.Taken.String(), "booked": b.Booked.String(), "left": b.Left.String()}, Sources: []string{"your leave record"}}, nil
	case assistant.ToolPayDay:
		var payDay, cutoff, approval int
		err := r.tx.QueryRow(ctx, `SELECT g.pay_day, g.cutoff_days, g.approval_days FROM nemura.person_pay p JOIN nemura.pay_group g ON g.organisation_id = p.organisation_id AND g.id = p.pay_group_id
WHERE p.person_id = $1 AND p.effective_from <= $2 AND (p.effective_to IS NULL OR p.effective_to > $2)`, r.person, day(r.now)).Scan(&payDay, &cutoff, &approval)
		if err != nil {
			return assistant.Result{Tool: c.Tool, Facts: map[string]string{"next_pay_day": "not set yet: ask the people team"}, Sources: []string{"your pay group"}}, nil
		}
		g := payroll.PayGroup{PayDay: payDay, CutoffDays: cutoff, ApprovalDays: approval}
		this := time.Date(r.now.Year(), r.now.Month(), 1, 0, 0, 0, 0, time.UTC)
		next := g.PayDate(this)
		if next.Before(time.Date(r.now.Year(), r.now.Month(), r.now.Day(), 0, 0, 0, 0, time.UTC)) {
			next = g.PayDate(this.AddDate(0, 1, 0))
		}
		return assistant.Result{Tool: c.Tool, Facts: map[string]string{"next_pay_day": next.Format(time.DateOnly)}, Sources: []string{"the calendar of your pay group"}}, nil
	case assistant.ToolPayslipLine:
		rule, ok := payslipRules[strings.ToLower(c.Arg)]
		if !ok {
			return assistant.Result{Tool: c.Tool, Facts: map[string]string{"rule": "That line is not one I can explain yet: ask the people team."}, Sources: []string{"payslip lines"}}, nil
		}
		return assistant.Result{Tool: c.Tool, Facts: map[string]string{"rule": rule[0]}, Sources: []string{rule[1]}}, nil
	case assistant.ToolPolicy:
		v, ok := r.packs.inForce("sg-people", r.now)
		if !ok {
			return assistant.Result{Tool: c.Tool, Facts: map[string]string{"answer": "The Singapore people pack is not loaded for today: ask the people team."}, Sources: []string{"the Singapore people pack"}}, nil
		}
		src := []string{v.ID + " (read from MOM's official pages)"}
		switch c.Arg {
		case "public_holiday":
			for _, h := range pack.HolidaysIn(v, r.now.Year()) {
				d, _ := pack.ParseDate(h)
				if !d.Before(r.now) {
					name, _ := pack.PublicHoliday(v, d)
					return assistant.Result{Tool: c.Tool, Facts: map[string]string{"answer": fmt.Sprintf("The next public holiday is %s on %s.", name, h)}, Sources: src, Confirm: true}, nil
				}
			}
			return assistant.Result{Tool: c.Tool, Facts: map[string]string{"answer": "There is no public holiday left in this year's calendar."}, Sources: src}, nil
		case "sick_leave":
			o, h, ok := pack.SickLeave(v, 12, r.now)
			if !ok {
				break
			}
			return assistant.Result{Tool: c.Tool, Facts: map[string]string{"answer": fmt.Sprintf("After 12 months of service paid sick leave is %d days a year as an outpatient and %d days in all with hospitalisation.", o, h)}, Sources: src, Confirm: true}, nil
		default:
			first, ok1 := pack.AnnualLeaveDays(v, 1, r.now)
			top, ok2 := pack.AnnualLeaveDays(v, 8, r.now)
			if ok1 && ok2 {
				return assistant.Result{Tool: c.Tool, Facts: map[string]string{"answer": fmt.Sprintf("Paid annual leave is %d days in the first year of service, rising by one day a year to %d days.", first, top)}, Sources: src, Confirm: true}, nil
			}
		}
		return assistant.Result{Tool: c.Tool, Facts: map[string]string{"answer": "I could not find that rule in the pack: ask the people team."}, Sources: src}, nil
	}
	return assistant.Result{}, fmt.Errorf("no such tool %q", c.Tool)
}

// Ask answers a question as the signed-in person. The assistant must be on for the organisation, with a route, and the person must have a record. The question is not stored; the audit event says which tools
// ran and whether the question was refused, never what was asked.
func (a AskService) Ask(ctx context.Context, s api.Session, question string) (api.AIAnswer, error) {
	var out api.AIAnswer
	if a.Model == nil {
		return out, api.ErrUnavailable
	}
	err := a.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		set, err := AISettings(ctx, tx)
		if err != nil {
			return err
		}
		if !set.Enabled || set.Route == "none" {
			return &assistant.Refusal{Code: assistant.CodeOff, Message: "The assistant is off: the Owner turns it on in the assistant's settings."}
		}
		person, err := OwnPerson(ctx, tx, s.Member)
		if err != nil {
			return &assistant.Refusal{Code: assistant.CodeInvalid, Message: "Your login is not linked to a person record yet: ask the people team."}
		}
		sw, err := FeatureSwitches(ctx, tx)
		if err != nil {
			return err
		}
		ans, err := assistant.Ask(ctx, a.Model, toolRunner{switches: sw, tx: tx, person: person, now: a.Clock.Now(), packs: a.Packs}, question)
		if err != nil {
			return err
		}
		out = api.AIAnswer{Tools: ans.Tools, Refused: ans.Refused != "", Dropped: len(ans.Dropped), Segments: []api.AISegment{}}
		if out.Tools == nil {
			out.Tools = []string{}
		}
		for _, seg := range ans.Segments {
			src := seg.Sources
			if src == nil {
				src = []string{}
			}
			out.Segments = append(out.Segments, api.AISegment{Text: seg.Text, Sources: src})
		}
		return Audit(ctx, tx, s.Organisation, a.Clock.Now(), user(s.Member), "nemura.assistant.asked", audit.Object{Type: "person", ID: person},
			map[string]audit.Change{"tools": {nil, ans.Tools}, "refused": {nil, ans.Refused != ""}, "model": {nil, a.Model.ID()}}, "")
	})
	return out, err
}
