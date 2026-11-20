package nemura

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/nemura/access"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
	"github.com/cognizhi/fyniq/internal/ports"
)

// AISInput is what only the employer knows of its Auto-Inclusion Scheme submission: who is authorised, how to reach them and whether this is the original, an amendment or a revision
// (IRAS data items A5 to A8). Nothing here is guessed (p2-payroll-outputs design D2).
type AISInput struct {
	AuthorisedPerson, ContactNumber, Email, SubmissionType string
	Part                                                   int // which payload of a year of more than MaxAISRecords people; 0 is the first
	Chunk                                                  int // the most records in a payload; 0 is IRAS's limit (tests use a smaller one)
}

type yearPerson struct {
	id    uuid.UUID
	name  string
	items []payroll.MapItem
	cpf   *apd.Decimal
}

// incomeYearLines gathers, for each person, the pay items and the employee's CPF of the income year: every run whose pay date falls in the year, each from its newest approved version
// that a correction has not superseded (PRD-002 NM-PAY-14; the AIS counts income by the day it is paid). It returns the runs it used, newest last.
func incomeYearLines(ctx context.Context, tx pgx.Tx, year int) ([]yearPerson, []string, error) {
	rows, err := tx.Query(ctx, "SELECT id FROM nemura.pay_run WHERE month >= $1 AND month < $2 ORDER BY month", time.Date(year-1, 12, 1, 0, 0, 0, 0, time.UTC), time.Date(year+1, 2, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return nil, nil, err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	byPerson := map[uuid.UUID]*yearPerson{}
	var used []string
	for _, id := range ids {
		r, vs, err := RunOf(ctx, tx, id)
		if err != nil {
			return nil, nil, err
		}
		if r.PayDate.Year() != year {
			continue
		}
		var best *VersionRow
		for i := range vs {
			if vs[i].ApprovedBy != nil && vs[i].State != "superseded" && (best == nil || vs[i].Number > best.Number) {
				best = &vs[i]
			}
		}
		if best == nil {
			continue
		}
		lines, err := RunLines(ctx, tx, best.ID)
		if err != nil {
			return nil, nil, err
		}
		used = append(used, fmt.Sprintf("%s v%d", day(r.Month)[:7], best.Number))
		for _, l := range lines {
			p := byPerson[l.Person]
			if p == nil {
				p = &yearPerson{id: l.Person, name: l.PersonName, cpf: dec("0")}
				byPerson[l.Person] = p
			}
			p.cpf = addDec(p.cpf, dec(l.CPFEmployee))
			for _, it := range l.Items {
				if it.Class == "deduction" {
					continue
				}
				p.items = append(p.items, payroll.MapItem{Nature: it.Nature, Class: it.Class, Amount: dec(it.Amount)})
			}
		}
	}
	out := make([]yearPerson, 0, len(byPerson))
	for _, p := range byPerson {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].name != out[j].name {
			return out[i].name < out[j].name
		}
		return out[i].id.String() < out[j].id.String()
	})
	return out, used, nil
}

func whole(d *apd.Decimal) *int64 {
	var r apd.Decimal
	c := apd.BaseContext.WithPrecision(40)
	c.Rounding = apd.RoundHalfUp
	if _, err := c.Quantize(&r, d, 0); err != nil {
		return nil
	}
	n, err := r.Int64()
	if err != nil || n == 0 {
		return nil
	}
	return &n
}

