package nemura

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/nemura/selfservice"
)

func selfError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM165":
			return &selfservice.Refusal{Code: selfservice.CodeConflict, Message: "That request is decided: it keeps its words and its evidence, and is decided once."}
		case "NM166":
			return &selfservice.Refusal{Code: selfservice.CodeRole, Message: "Nobody decides a change to their own details: another member of the people team does."}
		case "NM167":
			return &selfservice.Refusal{Code: selfservice.CodeConflict, Message: "That question is closed: it keeps its words and is answered or withdrawn once."}
		case "NM168":
			return &selfservice.Refusal{Code: selfservice.CodeRole, Message: "Nobody answers their own question: another member of the people team does."}
		}
	}
	return err
}

// OwnPerson is the person a login is linked to.
func OwnPerson(ctx context.Context, tx pgx.Tx, member uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	if err := tx.QueryRow(ctx, "SELECT id FROM nemura.person WHERE member_id = $1", member).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, &people.Refusal{Code: people.CodeNotFound, Message: "Your login is linked to no person record: ask the people team to link it."}
		}
		return uuid.Nil, err
	}
	return id, nil
}

// MyDetails are a person's own contact details and emergency contacts.
type MyDetails struct {
	Contact   selfservice.Contact
	Emergency []selfservice.EmergencyContact
}

// DetailsOf reads a person's own details: the phone from the people record, the rest from what the person keeps.
func DetailsOf(ctx context.Context, tx pgx.Tx, person uuid.UUID) (MyDetails, error) {
	var d MyDetails
	var phone, email, address, postal *string
	if err := tx.QueryRow(ctx, "SELECT phone FROM nemura.person WHERE id = $1", person).Scan(&phone); err != nil {
		return d, err
	}
	if err := tx.QueryRow(ctx, "SELECT personal_email, address, postal_code FROM nemura.person_contact WHERE person_id = $1", person).Scan(&email, &address, &postal); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return d, err
	}
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	d.Contact = selfservice.Contact{Phone: deref(phone), PersonalEmail: deref(email), Address: deref(address), PostalCode: deref(postal)}
	rows, err := tx.Query(ctx, "SELECT contact_name, relationship, contact_phone FROM nemura.emergency_contact WHERE person_id = $1 ORDER BY position", person)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var c selfservice.EmergencyContact
		if err := rows.Scan(&c.Name, &c.Relationship, &c.Phone); err != nil {
			return d, err
		}
		d.Emergency = append(d.Emergency, c)
	}
	return d, rows.Err()
}

// SetDetails saves a person's contact details at once and audits which ones changed: the event names the fields, never the values (NM-ESS-03).
func SetDetails(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, c selfservice.Contact) error {
	if err := selfservice.CheckContact(c); err != nil {
		return err
	}
	old, err := DetailsOf(ctx, tx, person)
	if err != nil {
		return err
	}
	changed := map[string]audit.Change{}
	mark := func(field, before, after string) {
		if strings.TrimSpace(before) != strings.TrimSpace(after) {
			changed[field] = audit.Change{nil, "changed"}
		}
	}
	mark("phone", old.Contact.Phone, c.Phone)
	mark("personal_email", old.Contact.PersonalEmail, c.PersonalEmail)
	mark("address", old.Contact.Address, c.Address)
	mark("postal_code", old.Contact.PostalCode, c.PostalCode)
	if len(changed) == 0 {
		return nil
	}
	trim := func(s string) any { return orNil(strings.TrimSpace(s)) }
	if _, ok := changed["phone"]; ok {
		if _, err := tx.Exec(ctx, "UPDATE nemura.person SET phone = $2 WHERE id = $1", person, trim(c.Phone)); err != nil {
			return err
		}
		if err := Audit(ctx, tx, org, at, user(by), "nemura.person.phone_changed", audit.Object{Type: "person", ID: person}, map[string]audit.Change{"phone": changed["phone"]}, ""); err != nil {
			return err
		}
	}
	if _, ok := changed["personal_email"]; ok || changed["address"] != (audit.Change{}) || changed["postal_code"] != (audit.Change{}) {
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.person_contact (person_id, personal_email, address, postal_code, updated_at) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (organisation_id, person_id) DO UPDATE SET personal_email = EXCLUDED.personal_email, address = EXCLUDED.address, postal_code = EXCLUDED.postal_code, updated_at = EXCLUDED.updated_at`,
			person, trim(c.PersonalEmail), trim(c.Address), trim(c.PostalCode), at); err != nil {
			return err
		}
		delete(changed, "phone")
		return Audit(ctx, tx, org, at, user(by), "nemura.person.contact_changed", audit.Object{Type: "person_contact", ID: person}, changed, "")
	}
	return nil
}

// SetEmergencyContacts replaces a person's emergency contacts with the list given, in order, and audits that they changed, never to whom (NM-ESS-03).
func SetEmergencyContacts(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, list []selfservice.EmergencyContact) error {
	if err := selfservice.CheckEmergency(list); err != nil {
		return err
	}
	old, err := DetailsOf(ctx, tx, person)
	if err != nil {
		return err
	}
	same := len(old.Emergency) == len(list)
	for i := 0; same && i < len(list); i++ {
		same = old.Emergency[i] == (selfservice.EmergencyContact{Name: strings.TrimSpace(list[i].Name), Relationship: strings.TrimSpace(list[i].Relationship), Phone: strings.TrimSpace(list[i].Phone)})
	}
	if same {
		return nil
	}
	if _, err := tx.Exec(ctx, "DELETE FROM nemura.emergency_contact WHERE person_id = $1 AND position > $2", person, len(list)); err != nil {
		return err
	}
	for i, c := range list {
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.emergency_contact (id, person_id, position, contact_name, relationship, contact_phone, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (organisation_id, person_id, position) DO UPDATE SET contact_name = EXCLUDED.contact_name, relationship = EXCLUDED.relationship, contact_phone = EXCLUDED.contact_phone, updated_at = EXCLUDED.updated_at`,
			uuid.Must(uuid.NewV7()), person, i+1, strings.TrimSpace(c.Name), strings.TrimSpace(c.Relationship), strings.TrimSpace(c.Phone), at); err != nil {
			return err
		}
	}
	// The audit event names the person's contact set, never the people on it.
	return Audit(ctx, tx, org, at, user(by), "nemura.person.emergency_contacts_changed", audit.Object{Type: "emergency_contact", ID: person},
		map[string]audit.Change{"count": {len(old.Emergency), len(list)}}, "")
}

