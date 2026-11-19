package nemura

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
)

// SickAsk is a person telling the company they are sick (NM-LV-12): sick leave is notified, not applied for.
type SickAsk struct {
	Person   uuid.UUID
	Kind     string
	From, To time.Time
	Note     string
}

// SickResult is how a notification fell against the year's ceilings.
type SickResult struct {
	ID                 uuid.UUID
	Days, Paid, Unpaid leave.Days
	Late               bool
}

func sickError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM147":
			return &leave.Refusal{Code: leave.CodeConflict, Message: "You already have sick leave or other leave on some of those days: ask the people team to correct it, or choose the days again."}
		case "NM146":
			return &leave.Refusal{Code: leave.CodeConflict, Message: "A sick leave notification is evidence: only its certificate and verification are added, once."}
		case "NM148":
			return &leave.Refusal{Code: leave.CodeRole, Message: "Nobody verifies their own certificate: another member of the people team does."}
		}
	}
	return leaveError(err)
}

// topUp raises a sick ledger type's entitlement to the ceiling in force, never lowering it (the ceiling steps up with service, on the date of absence).
func topUp(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, typeKey string, year int, ceiling leave.Days) error {
	b, err := LeaveBalance(ctx, tx, person, typeKey, year)
	if err != nil {
		return err
	}
	if ceiling <= b.Entitled {
		return nil
	}
	_, err = AppendLeave(ctx, tx, org, at, by, person, typeKey, year, nil, leave.Entry{Kind: leave.Accrual, Amount: ceiling - b.Entitled, Source: fmt.Sprintf("sick-ceiling:%d", year)}, "")
	return err
}

