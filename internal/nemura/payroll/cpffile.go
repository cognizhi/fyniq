package payroll

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
)

// The CPF EZPay (FTP) file (PRD-002 NM-PAY-13; PRD-001 FR-PAY-09; p2-payroll-outputs design D1). Source: CPF Board, "CPF EZPay (FTP) File
// Specifications (effective from 16 January 2025)", last updated Jan 2025, cpf.gov.sg/content/dam/web/employer/making-cpf-contributions/documents/
// CPFEZPayFTPSpecifications.pdf, retrieved 20 Nov 2026. Fixed records of 150 bytes: an Employer Header, an Employer Contribution Summary for each payment
// code, an Employer Contribution Detail for each person and fund, and an Employer Trailer. The sample file on the specification's last page is parsed
// back in the tests.

// RecordLength is the length of every record, in bytes.
const RecordLength = 150

// The payment codes of a summary record (column 27 to 28) that Nemura writes: CPF, the self-help group funds and the skills development levy. 07 (penalty
// interest) and 10 (Community Chest) are the employer's own and are written with a zero amount when 10, never 07.
const (
	CodeCPF   = "01"
	CodeMBMF  = "02"
	CodeSINDA = "03"
	CodeCDAC  = "04"
	CodeECF   = "05"
	CodeChest = "10"
	CodeSDL   = "11"
)

// FundCode is a self-help group fund's payment code.
var FundCode = map[string]string{"MBMF": CodeMBMF, "SINDA": CodeSINDA, "CDAC": CodeCDAC, "ECF": CodeECF}

// CPFEmployer is what the header, the summaries and the trailer name: the employer's UEN, the CPF Submission Number's payment type and serial number, the advice
// code, the month the contributions are for and the moment the file is made. Nothing here is guessed: the payment type (such as PTE) and the serial are the
// employer's own, as its CPF EZPay account holds them.
type CPFEmployer struct {
	ID          string // UEN, NRIC or FIN, 9 or 10 bytes
	PaymentType string // such as PTE, 3 characters
	Serial      string // 01 to 99
	AdviceCode  string // 01 to 99
	Month       time.Time
	SDL         *apd.Decimal // the month's skills development levy, a summary record of code 11; nil for none
	Made        time.Time
}

// CPFPerson is a person's line of the month. CPF is the total contribution payable (the employer's, rounded to the nearest dollar, with the employee's, rounded down),
// as RND-CPF makes it; Fund and SHG are the self-help group fund the person contributes to, with its amount.
type CPFPerson struct {
	Account string       // the CPF account number: first byte S or T, last byte the check letter
	Name    string       // as shown on the identity card
	Status  string       // E (existing, or leaves and joins in the month), L (leaver), N (new joiner), O (joins and leaves in the month)
	OW, AW  *apd.Decimal // ordinary and additional wages, in dollars
	CPF     *apd.Decimal
	Fund    string // MBMF, SINDA, CDAC or ECF; empty for none
	SHG     *apd.Decimal
}

var (
	idForm      = regexp.MustCompile(`^[A-Z0-9]{9,10}$`)
	typeForm    = regexp.MustCompile(`^[A-Z0-9]{3}$`)
	twoDigits   = regexp.MustCompile(`^(0[1-9]|[1-9][0-9])$`)
	accountForm = regexp.MustCompile(`^[ST][0-9]{7}[A-Z]$`)
	// the characters the specification forbids anywhere in the file (note 2): _+$<>:;?!=[]`^|\"~
	forbidden = "_+$<>:;?!=[]`^|\\\"~"
)

// CPFError is a person or an employer detail the file cannot carry.
type CPFError struct{ Message string }

func (e *CPFError) Error() string { return "the CPF file cannot be made: " + e.Message }

func cpfErr(format string, a ...any) error { return &CPFError{Message: fmt.Sprintf(format, a...)} }

// digits writes a non-negative decimal as a fixed number of digits with two implied decimals (9(n)V99): 3000.5 in 10 columns is 0000300050. Nothing is rounded: more than
// two decimals, a negative amount or too large a one is refused.
func digits(d *apd.Decimal, width int, what string) (string, error) {
	if d == nil {
		d = zero()
	}
	if d.Sign() < 0 {
		return "", cpfErr("%s is negative: the file allows no negative amount", what)
	}
	var scaled apd.Decimal
	if _, err := work.Mul(&scaled, d, apd.New(100, 0)); err != nil {
		return "", cpfErr("%s is not a number", what)
	}
	var whole apd.Decimal
	if _, err := work.Quantize(&whole, &scaled, 0); err != nil || whole.Cmp(&scaled) != 0 {
		return "", cpfErr("%s %s has more than two decimals: no figure is rounded here", what, d.Text('f'))
	}
	n := whole.Text('f')
	if len(n) > width {
		return "", cpfErr("%s %s does not fit in %d columns", what, d.Text('f'), width)
	}
	return strings.Repeat("0", width-len(n)) + n, nil
}

// pad writes s left-justified in width columns, or refuses a longer text or one with a forbidden character.
func pad(s string, width int, what string) (string, error) {
	if len(s) > width {
		return "", cpfErr("%s %q is longer than %d columns", what, s, width)
	}
	if strings.ContainsAny(s, forbidden) {
		return "", cpfErr("%s %q has a character the specification forbids in the file (%s)", what, s, forbidden)
	}
	for _, r := range s {
		if r < 32 || r > 126 {
			return "", cpfErr("%s %q has a character outside plain ASCII", what, s)
		}
	}
	return s + strings.Repeat(" ", width-len(s)), nil
}