// ChangeAsk is a request to change what needs evidence and approval, with the evidence already stored as a document of the person.
type ChangeAsk struct {
	Person   uuid.UUID
	Change   selfservice.Change
	Evidence uuid.UUID
}

// RequestChange records a request with its evidence and the day it is due (NM-ESS-03). It holds no NRIC or FIN.
func RequestChange(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, a ChangeAsk) (uuid.UUID, time.Time, error) {
	if err := selfservice.CheckChange(a.Change); err != nil {
		return uuid.Nil, time.Time{}, err
	}
	due := workingDaysAfter(time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC), selfservice.ChangeDueDays)
	id := uuid.Must(uuid.NewV7())
	var name, res any
	if a.Change.Kind == selfservice.LegalName {
		name = strings.TrimSpace(a.Change.LegalName)
	}
	if a.Change.Kind == selfservice.Residency {
		res = a.Change.Residency
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.details_change (id, person_id, kind, new_legal_name, new_residency, new_pr_start, note, evidence_id, requested_by, requested_at, due_on)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, id, a.Person, a.Change.Kind, name, res, dayPtr(a.Change.PRStart), strings.TrimSpace(a.Change.Note), a.Evidence, by, at, day(due)); err != nil {
		return uuid.Nil, time.Time{}, selfError(err)
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.details_change.requested", audit.Object{Type: "details_change", ID: id}, map[string]audit.Change{"person": {nil, a.Person.String()}, "kind": {nil, a.Change.Kind}, "due_on": {nil, day(due)}}, ""); err != nil {
		return uuid.Nil, time.Time{}, err
	}
	return id, due, notice(ctx, tx, "details_change_requested", a.Person, id, "details-change-requested")
}