// NotifySick records a person's sick leave and takes its paid days from the ledger at once (NM-LV-12, design D5). Each working day is taken against the ceiling in
// force on that day, the outpatient and hospitalisation ceilings being one; days beyond it are recorded as unpaid for the people team to decide on. A late notice is
// flagged and accepted. A day the person already has leave or sick leave on is refused, so nothing is subtracted twice.
func NotifySick(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, v pack.Version, a SickAsk) (SickResult, error) {
	var res SickResult
	if a.Kind != leave.Outpatient && a.Kind != leave.Hospitalisation {
		return res, &leave.Refusal{Code: leave.CodeInvalid, Message: "The kind of sick leave is outpatient or hospitalisation: choose one."}
	}
	if a.From.Year() != a.To.Year() {
		return res, &leave.Refusal{Code: leave.CodeInvalid, Message: "The sick leave runs over the new year: tell us once for each year, so each comes out of its own ceiling."}
	}
	var start time.Time
	var left *time.Time
	if err := tx.QueryRow(ctx, "SELECT employment_start, leaving_date FROM nemura.person WHERE id = $1", a.Person).Scan(&start, &left); err != nil {
		return res, err
	}
	if a.From.Before(start) || (left != nil && a.To.After(*left)) {
		return res, &leave.Refusal{Code: leave.CodeInvalid, Message: "Those dates are outside the person's employment: choose days from the first day to the last."}
	}
	p, ok, err := WorkingPatternOn(ctx, tx, a.Person, a.From)
	if err != nil {
		return res, err
	}
	if !ok {
		return res, &leave.Refusal{Code: leave.CodeInvalid, Message: "No working pattern is set on the first day: ask the people team to set it, so the days can be counted."}
	}
	ch, err := leave.Count(v, p, leave.WorkingDays, a.From, a.To, false, false)
	if err != nil {
		return res, err
	}
	year := a.From.Year()
	// The head of the outpatient type is locked before the hospitalisation type's, always in this order.
	for _, typ := range []string{leave.SickOutpatientType, leave.SickHospitalType} {
		if _, err := tx.Exec(ctx, "SELECT nemura.lock_leave_head($1, $2, $3)", a.Person, typ, year); err != nil {
			return res, leaveError(err)
		}
	}
	out, err := LeaveBalance(ctx, tx, a.Person, leave.SickOutpatientType, year)
	if err != nil {
		return res, err
	}
	hosp, err := LeaveBalance(ctx, tx, a.Person, leave.SickHospitalType, year)
	if err != nil {
		return res, err
	}
	alloc := leave.SickAllocate(v, start, a.Kind, ch.Counted, out.Taken, hosp.Taken)
	id := uuid.Must(uuid.NewV7())
	res = SickResult{ID: id, Days: ch.Days, Paid: alloc.Paid, Unpaid: alloc.Unpaid, Late: leave.LateNotice(a.From, at)}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.sick_notification (id, person_id, kind, leave_year, start_date, end_date, days, paid_days, unpaid_days, late, note, notified_by, notified_at)
VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8::numeric, $9::numeric, $10, $11, $12, $13)`, id, a.Person, a.Kind, year, day(a.From), day(a.To), ch.Days.String(), alloc.Paid.String(), alloc.Unpaid.String(),
		res.Late, orNil(a.Note), by, at); err != nil {
		return SickResult{}, sickError(err)
	}
	if err := topUp(ctx, tx, org, at, by, a.Person, leave.SickOutpatientType, year, alloc.Outpatient); err != nil {
		return SickResult{}, err
	}
	if err := topUp(ctx, tx, org, at, by, a.Person, leave.SickHospitalType, year, alloc.Hospital); err != nil {
		return SickResult{}, err
	}
	if alloc.Paid > 0 {
		types := []string{leave.SickHospitalType}
		if a.Kind == leave.Outpatient {
			types = []string{leave.SickOutpatientType, leave.SickHospitalType}
		}
		for _, typ := range types {
			if _, err := AppendLeave(ctx, tx, org, at, by, a.Person, typ, year, nil, leave.Entry{Kind: leave.Use, Amount: -alloc.Paid, Source: "sick:" + id.String()}, ""); err != nil {
				return SickResult{}, err
			}
		}
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.sick_leave.notified", audit.Object{Type: "sick_notification", ID: id}, map[string]audit.Change{"person": {nil, a.Person.String()},
		"from": {nil, day(a.From)}, "to": {nil, day(a.To)}, "days": {nil, ch.Days.String()}, "paid": {nil, alloc.Paid.String()}, "unpaid": {nil, alloc.Unpaid.String()}, "late": {nil, res.Late}}, ""); err != nil {
		return SickResult{}, err
	}
	return res, notice(ctx, tx, "sick_notified", a.Person, id, "sick-notified")
}

// AttachSickCertificate points a notification at the certificate document the person or the people team added: once (NM-LV-06). The document is a restricted
// record; this keeps only who issued it.
func AttachSickCertificate(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, notification, document uuid.UUID, issuer string) error {
	if len(issuer) == 0 || len(issuer) > 200 {
		return &leave.Refusal{Code: leave.CodeInvalid, Message: "The issuer is required and at most 200 characters: write the clinic, hospital or doctor on the certificate."}
	}
	tag, err := tx.Exec(ctx, "UPDATE nemura.sick_notification SET certificate_id = $2, issuer = $3, certificate_added_at = $4 WHERE id = $1 AND certificate_id IS NULL", notification, document, issuer, at)
	if err != nil {
		return sickError(err)
	}
	if tag.RowsAffected() != 1 {
		return &leave.Refusal{Code: leave.CodeConflict, Message: "That notification has a certificate already: it is given once."}
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.sick_leave.certificate_added", audit.Object{Type: "sick_notification", ID: notification}, map[string]audit.Change{"certificate": {nil, document.String()}}, "")
}

// VerifySick records that the people team has verified the certificate, once, and never by the person themselves (NM-LV-06, NM-LV-12).
func VerifySick(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, notification uuid.UUID) error {
	tag, err := tx.Exec(ctx, "UPDATE nemura.sick_notification SET verified_by = $2, verified_at = $3 WHERE id = $1 AND certificate_id IS NOT NULL AND verified_by IS NULL", notification, by, at)
	if err != nil {
		return sickError(err)
	}
	if tag.RowsAffected() != 1 {
		return &leave.Refusal{Code: leave.CodeConflict, Message: "There is no certificate waiting to be verified for that notification: it has none yet, or it is verified already."}
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.sick_leave.verified", audit.Object{Type: "sick_notification", ID: notification}, nil, "")
}

// SickRow is a notification as it is listed.
type SickRow struct {
	ID, Person         uuid.UUID
	PersonName, Kind   string
	From, To           time.Time
	Days, Paid, Unpaid leave.Days
	Late               bool
	Note               string
	Certificate        *uuid.UUID
	Issuer             string
	Verified           bool
}

// SickScope is whose notifications a caller reads: their own, or everyone's for the HR officer and the HR administrator (Appendix B: Medical certificates and notes).
type SickScope struct {
	Member     uuid.UUID
	All        bool
	Unverified bool // only those with a certificate waiting to be verified
}

// Sick lists notifications in a scope, newest first.
func Sick(ctx context.Context, tx pgx.Tx, s SickScope, person *uuid.UUID) ([]SickRow, error) {
	rows, err := tx.Query(ctx, `SELECT n.id, n.person_id, coalesce(nullif(p.preferred_name, ''), p.legal_name), n.kind, n.start_date, n.end_date, n.days::text, n.paid_days::text, n.unpaid_days::text,
 n.late, coalesce(n.note, ''), n.certificate_id, coalesce(n.issuer, ''), n.verified_by IS NOT NULL
FROM nemura.sick_notification n JOIN nemura.person p ON p.organisation_id = n.organisation_id AND p.id = n.person_id
WHERE ($2 OR p.member_id = $1) AND (NOT $3 OR (n.certificate_id IS NOT NULL AND n.verified_by IS NULL)) AND ($4::uuid IS NULL OR n.person_id = $4)
ORDER BY n.start_date DESC, n.id`, s.Member, s.All, s.Unverified, person)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SickRow
	for rows.Next() {
		var r SickRow
		var days, paid, unpaid string
		if err := rows.Scan(&r.ID, &r.Person, &r.PersonName, &r.Kind, &r.From, &r.To, &days, &paid, &unpaid, &r.Late, &r.Note, &r.Certificate, &r.Issuer, &r.Verified); err != nil {
			return nil, err
		}
		for i, dst := range []*leave.Days{&r.Days, &r.Paid, &r.Unpaid} {
			if *dst, err = leave.ParseDays([]string{days, paid, unpaid}[i]); err != nil {
				return nil, err
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