// ExportAIS makes the IR8A / Auto-Inclusion Scheme payload (IRAS AIS-API 2.0 version 2.6; PRD-002 NM-PAY-14, PRD-001 FR-PAY-10) of the income year the run's pay date falls in, from every
// approved pay run of that year: each person's pay items sum into the IR8A's boxes by their nature (payroll.IR8AFrom: [accountant to confirm]), the employee's CPF is the year's,
// the NRIC opens from its sealed form and the sex, address and designation from theirs, each read audited. A person with no NRIC of a kind IRAS takes, no particulars or no date of birth
// is left out and listed with why; a payload that breaks one of IRAS's data-item rules is refused with each rule named, and a year of more than 2,000 people is made in parts of at most 2,000, a part at a time (AISInput.Part), as IRAS takes 2,000 records in a submission (A11).
// The file is the payload to send, not proof that IRAS received it.
func ExportAIS(ctx context.Context, tx pgx.Tx, c ports.Cipher, org uuid.UUID, at time.Time, by, run uuid.UUID, version int, in AISInput) ([]byte, Manifest, error) {
	r, v, err := approvedVersion(ctx, tx, run, version)
	if err != nil {
		return nil, Manifest{}, err
	}
	year := r.PayDate.Year()
	var uen string
	if err := tx.QueryRow(ctx, "SELECT uen FROM nemura.letter_settings").Scan(&uen); errors.Is(err, pgx.ErrNoRows) {
		return nil, Manifest{}, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The company's UEN is not set: record it under the letter settings first, as IRAS names the employer by it."}
	} else if err != nil {
		return nil, Manifest{}, err
	}
	kind := strings.ToUpper(strings.TrimSpace(in.SubmissionType))
	if kind == "" {
		kind = "O"
	}
	org8 := payroll.AISOrg{CurrentYear: at.Year(), IncomeYear: year, EmployerTaxRef: strings.ToUpper(strings.TrimSpace(uen)), AuthorisedPerson: strings.TrimSpace(in.AuthorisedPerson),
		ContactNumber: strings.TrimSpace(in.ContactNumber), Email: strings.TrimSpace(in.Email), SubmissionType: kind, SoftwareCompany: "Fyniq", SoftwareName: "Nemura"}
	yp, _, err := incomeYearLines(ctx, tx, year)
	if err != nil {
		return nil, Manifest{}, err
	}
	yearStart, yearEnd := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	var people []payroll.AISPerson
	var skipped []Skipped
	var total int64
	for _, p := range yp {
		skip := func(why string) {
			skipped = append(skipped, Skipped{PersonID: p.id.String(), Person: p.name, Reason: why})
		}
		var sealed []byte
		var start time.Time
		var leaving, birth *time.Time
		if err := tx.QueryRow(ctx, `SELECT p.employment_start, p.leaving_date, s.nric_sealed,
  (SELECT birth_date FROM nemura.person_classification c WHERE c.person_id = p.id AND c.effective_from <= $2 AND (c.effective_to IS NULL OR c.effective_to > $2) ORDER BY c.effective_from DESC LIMIT 1)
FROM nemura.person p LEFT JOIN nemura.person_secret s ON s.person_id = p.id WHERE p.id = $1`, p.id, yearEnd).Scan(&start, &leaving, &sealed, &birth); err != nil {
			return nil, Manifest{}, err
		}
		if sealed == nil {
			skip("No NRIC or FIN is on record: the IR8A names the employee by it.")
			continue
		}
		if birth == nil {
			skip("No date of birth is on record: record the classification with it.")
			continue
		}
		if err := auditRead(ctx, tx, org, at, by, p.id, CapNRIC, access.Full); err != nil {
			return nil, Manifest{}, err
		}
		plain, err := c.Open(ctx, sealed, aad(org, p.id, "nric"))
		if err != nil {
			return nil, Manifest{}, err
		}
		id := strings.ToUpper(strings.TrimSpace(string(plain)))
		idType := ""
		switch {
		case strings.HasPrefix(id, "S") || strings.HasPrefix(id, "T"):
			idType = "1"
		case strings.HasPrefix(id, "F") || strings.HasPrefix(id, "G") || strings.HasPrefix(id, "M"):
			idType = "2"
		default:
			skip("The identification number is neither an NRIC nor a FIN: a work permit, passport or Malaysian IC is not yet written.")
			continue
		}
		part, ok, err := ir8aOf(ctx, tx, c, org, at, by, p.id)
		if err != nil {
			return nil, Manifest{}, err
		}
		if !ok {
			skip("The IR8A particulars (sex and address) are not on record: set them on the person first.")
			continue
		}
		boxes := payroll.IR8AFrom(p.items)
		ap := payroll.AISPerson{IDType: idType, IDNumber: id, Name: p.name, DateOfBirth: day(*birth), Sex: part.Sex, AddressType: part.AddressType, Address1: part.Address1, Address2: part.Address2,
			Address3: part.Address3, PostalCode: part.PostalCode, Country: part.Country, Designation: part.Designation, Commencement: day(start),
			From: day(yearStart), To: day(yearEnd), Salary: boxes.Salary, Bonus: boxes.Bonus, Allowances: boxes.Allowances, Commission: boxes.Commission, Gratuity: boxes.Gratuity,
			CompensationForLoss: boxes.CompensationForLoss, EmployeeCPF: whole(p.cpf)}
		if boxes.CompensationForLoss != nil {
			ap.CompensationIndicator = "Y"
		}
		if !start.Before(yearStart) {
			ap.From = day(start)
		}
		if leaving != nil && leaving.Year() == year {
			ap.Cessation, ap.To = day(*leaving), day(*leaving)
		}
		total += ap.TotalIncome()
		people = append(people, ap)
	}
	if len(people) == 0 {
		return nil, Manifest{}, &payroll.Refusal{Code: payroll.CodeConflict, Message: fmt.Sprintf("Nobody paid in %d can go in the file: see who was left out and why.", year)}
	}
	chunks := payroll.Chunks(people, in.Chunk)
	part := max(in.Part, 1)
	if part > len(chunks) {
		return nil, Manifest{}, &payroll.Refusal{Code: payroll.CodeInvalid, Message: fmt.Sprintf("part: the year's file has %d part(s) of at most %d people (%d people in all); part %d does not exist.", len(chunks), payroll.MaxAISRecords, len(people), part)}
	}
	people = chunks[part-1]
	total = 0
	for _, p := range people {
		total += p.TotalIncome()
	}
	file, findings := payroll.AISPayload(org8, people, false, false)
	if len(findings) > 0 {
		var lines []string
		for i, f := range findings {
			if i == 8 {
				lines = append(lines, fmt.Sprintf("and %d more", len(findings)-i))
				break
			}
			lines = append(lines, f.String())
		}
		return nil, Manifest{}, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "IRAS's data-item rules are broken: " + strings.Join(lines, "; ")}
	}
	m, err := record(ctx, tx, org, at, by, run, v, "ais", file, len(people), skipped, cents2(dec(fmt.Sprint(total))), r)
	m.FileName = fmt.Sprintf("ir8a-%d.json", year)
	if len(chunks) > 1 {
		m.Parts, m.Part = len(chunks), part
		m.FileName = fmt.Sprintf("ir8a-%d-part-%d-of-%d.json", year, part, len(chunks))
	}
	return file, m, err
}