// Check refuses an employer detail out of the form the specification gives, before anybody is read for the file.
func (e CPFEmployer) Check() error {
	if !idForm.MatchString(e.ID) || !typeForm.MatchString(e.PaymentType) || !twoDigits.MatchString(e.Serial) || !twoDigits.MatchString(e.AdviceCode) {
		return cpfErr("the employer's number, payment type, serial or advice code is not in the form the specification gives")
	}
	if e.Month.IsZero() || e.Made.IsZero() {
		return cpfErr("the month and the moment the file is made are needed")
	}
	return nil
}

// CPFFileName is the file's name: <CSN><Month Paid><Advice Code>.DTL, the Board's own example being 234567891APTE01JAN202201.DTL (the CSN is the UEN, the
// payment type and the serial; the month paid is three letters and the year).
func CPFFileName(e CPFEmployer) string {
	return e.ID + e.PaymentType + e.Serial + strings.ToUpper(e.Month.Format("Jan2006")) + e.AdviceCode + ".DTL"
}

// CPFFile writes the EZPay file of a month: one payment advice. The header, a summary for each of the payment codes 01 to 05, 10 and 11 (with a zero amount for a
// fund nobody contributes to, as the Board's sample does), each person's detail records and the trailer. The summary of the CPF is the sum of its details, and
// the trailer's amount the sum of the summaries.
func CPFFile(e CPFEmployer, people []CPFPerson) ([]byte, error) {
	if err := e.Check(); err != nil {
		return nil, err
	}
	if len(people) == 0 {
		return nil, cpfErr("there is nobody to put in it")
	}
	key := "F" // submission mode: file transfer
	head := func(kind string) string {
		return key + kind + e.ID + strings.Repeat(" ", 10-len(e.ID)) + e.PaymentType + e.Serial + " " + e.AdviceCode
	}
	month := e.Month.Format("200601")
	var out []string
	rec := func(s string) {
		out = append(out, s+strings.Repeat(" ", RecordLength-len(s)))
	}
	rec(head(" ") + e.Made.Format("20060102") + e.Made.Format("150405") + "FTP.DTL")
	sums := map[string]*apd.Decimal{CodeCPF: zero(), CodeMBMF: zero(), CodeSINDA: zero(), CodeCDAC: zero(), CodeECF: zero()}
	donors := map[string]int{}
	var details []string
	for _, p := range people {
		if !accountForm.MatchString(p.Account) {
			return nil, cpfErr("%q is not a CPF account number: S or T, seven digits and a check letter", p.Account)
		}
		if p.Status != "E" && p.Status != "L" && p.Status != "N" && p.Status != "O" {
			return nil, cpfErr("the employment status %q of %s is not E, L, N or O", p.Status, p.Name)
		}
		name, err := pad(strings.ToUpper(p.Name), 66, "the name")
		if err != nil {
			return nil, err
		}
		if p.CPF != nil && p.CPF.Sign() != 0 {
			var frac apd.Decimal
			if _, err := work.Quantize(&frac, p.CPF, 0); err != nil || frac.Cmp(p.CPF) != 0 {
				return nil, cpfErr("the CPF contribution %s of %s is not whole dollars: RND-CPF makes it so, and a figure here that is not is a defect", p.CPF.Text('f'), p.Name)
			}
		}
		amount, err := digits(p.CPF, 12, "the CPF contribution")
		if err != nil {
			return nil, err
		}
		ow, err := digits(p.OW, 10, "the ordinary wages")
		if err != nil {
			return nil, err
		}
		aw, err := digits(p.AW, 10, "the additional wages")
		if err != nil {
			return nil, err
		}
		details = append(details, head("1")+month+CodeCPF+p.Account+amount+ow+aw+p.Status+name)
		sums[CodeCPF] = add(sums[CodeCPF], orZero(p.CPF))
		if p.Fund != "" && p.SHG != nil && p.SHG.Sign() > 0 {
			code, ok := FundCode[p.Fund]
			if !ok {
				return nil, cpfErr("%q is not a self-help group fund the file knows: MBMF, SINDA, CDAC or ECF", p.Fund)
			}
			shg, err := digits(p.SHG, 12, "the fund contribution")
			if err != nil {
				return nil, err
			}
			details = append(details, head("1")+month+code+p.Account+shg+strings.Repeat("0", 20)+" "+name)
			sums[code] = add(sums[code], p.SHG)
			donors[code]++
		}
	}
	total := zero()
	for _, code := range []string{CodeCPF, CodeMBMF, CodeSINDA, CodeCDAC, CodeECF, CodeChest, CodeSDL} {
		amt := zero()
		switch code {
		case CodeSDL:
			amt = orZero(e.SDL)
		case CodeChest:
		default:
			amt = sums[code]
		}
		a, err := digits(amt, 12, "a summary amount")
		if err != nil {
			return nil, err
		}
		n := fmt.Sprintf("%07d", donors[code])
		rec(head("0") + month + code + a + n)
		total = add(total, amt)
	}
	for _, d := range details {
		rec(d)
	}
	t, err := digits(total, 15, "the trailer's amount")
	if err != nil {
		return nil, err
	}
	count := len(out) + 1 // the trailer itself
	rec(head("9") + fmt.Sprintf("%07d", count) + t)
	return []byte(strings.Join(out, "\r\n") + "\r\n"), nil
}

func orZero(d *apd.Decimal) *apd.Decimal {
	if d == nil {
		return zero()
	}
	return d
}
