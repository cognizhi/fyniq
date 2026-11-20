package nemura

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/letters"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
	"github.com/cognizhi/fyniq/internal/ports"
)

// PublishPayslips makes the payslip of each person of an approved run version (NM-PAY-04): MOM's twelve items as data, the PDF drawn from them by the renderer, its SHA-256, and a reference.
// A version that is not approved, or was superseded by a corrected one, is refused; a version is published once. The employer's name is the letterhead's. The audit event of each payslip
// names it, never an amount. It returns how many were made.
func PublishPayslips(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, runID uuid.UUID, version int, r letters.Renderer, blobs ports.BlobStore, c ports.Cipher) (int, error) {
	if r == nil || blobs == nil || c == nil { // the detail is sealed under the person's own key, so a deployment with no key service makes no payslip (NM-SEC-08)
		return 0, &payroll.Refusal{Code: "NM_UNAVAILABLE", Message: "This deployment has no file store or renderer for payslips yet: ask whoever runs Nemura."}
	}
	run, vs, err := RunOf(ctx, tx, runID)
	if err != nil {
		return 0, err
	}
	var v *VersionRow
	for i := range vs {
		if vs[i].Number == version {
			v = &vs[i]
		}
	}
	if v == nil {
		return 0, &payroll.Refusal{Code: payroll.CodeNotFound, Message: "There is no such version of that pay run."}
	}
	if v.ApprovedBy == nil || v.State == "superseded" {
		return 0, &payroll.Refusal{Code: payroll.CodeConflict, Message: "Payslips are made from an approved version that a corrected one has not superseded."}
	}
	var made bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM nemura.payslip WHERE version_id = $1)", v.ID).Scan(&made); err != nil {
		return 0, err
	}
	if made {
		return 0, &payroll.Refusal{Code: payroll.CodeConflict, Message: "That version's payslips are published already: a correction is a new version, which has its own."}
	}
	settings, ok, err := LetterSettingsOf(ctx, tx)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The employer's name is the letterhead's, and it is not set: the HR administrator sets the company's name, UEN and address first."}
	}
	lines, err := RunLines(ctx, tx, v.ID)
	if err != nil {
		return 0, err
	}
	// One advisory lock for the numbering of a month's references.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "payslip-ref:"+org.String()+run.Month.Format("2006-01")); err != nil {
		return 0, err
	}
	var seq int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM nemura.payslip WHERE month = $1", day(run.Month)).Scan(&seq); err != nil {
		return 0, err
	}
	for _, l := range lines {
		// The date of payment is the confirmed one when the payment is already confirmed paid, and the pay group's scheduled pay day before that; Checks flags a payslip whose date differs from the
		// payment confirmed later, and the correction is the next version's payslip.
		payDate := run.PayDate
		var paidOn time.Time
		switch err := tx.QueryRow(ctx, "SELECT paid_on FROM nemura.pay_payment_event WHERE run_id = $1 AND person_id = $2 AND state = 'paid' ORDER BY seq DESC LIMIT 1", runID, l.Person).Scan(&paidOn); {
		case err == nil:
			payDate = paidOn
		case !errors.Is(err, pgx.ErrNoRows):
			return 0, err
		}
		in := payroll.PayslipInput{Employer: settings.Name, Employee: l.PersonName, Month: run.Month, PayDate: payDate, EmployeeCPF: dec(l.CPFEmployee), SHG: dec(l.SHG), Net: dec(l.Net), Reimbursements: dec(l.Reimbursements)}
		hours := apd.New(0, 0)
		for _, it := range l.Items {
			in.Items = append(in.Items, payroll.SlipLine{Key: it.Key, Name: it.Name, Class: it.Class, Amount: it.Amount, SourceType: it.SourceType, Nature: it.Nature})
			if it.SourceType == "overtime_entry" {
				var h string
				if err := tx.QueryRow(ctx, "SELECT hours::text FROM nemura.overtime_entry WHERE id = $1", it.SourceID).Scan(&h); err != nil {
					return 0, err
				}
				hours = addDec(hours, dec(h))
			}
		}
		if hours.Sign() > 0 {
			in.OvertimeHours = hours
		}
		slip, err := payroll.BuildPayslip(in)
		if err != nil {
			return 0, err
		}
		seq++
		ref := fmt.Sprintf("PS-%s-%04d", run.Month.Format("200601"), seq)
		blocks := []letters.Block{{Kind: letters.Heading, Text: "Itemised payslip"}}
		var text string
		for _, row := range slip.Lines() {
			text += row.Label + ": " + row.Value + "\n"
		}
		blocks = append(blocks, letters.Block{Kind: letters.Paragraph, Text: text}, letters.Block{Kind: letters.Paragraph, Text: "Reference " + ref})
		pdf, err := r.Render(ctx, letters.Document{Blocks: blocks, Footer: "Reference " + ref})
		if err != nil {
			var u letters.ErrUnsupported
			if errors.As(err, &u) {
				return 0, &payroll.Refusal{Code: payroll.CodeInvalid, Message: l.PersonName + ": " + err.Error()}
			}
			return 0, err
		}
		sum := sha256.Sum256(pdf)
		id := uuid.Must(uuid.NewV7())
		key := "payslips/" + org.String() + "/" + id.String() + ".pdf"
		if err := blobs.Create(ctx, key, bytes.NewReader(pdf)); err != nil {
			return 0, err
		}
		data, _ := json.Marshal(slip)
		sealed, err := sealForPerson(ctx, tx, c, org, l.Person, "payslip", data) // the itemised figures are kept sealed under the person's key, which an erasure destroys; the plain column holds a marker
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.payslip (id, version_id, person_id, month, pay_date, reference, sha256, blob_key, data, data_sealed, published_by, published_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '{"sealed": true}'::jsonb, $9, $10, $11)`,
			id, v.ID, l.Person, day(run.Month), day(payDate), ref, sum[:], key, sealed, by, at); err != nil {
			return 0, runError(err)
		}
		if err := Audit(ctx, tx, org, at, user(by), "nemura.payslip.published", audit.Object{Type: "payslip", ID: id}, map[string]audit.Change{"run": {nil, runID.String()}, "version": {nil, version}, "reference": {nil, ref}, "sha256": {nil, hex.EncodeToString(sum[:])}}, ""); err != nil {
			return 0, err
		}
		if err := notice(ctx, tx, "payslip_published", l.Person, id, "payslip-published"); err != nil {
			return 0, err
		}
	}
	return len(lines), nil
}

// PayslipRow is a payslip as listed and read.
type PayslipRow struct {
	ID, Version uuid.UUID
	Number      int
	Month       time.Time
	PayDate     time.Time
	Reference   string
	SHA256      string
	Net         string
	Superseded  bool // a corrected version of the month has payslips of its own
	Data        payroll.Payslip
	BlobKey     string
	PublishedAt time.Time
}

// OwnPayslips lists the payslips of the person the member is, newest month first and, within a month, the newest version first: a corrected payslip is shown above the one it corrects.
func OwnPayslips(ctx context.Context, tx pgx.Tx, c ports.Cipher, org, member uuid.UUID) ([]PayslipRow, error) {
	rows, err := tx.Query(ctx, `SELECT s.id, s.version_id, v.version, s.month, s.pay_date, s.reference, encode(s.sha256, 'hex'), s.data, s.data_sealed, s.person_id, s.blob_key, s.published_at,
 EXISTS (SELECT 1 FROM nemura.payslip s2 JOIN nemura.pay_run_version v2 ON v2.organisation_id = s2.organisation_id AND v2.id = s2.version_id
         WHERE s2.person_id = s.person_id AND s2.month = s.month AND v2.run_id = v.run_id AND v2.version > v.version)
FROM nemura.payslip s JOIN nemura.pay_run_version v ON v.organisation_id = s.organisation_id AND v.id = s.version_id
JOIN nemura.person p ON p.organisation_id = s.organisation_id AND p.id = s.person_id WHERE p.member_id = $1 ORDER BY s.month DESC, v.version DESC`, member)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PayslipRow
	var plain, sealed [][]byte
	var people []uuid.UUID
	for rows.Next() {
		var r PayslipRow
		var data, seal []byte
		var person uuid.UUID
		if err := rows.Scan(&r.ID, &r.Version, &r.Number, &r.Month, &r.PayDate, &r.Reference, &r.SHA256, &data, &seal, &person, &r.BlobKey, &r.PublishedAt, &r.Superseded); err != nil {
			return nil, err
		}
		out, plain, sealed, people = append(out, r), append(plain, data), append(sealed, seal), append(people, person)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		data := plain[i]
		if sealed[i] != nil { // the itemised figures are sealed under the person's key (NM-SEC-08)
			if c == nil {
				return nil, &payroll.Refusal{Code: "NM_UNAVAILABLE", Message: "This deployment has no key service: payslips cannot be read."}
			}
			var err error
			if data, err = openForPerson(ctx, tx, c, org, people[i], "payslip", sealed[i]); err != nil {
				return nil, err
			}
		}
		if err := json.Unmarshal(data, &out[i].Data); err != nil {
			return nil, err
		}
		out[i].Net = out[i].Data.Net
	}
	return out, nil
}

// ReadPayslip returns the PDF of one of the member's own payslips after checking that the stored bytes still hash to what was recorded: a changed byte is never served.
func ReadPayslip(ctx context.Context, tx pgx.Tx, blobs ports.BlobStore, member, id uuid.UUID) (string, []byte, error) {
	var key, ref string
	var sum []byte
	err := tx.QueryRow(ctx, `SELECT s.blob_key, s.reference, s.sha256 FROM nemura.payslip s JOIN nemura.person p ON p.organisation_id = s.organisation_id AND p.id = s.person_id WHERE s.id = $1 AND p.member_id = $2`, id, member).Scan(&key, &ref, &sum)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, &payroll.Refusal{Code: payroll.CodeNotFound, Message: "There is no such payslip of yours."}
	}
	if err != nil {
		return "", nil, err
	}
	rc, err := blobs.Get(ctx, key)
	if err != nil {
		return "", nil, err
	}
	pdf, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return "", nil, err
	}
	if got := sha256.Sum256(pdf); !bytes.Equal(got[:], sum) {
		return "", nil, &payroll.Refusal{Code: payroll.CodeConflict, Message: "The stored payslip no longer matches its hash: it is not served, and the change is for the security team to look into."}
	}
	return ref, pdf, nil
}
