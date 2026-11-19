package payroll_test

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	. "github.com/cognizhi/fyniq/internal/nemura/payroll"
)

type goldenCase struct {
	Name   string `json:"name"`
	Month  string `json:"month"`
	Person struct {
		Employee      *bool  `json:"employee"`
		Residency     string `json:"residency"`
		PRStart       string `json:"pr_start"`
		BirthDate     string `json:"birth_date"`
		JointElection bool   `json:"joint_election"`
		SHGFund       string `json:"shg_fund"`
		SHGOptOut     bool   `json:"shg_opt_out"`
	} `json:"person"`
	Items []struct {
		Key           string `json:"key"`
		Name          string `json:"name"`
		Amount        string `json:"amount"`
		CPFWages      bool   `json:"cpf_wages"`
		ForMonth      string `json:"for_month"`
		PayableOnDate string `json:"payable_on"`
		Suggested     string `json:"suggested"`
	} `json:"items"`
	YTD *struct {
		OW        string `json:"ow"`
		AWSubject string `json:"aw_subject"`
		AWPaid    string `json:"aw_paid"`
	} `json:"ytd"`
	LastMonth      bool              `json:"last_month"`
	Reimbursements []string          `json:"reimbursements"`
	Expect         map[string]string `json:"expect"`
}

// The golden employee-months (test/differential/payrolls), worked by hand from PRD-001 B.3 and the packs, run through the Go engine; the independent oracle reads the same file `[oracle]`
// (QA-ENG-02). Every expected figure in a case is one the case's `why` works out in words.
// req: FR-PAY-01, FR-PAY-02, FR-PAY-03, FR-PAY-04, FR-PAY-14, FR-PAY-17, QA-ENG-02
func TestTheGoldenEmployeeMonthsAreTheEnginesToTheCent(t *testing.T) {
	raw, err := os.ReadFile("../../../test/differential/payrolls/employee-months.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 16 {
		t.Fatalf("only %d golden months", len(cases))
	}
	p := statutory(t)
	date := func(s string) time.Time {
		if s == "" {
			return time.Time{}
		}
		return day(s)
	}
	for _, c := range cases {
		m := Month{Month: day(c.Month), Person: Person{Employee: c.Person.Employee == nil || *c.Person.Employee, Residency: c.Person.Residency, BirthDate: day(c.Person.BirthDate), JointElection: c.Person.JointElection,
			SHGFund: c.Person.SHGFund, SHGOptOut: c.Person.SHGOptOut}, LastMonth: c.LastMonth}
		if c.Person.PRStart != "" {
			d := day(c.Person.PRStart)
			m.Person.PRStart = &d
		}
		for _, it := range c.Items {
			m.Items = append(m.Items, Item{Key: it.Key, Name: it.Name, Amount: D(it.Amount), CPFWages: it.CPFWages, ForMonth: date(it.ForMonth), PayableOn: date(it.PayableOnDate), Suggested: it.Suggested})
		}
		if c.YTD != nil {
			m.YTD = YearToDate{Known: true, OW: D(c.YTD.OW), AWSubject: D(c.YTD.AWSubject), AWPaid: D(c.YTD.AWPaid)}
		}
		for _, x := range c.Reimbursements {
			m.Reimbursements = append(m.Reimbursements, D(x))
		}
		r, err := Calculate(m, p)
		if want, ok := c.Expect["error"]; ok {
			if want != "needs_year_to_date" || !errors.Is(err, ErrNeedsYearToDate) {
				t.Errorf("%s: error %v, want %s", c.Name, err, want)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		got := map[string]*apd.Decimal{"gross": r.Gross, "ow_subject": r.OWSubject, "aw_subject": r.AWSubject, "cpf_total": r.CPF.Total, "cpf_employee": r.CPF.Employee, "cpf_employer": r.CPF.Employer,
			"sdl": r.SDL, "shg": r.SHG, "net": r.Net, "disburse": r.Disburse}
		if r.Refund != nil {
			got["refund_wages"] = r.Refund.Wages
		}
		for k, want := range c.Expect {
			g, ok := got[k]
			if !ok {
				t.Errorf("%s: no %s in the result", c.Name, k)
				continue
			}
			if g.Cmp(D(want)) != 0 {
				t.Errorf("%s: %s is %s, want %s", c.Name, k, g.Text('f'), want)
			}
		}
	}
}
