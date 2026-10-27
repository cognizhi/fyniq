package chart

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The codes of an account's rules (FR-COA-02, FR-COA-03).
const (
	CodeAccountCode = "COA-01" // the code is not an account code
	CodeText        = "COA-02" // a text is blank or too long
	CodeType        = "COA-03" // not one of the five types
	CodeFunction    = "COA-04" // not a function
	CodeGSTCode     = "COA-05" // not the form of a GST code
	CodeCurrency    = "COA-06" // not a currency Fyniq knows
	CodeRole        = "COA-07" // not a system role
	CodeProtected   = "COA-08" // the control or payroll-cost flag changes only with approval
	CodeSystem      = "COA-09" // a system account stays active
	CodeRevalue     = "COA-10" // only a monetary asset or liability is revalued at month end
	CodeUsefulLife  = "COA-11" // a default useful life of 1 to 600 months, only on an asset account of the register
)

// Problem is one rule that failed.
type Problem struct {
	Code, Field, Message, Fix string
}

var (
	accountCode = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.-]{0,19}$`)
	gstCode     = regexp.MustCompile(`^[A-Z]{2,4}$`)
	functions   = []string{"COGS", "R&D", "S&M", "G&A", "Other"}
)

func checkAttributes(name string, a storage.AccountAttributes, add func(code, field, message, fix string)) {
	if strings.TrimSpace(name) == "" || len([]rune(name)) > 200 {
		add(CodeText, "name", "is blank or longer than 200 characters", "Enter the account's name")
	}
	for field, v := range map[string]string{"report_group": a.ReportGroup, "cash_flow_line": a.CashFlowLine} {
		if len([]rune(v)) > 60 || (v != "" && strings.TrimSpace(v) == "") {
			add(CodeText, field, "is only spaces or longer than 60 characters", "Shorten it, or leave it empty")
		}
	}
	if len([]rune(a.Guidance)) > 500 || (a.Guidance != "" && strings.TrimSpace(a.Guidance) == "") {
		add(CodeText, "guidance", "is only spaces or longer than 500 characters", "Shorten it, or leave it empty")
	}
	if a.Function != "" && !slices.Contains(functions, a.Function) {
		add(CodeFunction, "function", fmt.Sprintf("%q is not a function", a.Function), "Use COGS, R&D, S&M, G&A or Other, or leave it empty")
	}
	if a.DefaultGSTCode != "" && !gstCode.MatchString(a.DefaultGSTCode) {
		add(CodeGSTCode, "default_gst_code", fmt.Sprintf("%q is not a GST code", a.DefaultGSTCode), "Use the GST code, such as TX, SR or NA")
	}
	if a.Currency != "" {
		if _, err := money.Lookup(a.Currency); err != nil {
			add(CodeCurrency, "currency", fmt.Sprintf("%q is not a currency Fyniq knows", a.Currency), "Use an ISO 4217 code, such as SGD or USD")
		}
	}
}

// checkRevalue refuses month-end revaluation on an account that is not a monetary item (FR-GL-04; rev 0.72): only an
// asset or a liability, never prepayments, deferred revenue or customer deposits, whose amounts are not settled in money.
func checkRevalue(t storage.AccountType, role storage.AccountRole, revalue bool, add func(code, field, message, fix string)) {
	switch {
	case !revalue:
	case t != storage.Asset && t != storage.Liability:
		add(CodeRevalue, "revalue", fmt.Sprintf("a %s account is not revalued at month end", t), "Revalue only asset and liability accounts settled in money")
	case role == "prepayments" || role == "deferred-revenue" || role == "customer-deposits":
		add(CodeRevalue, "revalue", fmt.Sprintf("the %s account is not settled in money, so it is not revalued", role),
			"Leave it at the rate it was recorded at (FR-GL-04)")
	}
}

// checkUsefulLife refuses a default useful life outside 1 to 600 months, or on an account no asset sits on: a heading,
// or an account outside the asset register's report groups (FR-COA-02, FR-FA-01; rev 0.151).
func checkUsefulLife(group bool, a storage.AccountAttributes, add func(code, field, message, fix string)) {
	switch {
	case a.UsefulLife == 0:
	case a.UsefulLife < 1 || a.UsefulLife > 600:
		add(CodeUsefulLife, "useful_life_months", fmt.Sprintf("%d is not a useful life of 1 to 600 months", a.UsefulLife),
			"Enter whole months from 1 to 600, such as 36, or leave it empty")
	case group || !slices.Contains(asset.RegisterGroups, a.ReportGroup):
		add(CodeUsefulLife, "useful_life_months", "only an asset account of the register (Property, plant & equipment or Intangible assets) has a default useful life",
			"Leave it empty, or give the account one of those report groups first")
	}
}

// CheckAccount returns every rule a new account breaks, or nil.
func CheckAccount(a storage.NewAccount) []Problem {
	var out []Problem
	add := func(code, field, message, fix string) { out = append(out, Problem{code, field, message, fix}) }
	if !accountCode.MatchString(a.Code) {
		add(CodeAccountCode, "code", fmt.Sprintf("%q is not an account code", a.Code), "Use 1 to 20 letters, digits, dots or hyphens, starting with a letter or digit, such as 6110")
	}
	if !slices.Contains(types, a.Type) {
		add(CodeType, "type", fmt.Sprintf("%q is not an account type", a.Type), "Use asset, liability, equity, revenue or expense")
	}
	if a.Role != "" && !slices.Contains(storage.SystemRoles, a.Role) {
		add(CodeRole, "role", fmt.Sprintf("%q is not a system role", a.Role), "Leave the role empty; system roles come with the standard chart")
	}
	checkAttributes(a.Name, a.AccountAttributes, add)
	checkRevalue(a.Type, a.Role, a.Revalue, add)
	checkUsefulLife(a.Group, a.AccountAttributes, add)
	return out
}

// CheckChange returns every rule changing before to c breaks, or nil. The control and payroll-cost flags
// change only with an approval (FR-COA-02), which arrives with the approval engine; until then a change
// to them is refused, never applied.
func CheckChange(before storage.Account, c storage.AccountChange) []Problem {
	var out []Problem
	add := func(code, field, message, fix string) { out = append(out, Problem{code, field, message, fix}) }
	checkAttributes(c.Name, c.AccountAttributes, add)
	checkRevalue(before.Type, before.Role, c.Revalue, add)
	checkUsefulLife(c.Group, c.AccountAttributes, add)
	if c.Control != before.Control {
		add(CodeProtected, "control", "changes only with an approval", "Ask for the change once approvals are available (p1-approval-engine)")
	}
	if c.PayrollCost != before.PayrollCost {
		add(CodeProtected, "payroll_cost", "changes only with an approval, like the control-account flag", "Ask for the change once approvals are available (p1-approval-engine)")
	}
	if before.Role != "" && before.Active && !c.Active {
		add(CodeSystem, "active", fmt.Sprintf("account %s is a system account (%s), so it stays active", before.Code, before.Role),
			"The engine posts to it (FR-COA-03); deactivate another account")
	}
	return out
}