// DecideChange approves or refuses a request. Another person decides it, with a reason to refuse. Approving a legal name or a residency applies it to the record from the day
// of the decision; approving an NRIC or FIN change records that the evidence was checked, and the people team then records the number through the sealed flow.
func DecideChange(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, id uuid.UUID, approve bool, reason string) (string, error) {
	var person uuid.UUID
	var kind, state string
	var name, res *string
	var pr *time.Time
	err := tx.QueryRow(ctx, "SELECT person_id, kind, state, new_legal_name, new_residency, new_pr_start FROM nemura.details_change WHERE id = $1 FOR UPDATE", id).Scan(&person, &kind, &state, &name, &res, &pr)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", &selfservice.Refusal{Code: selfservice.CodeNotFound, Message: "There is no such request."}
	}
	if err != nil {
		return "", err
	}
	if state != "requested" {
		return kind, &selfservice.Refusal{Code: selfservice.CodeConflict, Message: "That request is decided: it keeps its words and its evidence, and is decided once."}
	}
	reason = strings.TrimSpace(reason)
	if !approve && reason == "" {
		return kind, &selfservice.Refusal{Code: selfservice.CodeInvalid, Message: "Say why you refuse it, so the person can fix it and ask again."}
	}
	if len(reason) > 500 {
		return kind, &selfservice.Refusal{Code: selfservice.CodeInvalid, Message: "The reason is up to 500 characters."}
	}
	next := "approved"
	if !approve {
		next = "refused"
	}
	if _, err := tx.Exec(ctx, "UPDATE nemura.details_change SET state = $2, decided_by = $3, decided_at = $4, decision_reason = $5 WHERE id = $1", id, next, by, at, orNil(reason)); err != nil {
		return kind, selfError(err)
	}
	if approve {
		switch kind {
		case selfservice.LegalName:
			p, err := PersonOn(ctx, tx, person, at)
			if err != nil {
				return kind, err
			}
			if err := UpdatePerson(ctx, tx, org, at, by, person, people.Person{LegalName: *name, PreferredName: p.PreferredName, WorkEmail: p.WorkEmail, Phone: p.Phone, EmploymentStart: p.Start, ProbationEnd: p.ProbationEnd,
				LeavingDate: p.LeavingDate, LeavingReason: p.LeavingReason}); err != nil {
				return kind, err
			}
		case selfservice.Residency:
			c, ok, err := ClassificationOn(ctx, tx, person, at)
			if err != nil {
				return kind, err
			}
			if !ok {
				return kind, &selfservice.Refusal{Code: selfservice.CodeConflict, Message: "The person has no legal classification yet: record it first, then approve the change."}
			}
			c.Residency, c.PRStart = *res, pr
			from := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
			if err := SetClassification(ctx, tx, org, at, by, person, from, c); err != nil {
				var r *people.Refusal
				if !errors.As(err, &r) || r.Code != people.CodeHistory {
					return kind, err
				}
				if err := SetClassification(ctx, tx, org, at, by, person, from.AddDate(0, 0, 1), c); err != nil {
					return kind, err
				}
			}
		}
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.details_change."+next, audit.Object{Type: "details_change", ID: id}, map[string]audit.Change{"state": {"requested", next}, "kind": {nil, kind}}, reason); err != nil {
		return kind, err
	}
	return kind, notice(ctx, tx, "details_change_"+next, person, id, "details-change-"+next)
}

// AskHR records a question to the people team and the day it is due.
func AskHR(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, question string) (uuid.UUID, time.Time, error) {
	if err := selfservice.CheckQuestion(question); err != nil {
		return uuid.Nil, time.Time{}, err
	}
	due := workingDaysAfter(time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC), selfservice.QuestionDueDays)
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.hr_question (id, person_id, question, asked_at, due_on) VALUES ($1, $2, $3, $4, $5)", id, person, strings.TrimSpace(question), at, day(due)); err != nil {
		return uuid.Nil, time.Time{}, selfError(err)
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.question.asked", audit.Object{Type: "hr_question", ID: id}, map[string]audit.Change{"person": {nil, person.String()}, "due_on": {nil, day(due)}}, ""); err != nil {
		return uuid.Nil, time.Time{}, err
	}
	return id, due, notice(ctx, tx, "question_asked", person, id, "question-asked")
}

// AnswerHR answers a question once, in writing, as someone who is not the asker.
func AnswerHR(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, id uuid.UUID, reply string) error {
	reply = strings.TrimSpace(reply)
	if reply == "" || len(reply) > 2000 {
		return &selfservice.Refusal{Code: selfservice.CodeInvalid, Message: "Write the answer in up to 2,000 characters."}
	}
	var person uuid.UUID
	var state string
	if err := tx.QueryRow(ctx, "SELECT person_id, state FROM nemura.hr_question WHERE id = $1 FOR UPDATE", id).Scan(&person, &state); errors.Is(err, pgx.ErrNoRows) {
		return &selfservice.Refusal{Code: selfservice.CodeNotFound, Message: "There is no such question."}
	} else if err != nil {
		return err
	}
	if state != "open" {
		return &selfservice.Refusal{Code: selfservice.CodeConflict, Message: "That question is closed: it is answered or withdrawn once."}
	}
	if _, err := tx.Exec(ctx, "UPDATE nemura.hr_question SET state = 'answered', reply = $2, replied_by = $3, replied_at = $4 WHERE id = $1", id, reply, by, at); err != nil {
		return selfError(err)
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.question.answered", audit.Object{Type: "hr_question", ID: id}, map[string]audit.Change{"state": {"open", "answered"}}, ""); err != nil {
		return err
	}
	return notice(ctx, tx, "question_answered", person, id, "question-answered")
}

