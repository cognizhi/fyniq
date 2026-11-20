package nemura

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/access"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The standalone hand-off files of an approved pay run (PRD-002 NM-PAY-05; design O3): a journal file and a bank payment file, as generic CSV with a manifest of the run, the version, the totals and the
// SHA-256 of the exact bytes. No bank's own format is guessed (`[live-verify]` against a named bank's). A file is never proof of payment: nothing here marks a payment paid, and the manifest says so.

// ExportNote is printed in every manifest.
const ExportNote = "This file is not proof of payment: a payment is paid only when the bank's feed matches the debit or a person records the outcome with evidence."

// Skipped is a person left out of a payment file, with why.
type Skipped struct {
	PersonID string `json:"person_id"`
	Person   string `json:"person"`
	Reason   string `json:"reason"`
}

// Manifest describes a file as handed out.
type Manifest struct {
	ID        string    `json:"id"`
	Run       string    `json:"run_id"`
	Version   int       `json:"version"`
	Kind      string    `json:"kind"`
	Month     string    `json:"wage_month"`
	Lines     int       `json:"lines"`
	Total     string    `json:"total"`
	SHA256    string    `json:"sha256"`
	Created   string    `json:"generated_at"`
	Note      string    `json:"note"`
	Skipped   []Skipped `json:"skipped"`
	Reference string    `json:"reference,omitempty"`
	FileName  string    `json:"file_name,omitempty"`
	Parts     int       `json:"parts,omitempty"` // an IR8A payload of a year of more than one part
	Part      int       `json:"part,omitempty"`
}

// safeCell keeps a spreadsheet from running a cell as a formula: a value that starts with =, +, - or @ (or a tab or return) is prefixed with an apostrophe.
func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func approvedVersion(ctx context.Context, tx pgx.Tx, run uuid.UUID, version int) (RunRow, VersionRow, error) {
	r, vs, err := RunOf(ctx, tx, run)
	if err != nil {
		return r, VersionRow{}, err
	}
	for _, v := range vs {
		if v.Number == version {
			if v.ApprovedBy == nil || v.State == "superseded" {
				return r, v, &payroll.Refusal{Code: payroll.CodeConflict, Message: "A file is made from an approved version that a corrected one has not superseded."}
			}
			return r, v, nil
		}
	}
	return r, VersionRow{}, &payroll.Refusal{Code: payroll.CodeNotFound, Message: "There is no such version of that pay run."}
}