// WithdrawOwn withdraws a request of the person's own that nobody has decided: a change of details or a question.
func WithdrawOwn(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, "UPDATE nemura.details_change SET state = 'withdrawn', decided_at = $3 WHERE id = $1 AND person_id = $2 AND state = 'requested'", id, person, at)
	if err != nil {
		return selfError(err)
	}
	table, from := "details_change", "requested"
	if tag.RowsAffected() == 0 {
		if tag, err = tx.Exec(ctx, "UPDATE nemura.hr_question SET state = 'withdrawn' WHERE id = $1 AND person_id = $2 AND state = 'open'", id, person); err != nil {
			return selfError(err)
		}
		table, from = "hr_question", "open"
	}
	if tag.RowsAffected() == 0 {
		return &selfservice.Refusal{Code: selfservice.CodeNotFound, Message: "There is no such request of yours that is still waiting."}
	}
	return Audit(ctx, tx, org, at, user(by), "nemura."+table+".withdrawn", audit.Object{Type: table, ID: id}, map[string]audit.Change{"state": {from, "withdrawn"}}, "")
}

// RequestRow is one row of a person's requests, or of the people team's queue.
type RequestRow struct {
	selfservice.Request
	Person     uuid.UUID
	PersonName string
	Evidence   *uuid.UUID
	ChangeKind string
	Detail     string
}

// RequestsOf lists a person's requests of every kind with where each is: letters, flexible work, changes of details and questions, newest first.
func RequestsOf(ctx context.Context, tx pgx.Tx, person uuid.UUID) ([]RequestRow, error) {
	rows, err := requestRows(ctx, tx, "r.person_id = $1", person)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Asked.After(rows[j].Asked) })
	return rows, err
}

// Queue lists what waits for the people team: changes of details and questions not yet decided or answered, soonest due first.
func Queue(ctx context.Context, tx pgx.Tx) ([]RequestRow, error) {
	rows, err := requestRows(ctx, tx, "$1::uuid IS NULL", nil)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Due.Before(*rows[j].Due) })
	var out []RequestRow
	for _, r := range rows {
		if (r.Kind == selfservice.KindChange || r.Kind == selfservice.KindQuestion) && r.Step == selfservice.Received {
			out = append(out, r)
		}
	}
	return out, nil
}

func requestRows(ctx context.Context, tx pgx.Tx, where string, arg any) ([]RequestRow, error) {
	w := func(alias string) string { return strings.ReplaceAll(where, "r.", alias+".") }
	q := `SELECT * FROM (
SELECT r.id, 'letter' AS kind, r.type_key AS title, r.state, r.requested_at AS asked, r.due_on AS due, coalesce(r.decision_reason, '') AS reply, r.person_id, p.legal_name, NULL::uuid AS evidence, '' AS ckind, r.purpose AS detail
  FROM nemura.letter_request r JOIN nemura.person p ON p.organisation_id = r.organisation_id AND p.id = r.person_id WHERE ` + w("r") + `
UNION ALL
SELECT r.id, 'flexible_work', r.arrangement, r.state, r.requested_at, r.reply_due, coalesce(r.written_reply, ''), r.person_id, p.legal_name, NULL::uuid, '', r.reason
  FROM nemura.flex_request r JOIN nemura.person p ON p.organisation_id = r.organisation_id AND p.id = r.person_id WHERE ` + w("r") + `
UNION ALL
SELECT r.id, 'details_change', r.kind, r.state, r.requested_at, r.due_on, coalesce(r.decision_reason, ''), r.person_id, p.legal_name, r.evidence_id, r.kind, r.note
  FROM nemura.details_change r JOIN nemura.person p ON p.organisation_id = r.organisation_id AND p.id = r.person_id WHERE ` + w("r") + `
UNION ALL
SELECT r.id, 'question', 'question', r.state, r.asked_at, r.due_on, coalesce(r.reply, ''), r.person_id, p.legal_name, NULL::uuid, '', r.question
  FROM nemura.hr_question r JOIN nemura.person p ON p.organisation_id = r.organisation_id AND p.id = r.person_id WHERE ` + w("r") + `
) x`
	rows, err := tx.Query(ctx, q, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RequestRow
	for rows.Next() {
		var r RequestRow
		var due time.Time
		if err := rows.Scan(&r.ID, &r.Kind, &r.Title, &r.State, &r.Asked, &due, &r.Reply, &r.Person, &r.PersonName, &r.Evidence, &r.ChangeKind, &r.Detail); err != nil {
			return nil, err
		}
		r.Due = &due
		r.Step = selfservice.Step(r.Kind, r.State)
		out = append(out, r)
	}
	return out, rows.Err()
}