func record(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, run uuid.UUID, v VersionRow, kind string, file []byte, lines int, skipped []Skipped, total string, r RunRow) (Manifest, error) {
	sum := sha256.Sum256(file)
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.pay_export (id, run_id, version_id, kind, sha256, line_count, skipped_count, total, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::numeric, $9, $10)`,
		id, run, v.ID, kind, sum[:], lines, len(skipped), total, by, at); err != nil {
		return Manifest{}, runError(err)
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.pay_export.created", audit.Object{Type: "pay_export", ID: id},
		map[string]audit.Change{"run": {nil, run.String()}, "version": {nil, v.Number}, "kind": {nil, kind}, "lines": {nil, lines}, "skipped": {nil, len(skipped)}, "sha256": {nil, hex.EncodeToString(sum[:])}}, ""); err != nil {
		return Manifest{}, err
	}
	if skipped == nil {
		skipped = []Skipped{}
	}
	return Manifest{ID: id.String(), Run: run.String(), Version: v.Number, Kind: kind, Month: day(r.Month), Lines: lines, Total: total, SHA256: hex.EncodeToString(sum[:]), Created: at.UTC().Format(time.RFC3339), Note: ExportNote, Skipped: skipped}, nil
}

// ExportJournal makes the journal file of an approved version: B.3's balanced entry for the whole run (Fyniq posts it by department when linked), one line for each account, debit and credit, in cents.
// It refuses an entry that does not balance, which would be a defect and never a rounding.
func ExportJournal(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, run uuid.UUID, version int) ([]byte, Manifest, error) {
	r, v, err := approvedVersion(ctx, tx, run, version)
	if err != nil {
		return nil, Manifest{}, err
	}
	lines, err := RunLines(ctx, tx, v.ID)
	if err != nil {
		return nil, Manifest{}, err
	}
	held := dec("0")
	for _, l := range lines {
		held = addDec(held, dec(l.Held))
	}
	t := payroll.Totals{Gross: dec(v.Totals.Gross), EmployeeCPF: dec(v.Totals.CPFEmployee), EmployerCPF: dec(v.Totals.CPFEmployer), SDLPayable: dec(v.Totals.SDLPayable), SHG: dec(v.Totals.SHG), Deductions: dec(v.Totals.Deductions),
		Reimbursements: dec(v.Totals.Reimbursements), Held: held, Disburse: dec(v.Totals.Disburse), Net: dec(v.Totals.Net)}
	// The version's Disburse already has the held monies taken out: the entry's 2100 is what is disbursed plus what is held.
	posts := payroll.Postings(t)
	if !payroll.Balanced(posts) {
		return nil, Manifest{}, &payroll.Refusal{Code: payroll.CodeConflict, Message: "The run's journal does not balance: it is not made. This is a defect for the people who run Nemura, never a rounding."}
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"run_id", "version", "wage_month", "account", "debit", "credit", "description"})
	debit := dec("0")
	n := 0
	for _, p := range posts {
		if p.Debit.Sign() == 0 && p.Credit.Sign() == 0 {
			continue
		}
		n++
		debit = addDec(debit, p.Debit)
		_ = w.Write([]string{run.String(), itoa(v.Number), r.Month.Format("2006-01"), safeCell(p.Account), cents2(p.Debit), cents2(p.Credit), safeCell(p.Why)})
	}
	w.Flush()
	m, err := record(ctx, tx, org, at, by, run, v, "journal", buf.Bytes(), n, nil, cents2(debit), r)
	return buf.Bytes(), m, err
}

func itoa(n int) string { return strconv.Itoa(n) }

// ExportPayments makes the bank payment file of an approved version: one row for each person owed money whose bank account is on record, with the account opened from its sealed form and each read
// audited. A person is left out, and listed in the manifest with why, when nothing is owed (held under a tax-clearance hold), no bank account is on record, the payment is already on its way or paid, or
// its outcome is unknown, which blocks a new payment until it is resolved (NM-PAY-10).
func ExportPayments(ctx context.Context, tx pgx.Tx, c ports.Cipher, org uuid.UUID, at time.Time, by, run uuid.UUID, version int) ([]byte, Manifest, error) {
	r, v, err := approvedVersion(ctx, tx, run, version)
	if err != nil {
		return nil, Manifest{}, err
	}
	pays, err := PaymentsOf(ctx, tx, run)
	if err != nil {
		return nil, Manifest{}, err
	}
	state := map[uuid.UUID]string{}
	for _, l := range pays.Lines {
		state[l.Person] = l.State
	}
	lines, err := RunLines(ctx, tx, v.ID)
	if err != nil {
		return nil, Manifest{}, err
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	ref := "Salary " + r.Month.Format("2006-01")
	_ = w.Write([]string{"payee", "account_number", "amount", "currency", "reference"})
	total := dec("0")
	var skipped []Skipped
	n := 0
	skip := func(l RunLine, why string) {
		skipped = append(skipped, Skipped{PersonID: l.Person.String(), Person: l.PersonName, Reason: why})
	}
	for _, l := range lines {
		owed := dec(l.Disburse)
		switch {
		case owed.Sign() <= 0 && dec(l.Held).Sign() > 0:
			skip(l, "Held under a tax-clearance hold: nothing is disbursed until it is released.")
			continue
		case owed.Sign() <= 0:
			continue
		}
		switch state[l.Person] {
		case payroll.PayUnknown:
			skip(l, "The outcome of an earlier payment is unknown: record whether it was paid, rejected or returned before paying again.")
			continue
		case payroll.PayPaid, payroll.PayReleased, payroll.PaySent, payroll.PayAccepted:
			skip(l, "The payment is already on its way or paid.")
			continue
		}
		acct, err := bankForPayment(ctx, tx, c, org, at, by, l.Person)
		if err != nil {
			return nil, Manifest{}, err
		}
		if acct == "" {
			skip(l, "No salary bank account is on record: the person proposes it and an approver and the person confirm it first.")
			continue
		}
		n++
		total = addDec(total, owed)
		_ = w.Write([]string{safeCell(l.PersonName), safeCell(acct), cents2(owed), "SGD", safeCell(ref)})
	}
	w.Flush()
	m, err := record(ctx, tx, org, at, by, run, v, "payments", buf.Bytes(), n, skipped, cents2(total), r)
	m.Reference = ref
	return buf.Bytes(), m, err
}

// bankForPayment opens a person's salary bank account for a payment file. The caller has checked the payroll right; the read is audited like any read of the field. No account on record is "".
func bankForPayment(ctx context.Context, tx pgx.Tx, c ports.Cipher, org uuid.UUID, at time.Time, by, person uuid.UUID) (string, error) {
	var sealed []byte
	err := tx.QueryRow(ctx, "SELECT bank_sealed FROM nemura.person_secret WHERE person_id = $1 AND bank_sealed IS NOT NULL", person).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if err := auditRead(ctx, tx, org, at, by, person, CapBank, access.Full); err != nil {
		return "", err
	}
	plain, err := c.Open(ctx, sealed, aad(org, person, "bank"))
	return string(plain), err
}

// CPFInput is what only the employer knows of its CPF EZPay submission: the payment type, serial number and advice code of its CPF Submission Number, as its CPF EZPay account holds
// them. They are never guessed (p2-payroll-outputs design D1).
type CPFInput struct{ PaymentType, Serial, AdviceCode string }

// ExportCPF makes the CPF EZPay (FTP) file of an approved version (PRD-002 NM-PAY-13): the CPF Board's own layout, one detail record for each person with a contribution and one for each
// self-help group fund they pay, the skills development levy as its own summary, the employer's UEN from the letter settings, and each person's CPF account number, which is their NRIC,
// opened from its sealed form with each read audited. A person is left out, and listed in the manifest with why, when no NRIC is on record or it is not an S or T number (a CPF account), or
// the person has no contribution this month. A file is never proof that CPF was submitted or paid.
func ExportCPF(ctx context.Context, tx pgx.Tx, c ports.Cipher, org uuid.UUID, at time.Time, by, run uuid.UUID, version int, in CPFInput) ([]byte, Manifest, error) {
	r, v, err := approvedVersion(ctx, tx, run, version)
	if err != nil {
		return nil, Manifest{}, err
	}
	var uen string
	if err := tx.QueryRow(ctx, "SELECT uen FROM nemura.letter_settings").Scan(&uen); errors.Is(err, pgx.ErrNoRows) {
		return nil, Manifest{}, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The company's UEN is not set: record it under the letter settings first, as the CPF file names the employer by it."}
	} else if err != nil {
		return nil, Manifest{}, err
	}
	lines, err := RunLines(ctx, tx, v.ID)
	if err != nil {
		return nil, Manifest{}, err
	}
	e := payroll.CPFEmployer{ID: strings.ToUpper(strings.TrimSpace(uen)), PaymentType: strings.ToUpper(in.PaymentType), Serial: in.Serial, AdviceCode: in.AdviceCode, Month: r.Month,
		SDL: dec(v.Totals.SDLPayable), Made: at}
	if err := e.Check(); err != nil {
		return nil, Manifest{}, &payroll.Refusal{Code: payroll.CodeInvalid, Message: err.Error()}
	}
	var people []payroll.CPFPerson
	var skipped []Skipped
	total := dec("0")
	for _, l := range lines {
		cpf, shg := dec(l.CPFTotal), dec(l.SHG)
		if cpf.Sign() <= 0 && shg.Sign() <= 0 {
			continue
		}
		skip := func(why string) {
			skipped = append(skipped, Skipped{PersonID: l.Person.String(), Person: l.PersonName, Reason: why})
		}
		var sealed []byte
		var start time.Time
		var leaving *time.Time
		var fund *string
		if err := tx.QueryRow(ctx, `SELECT p.employment_start, p.leaving_date, s.nric_sealed,
  (SELECT shg_fund FROM nemura.person_pay pp WHERE pp.person_id = p.id AND pp.effective_from <= $2 AND (pp.effective_to IS NULL OR pp.effective_to > $2) ORDER BY pp.effective_from DESC LIMIT 1)
FROM nemura.person p LEFT JOIN nemura.person_secret s ON s.person_id = p.id WHERE p.id = $1`, l.Person, day(r.Month)).Scan(&start, &leaving, &sealed, &fund); err != nil {
			return nil, Manifest{}, err
		}
		if sealed == nil {
			skip("No NRIC is on record: the CPF account number of a citizen or permanent resident is their NRIC.")
			continue
		}
		if err := auditRead(ctx, tx, org, at, by, l.Person, CapNRIC, access.Full); err != nil {
			return nil, Manifest{}, err
		}
		plain, err := c.Open(ctx, sealed, aad(org, l.Person, "nric"))
		if err != nil {
			return nil, Manifest{}, err
		}
		account := strings.ToUpper(strings.TrimSpace(string(plain)))
		if !strings.HasPrefix(account, "S") && !strings.HasPrefix(account, "T") {
			skip("The identification number is not an S or T number, so it is not a CPF account number.")
			continue
		}
		status := "E"
		inMonth := func(t time.Time) bool { return t.Year() == r.Month.Year() && t.Month() == r.Month.Month() }
		joined, left := inMonth(start), leaving != nil && inMonth(*leaving)
		switch {
		case joined && left:
			status = "O"
		case joined:
			status = "N"
		case left:
			status = "L"
		}
		p := payroll.CPFPerson{Account: account, Name: l.PersonName, Status: status, OW: dec(l.OWSubject), AW: dec(l.AWSubject), CPF: cpf}
		if fund != nil && shg.Sign() > 0 {
			p.Fund, p.SHG = *fund, shg
		}
		people = append(people, p)
		total = addDec(addDec(total, cpf), shg)
	}
	if len(people) == 0 {
		return nil, Manifest{}, &payroll.Refusal{Code: payroll.CodeConflict, Message: "Nobody in this version has a contribution that can go in the file."}
	}
	file, err := payroll.CPFFile(e, people)
	if err != nil {
		var ce *payroll.CPFError
		if errors.As(err, &ce) {
			return nil, Manifest{}, &payroll.Refusal{Code: payroll.CodeInvalid, Message: ce.Error()}
		}
		return nil, Manifest{}, err
	}
	total = addDec(total, dec(v.Totals.SDLPayable))
	m, err := record(ctx, tx, org, at, by, run, v, "cpf", file, len(people), skipped, cents2(total), r)
	m.FileName = payroll.CPFFileName(e)
	return file, m, err
}
