package nemura

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/access"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/outbox"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Restricted people data (NM-PPL-02 to NM-PPL-05, NM-PPL-07): sealed by the cipher port, kept with a masked form, read only through an audited,
// level-checked function, and never in an audit event.

// Capabilities of Appendix B for the restricted fields.
const (
	CapNRIC = "NRIC or FIN"
	CapBank = "Salary bank account"
)

// ErrHidden is returned for a field the reader may not see at all; the reader is told only that there is no such value.
var ErrHidden = &people.Refusal{Code: people.CodeNotFound, Message: "There is no such value, or you may not see it."}

func aad(org, person uuid.UUID, field string) []byte {
	return []byte(org.String() + "/" + person.String() + "/" + field)
}

func personExists(ctx context.Context, tx pgx.Tx, person uuid.UUID) error {
	var one int
	if err := tx.QueryRow(ctx, "SELECT 1 FROM nemura.person WHERE id = $1", person).Scan(&one); errors.Is(err, pgx.ErrNoRows) {
		return &people.Refusal{Code: people.CodeNotFound, Message: "There is no such person in this organisation."}
	} else if err != nil {
		return err
	}
	return nil
}

// auditRead records that a restricted field was read, at what level, or that the read was refused: never the value (NM-PPL-02, AUD-05).
func auditRead(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, actor, person uuid.UUID, capability string, level access.Level) error {
	action := "nemura.restricted.read"
	if level == access.Hidden {
		action = "nemura.restricted.refused"
	}
	return Audit(ctx, tx, org, at, user(actor), action, audit.Object{Type: "person", ID: person},
		map[string]audit.Change{"capability": {nil, capability}, "level": {nil, level.String()}}, "")
}

// SetNRIC seals a person's NRIC or FIN with its masked form and keyed fingerprint (NM-PPL-02). The event says the field changed, never its value.
func SetNRIC(ctx context.Context, tx pgx.Tx, c ports.Cipher, org uuid.UUID, at time.Time, by, person uuid.UUID, value string) error {
	v, err := people.NormaliseNRIC(value)
	if err != nil {
		return err
	}
	if err := personExists(ctx, tx, person); err != nil {
		return err
	}
	sealed, err := c.Seal(ctx, []byte(v), aad(org, person, "nric"))
	if err != nil {
		return err
	}
	fp, err := c.Fingerprint(ctx, []byte(org.String()), []byte(v))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.person_secret (person_id, nric_sealed, nric_masked, nric_fingerprint) VALUES ($1, $2, $3, $4)
ON CONFLICT (organisation_id, person_id) DO UPDATE SET nric_sealed = EXCLUDED.nric_sealed, nric_masked = EXCLUDED.nric_masked, nric_fingerprint = EXCLUDED.nric_fingerprint, updated_at = $5`,
		person, sealed, people.MaskNRIC(v), fp, at); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.person.nric_set", audit.Object{Type: "person_secret", ID: person}, map[string]audit.Change{"field": {nil, "nric"}}, "")
}

// ReadSecret reads a person's NRIC or bank account at the level the reader's roles give (a person sees their own in full), auditing the read or the
// refusal. The value of a hidden field is never returned (NM-PPL-02, NM-SEC-02).
func ReadSecret(ctx context.Context, tx pgx.Tx, c ports.Cipher, org uuid.UUID, at time.Time, actor uuid.UUID, held []authz.Role, person uuid.UUID, field string) (string, access.Level, error) {
	capability, sealedCol, maskedCol := CapNRIC, "nric_sealed", "nric_masked"
	if field == "bank" {
		capability, sealedCol, maskedCol = CapBank, "bank_sealed", "bank_masked"
	}
	var own bool
	if err := tx.QueryRow(ctx, "SELECT member_id IS NOT DISTINCT FROM $2 AND member_id IS NOT NULL FROM nemura.person WHERE id = $1", person, actor).Scan(&own); errors.Is(err, pgx.ErrNoRows) {
		return "", access.Hidden, ErrHidden
	} else if err != nil {
		return "", access.Hidden, err
	}
	level := access.Of(held, capability, own)
	if err := auditRead(ctx, tx, org, at, actor, person, capability, level); err != nil {
		return "", level, err
	}
	if level == access.Hidden {
		return "", level, ErrHidden
	}
	var sealed []byte
	var masked *string
	err := tx.QueryRow(ctx, "SELECT "+sealedCol+", "+maskedCol+" FROM nemura.person_secret WHERE person_id = $1", person).Scan(&sealed, &masked)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && masked == nil) {
		return "", level, ErrHidden
	}
	if err != nil {
		return "", level, err
	}
	if level == access.Masked {
		return *masked, level, nil
	}
	plain, err := c.Open(ctx, sealed, aad(org, person, field))
	return string(plain), level, err
}

// AddWorkPass records a work pass with its number sealed and masked, its dates and the reminders it will need (NM-PPL-03). A pass is a record: a renewal
// is a new pass.
func AddWorkPass(ctx context.Context, tx pgx.Tx, c ports.Cipher, org uuid.UUID, at time.Time, by, person uuid.UUID, kind, number string, issued, expires time.Time) (uuid.UUID, error) {
	n, err := people.NormalisePass(kind, number, issued, expires)
	if err != nil {
		return uuid.Nil, err
	}
	if err := personExists(ctx, tx, person); err != nil {
		return uuid.Nil, err
	}
	id := uuid.Must(uuid.NewV7())
	sealed, err := c.Seal(ctx, []byte(n), aad(org, id, "pass"))
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.work_pass (id, person_id, pass_type, number_sealed, number_masked, issued_on, expires_on) VALUES ($1, $2, $3, $4, $5, $6, $7)",
		id, person, kind, sealed, people.MaskPass(n), day(issued), day(expires)); err != nil {
		return uuid.Nil, err
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.person.work_pass_added", audit.Object{Type: "work_pass", ID: id},
		map[string]audit.Change{"person": {nil, person.String()}, "pass_type": {nil, kind}, "expires_on": {nil, day(expires)}}, "")
}

// PassRow is a work pass as it is shown: the number masked.
type PassRow struct {
	ID              uuid.UUID
	Type, Number    string
	Issued, Expires time.Time
}

// WorkPasses lists a person's passes, soonest to expire first.
func WorkPasses(ctx context.Context, tx pgx.Tx, person uuid.UUID) ([]PassRow, error) {
	rows, err := tx.Query(ctx, "SELECT id, pass_type, number_masked, issued_on, expires_on FROM nemura.work_pass WHERE person_id = $1 ORDER BY expires_on, id", person)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PassRow
	for rows.Next() {
		var r PassRow
		if err := rows.Scan(&r.ID, &r.Type, &r.Number, &r.Issued, &r.Expires); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PassesDueForReminder are the passes that expire in exactly 90, 60 or 30 days from a day and have no later pass of the same person and type, so a renewed
// pass stops the reminders (NM-PPL-03). Delivery is the notifier's, with notifications.
func PassesDueForReminder(ctx context.Context, tx pgx.Tx, on time.Time) ([]PassRow, error) {
	rows, err := tx.Query(ctx, `SELECT p.id, p.pass_type, p.number_masked, p.issued_on, p.expires_on FROM nemura.work_pass p
WHERE (p.expires_on - $1::date) IN (90, 60, 30)
  AND NOT EXISTS (SELECT 1 FROM nemura.work_pass n WHERE n.person_id = p.person_id AND n.pass_type = p.pass_type AND n.expires_on > p.expires_on)
ORDER BY p.expires_on, p.id`, day(on))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PassRow
	for rows.Next() {
		var r PassRow
		if err := rows.Scan(&r.ID, &r.Type, &r.Number, &r.Issued, &r.Expires); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddDependant records a dependant for family leave, restricted like other personal data (NM-PPL-07).
func AddDependant(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, d people.Dependant, from time.Time) (uuid.UUID, error) {
	if err := d.Check(); err != nil {
		return uuid.Nil, err
	}
	if err := personExists(ctx, tx, person); err != nil {
		return uuid.Nil, err
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.person_dependant (id, person_id, relationship, birth_date, adoption_intent, citizenship, declared_on, effective_from)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, id, person, d.Relationship, dayPtr(d.BirthDate), d.AdoptionIntent, d.Citizenship, day(d.DeclaredOn), day(from)); err != nil {
		return uuid.Nil, err
	}
	// The child's date of birth never enters the event.
	return id, Audit(ctx, tx, org, at, user(by), "nemura.person.dependant_added", audit.Object{Type: "person_dependant", ID: id},
		map[string]audit.Change{"person": {nil, person.String()}, "relationship": {nil, d.Relationship}, "citizenship": {nil, d.Citizenship}}, "")
}

// DependantRow is a dependant as the people team sees them.
type DependantRow struct {
	ID                        uuid.UUID
	Relationship, Citizenship string
	BirthDate                 *time.Time
	AdoptionIntent            bool
	From                      time.Time
	To                        *time.Time
}

// Dependants lists a person's dependants, reading them audited as a restricted field (NM-PPL-07).
func Dependants(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, actor uuid.UUID, held []authz.Role, person uuid.UUID) ([]DependantRow, error) {
	var own bool
	if err := tx.QueryRow(ctx, "SELECT member_id IS NOT DISTINCT FROM $2 AND member_id IS NOT NULL FROM nemura.person WHERE id = $1", person, actor).Scan(&own); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrHidden
	} else if err != nil {
		return nil, err
	}
	level := access.Of(held, "Dependants (NM-PPL-07)", own)
	if err := auditRead(ctx, tx, org, at, actor, person, "Dependants (NM-PPL-07)", level); err != nil {
		return nil, err
	}
	if level == access.Hidden {
		return nil, ErrHidden
	}
	rows, err := tx.Query(ctx, "SELECT id, relationship, citizenship, birth_date, adoption_intent, effective_from, effective_to FROM nemura.person_dependant WHERE person_id = $1 ORDER BY effective_from, id", person)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DependantRow
	for rows.Next() {
		var r DependantRow
		if err := rows.Scan(&r.ID, &r.Relationship, &r.Citizenship, &r.BirthDate, &r.AdoptionIntent, &r.From, &r.To); err != nil {
			return nil, err
		}
		if level == access.Masked {
			r.BirthDate = nil
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DocumentInput is a document's metadata; its content is in the blob store under BlobKey.
type DocumentInput struct {
	Type, Title, RetentionClass, BlobKey, ContentType string
	Size                                              int64
	SHA256                                            [32]byte
	Viewers                                           []string
}

// AddDocument records a document of a person: write-once evidence with a retention class and the roles who may see it (NM-PPL-05).
func AddDocument(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person, id uuid.UUID, d DocumentInput) error {
	if err := personExists(ctx, tx, person); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.person_document (id, person_id, doc_type, title, retention_class, blob_key, content_type, size_bytes, sha256, viewers, uploaded_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, id, person, d.Type, d.Title, d.RetentionClass, d.BlobKey, d.ContentType, d.Size, d.SHA256[:], d.Viewers, by); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.person.document_added", audit.Object{Type: "person_document", ID: id},
		map[string]audit.Change{"person": {nil, person.String()}, "doc_type": {nil, d.Type}, "title": {nil, d.Title}, "retention_class": {nil, d.RetentionClass}}, "")
}

// DocumentRow is a document's record.
type DocumentRow struct {
	ID                                       uuid.UUID
	Type, Title, RetentionClass, ContentType string
	BlobKey                                  string
	Size                                     int64
	SHA256                                   []byte
	Viewers                                  []string
	Created                                  time.Time
}

// Documents lists the person's documents that someone with these roles may see: a role is a viewer when the document names it.
func Documents(ctx context.Context, tx pgx.Tx, person uuid.UUID, held []authz.Role, ownRecord bool) ([]DocumentRow, error) {
	// "employee" names a person's own record (ownRecord), never every employee's.
	roles := make([]string, 0, len(held))
	for _, r := range held {
		if r != authz.Employee {
			roles = append(roles, string(r))
		}
	}
	rows, err := tx.Query(ctx, `SELECT id, doc_type, title, retention_class, content_type, blob_key, size_bytes, sha256, viewers, created_at FROM nemura.person_document
WHERE person_id = $1 AND (viewers && $2 OR ($3 AND 'employee' = ANY(viewers))) ORDER BY created_at, id`, person, roles, ownRecord)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DocumentRow
	for rows.Next() {
		var r DocumentRow
		if err := rows.Scan(&r.ID, &r.Type, &r.Title, &r.RetentionClass, &r.ContentType, &r.BlobKey, &r.Size, &r.SHA256, &r.Viewers, &r.Created); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// BankDigest is what an approval of a bank account change approves: the person and the keyed fingerprint of the new account, never the account.
func BankDigest(person uuid.UUID, fingerprint []byte) []byte {
	return BankDigestWith(person, fingerprint, "")
}

// BankDigestWith is BankDigest with the bank's code bound in: an approver signs the account and the bank it is at, so the BIC Fyniq derives from the code is what was approved (design D28). With no code it is
// exactly BankDigest's, so a change proposed before the code existed keeps its digest.
func BankDigestWith(person uuid.UUID, fingerprint []byte, bankCode string) []byte {
	h := sha256.New()
	h.Write([]byte("bank_account_change:" + person.String() + ":"))
	h.Write(fingerprint)
	if bankCode != "" {
		h.Write([]byte(":bank_code:" + bankCode))
	}
	return h.Sum(nil)
}

// BankDetailDigest is the digest of a bank-detail record a payment line carries (payroll.payments.ready): the confirmed change's ID, the keyed fingerprint of the account and the bank's code, as lower-case hex.
// It names the record without the account, so Fyniq can tell a changed record from the one it approved a line for.
func BankDetailDigest(change uuid.UUID, fingerprint []byte, bankCode string) string {
	sum := sha256.Sum256(append([]byte("bank_detail:"+change.String()+":"+bankCode+":"), fingerprint...))
	return hex.EncodeToString(sum[:])
}

// ProposeBankChange is an employee proposing a new salary bank account for themselves (NM-PPL-04). It takes effect only when someone else approves it and
// the employee confirms it with a passkey; the approval is a request of the approvals primitive.
func ProposeBankChange(ctx context.Context, tx pgx.Tx, c ports.Cipher, org uuid.UUID, at time.Time, proposer, person uuid.UUID, account string) (uuid.UUID, error) {
	return ProposeBankChangeAt(ctx, tx, c, org, at, proposer, person, account, "", "")
}

// ProposeBankChangeAt is ProposeBankChange with the bank's Singapore code and name beside the account (design D26, D28). Both are optional together: with none, the account is on record and no payment line is
// sent for it until a change with a bank is confirmed.
func ProposeBankChangeAt(ctx context.Context, tx pgx.Tx, c ports.Cipher, org uuid.UUID, at time.Time, proposer, person uuid.UUID, account, bankCode, bankName string) (uuid.UUID, error) {
	v, err := people.NormaliseBank(account)
	if err != nil {
		return uuid.Nil, err
	}
	bankCode, bankName = strings.TrimSpace(bankCode), strings.TrimSpace(bankName)
	if (bankCode == "") != (bankName == "") || (bankCode != "" && !people.BankCodeShape.MatchString(bankCode)) || len(bankName) > 140 {
		return uuid.Nil, &people.Refusal{Code: people.CodeInvalid, Message: "The bank is its Singapore code (4 digits, or 4 and a 3-digit branch) and its name, together: give both or neither."}
	}
	var member *uuid.UUID
	if err := tx.QueryRow(ctx, "SELECT member_id FROM nemura.person WHERE id = $1", person).Scan(&member); errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrHidden
	} else if err != nil {
		return uuid.Nil, err
	}
	if member == nil || *member != proposer {
		return uuid.Nil, &people.Refusal{Code: people.CodeInvalid, Message: "An employee proposes a change to their own salary bank account only."}
	}
	id, approval := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	sealed, err := c.Seal(ctx, []byte(v), aad(org, person, "bank"))
	if err != nil {
		return uuid.Nil, err
	}
	fp, err := c.Fingerprint(ctx, []byte(org.String()), []byte(v))
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.approval_request (id, object_type, object_id, digest, requested_by, required_role, expires_at)
VALUES ($1, 'bank_account_change', $2, $3, $4, 'hr_admin', $5)`, approval, id, BankDigestWith(person, fp, bankCode), proposer, at.Add(7*24*time.Hour)); err != nil {
		return uuid.Nil, err
	}
	if err := Audit(ctx, tx, org, at, user(proposer), "nemura.approval.requested", audit.Object{Type: "approval_request", ID: approval},
		map[string]audit.Change{"object_type": {nil, "bank_account_change"}}, ""); err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.bank_account_change (id, person_id, new_sealed, new_masked, new_fingerprint, proposed_by, approval_id, new_bank_code, new_bank_name) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)",
		id, person, sealed, people.MaskBank(v), fp, proposer, approval, nullable(bankCode), nullable(bankName)); err != nil {
		return uuid.Nil, err
	}
	return id, Audit(ctx, tx, org, at, user(proposer), "nemura.bank_account.change_proposed", audit.Object{Type: "bank_account_change", ID: id},
		map[string]audit.Change{"person": {nil, person.String()}}, "")
}

// DecideBankChange approves or refuses a proposal. The approver holds the HR administrator, Payroll administrator or Owner role, is not the proposer and
// is not the person whose account it is (NM-ROLE-05, NM-PPL-04).
func DecideBankChange(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, approver uuid.UUID, held []authz.Role, change uuid.UUID, approve bool, reason string) error {
	var person, proposer, approval uuid.UUID
	var subject *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT c.person_id, c.proposed_by, c.approval_id, p.member_id FROM nemura.bank_account_change c JOIN nemura.person p ON p.organisation_id = c.organisation_id AND p.id = c.person_id
WHERE c.id = $1 AND c.state = 'proposed'`, change).Scan(&person, &proposer, &approval, &subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return &people.Refusal{Code: people.CodeNotFound, Message: "There is no open bank account change with that ID."}
	} else if err != nil {
		return err
	}
	may := false
	for _, r := range held {
		may = may || r == authz.HRAdmin || r == authz.PayrollAdmin || r == authz.Owner
	}
	if !may || approver == proposer || (subject != nil && *subject == approver) {
		return &people.Refusal{Code: people.CodeInvalid, Message: "A bank account change is decided by the HR administrator, the Payroll administrator or the Owner, and never by the person whose account it is."}
	}
	state, action := "refused", "nemura.approval.refused"
	if approve {
		state, action = "approved", "nemura.approval.approved"
	}
	if !approve && len(reason) == 0 {
		return &people.Refusal{Code: people.CodeInvalid, Message: "A refusal says why: give a reason."}
	}
	if _, err := tx.Exec(ctx, "UPDATE nemura.approval_request SET state = $2, decided_by = $3, decided_at = $4, reason = $5 WHERE id = $1", approval, state, approver, at, nullable(reason)); err != nil {
		return err
	}
	if err := Audit(ctx, tx, org, at, user(approver), action, audit.Object{Type: "approval_request", ID: approval}, map[string]audit.Change{"state": {"pending", state}}, reason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE nemura.bank_account_change SET state = $2 WHERE id = $1", change, state); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(approver), "nemura.bank_account.change_"+state, audit.Object{Type: "bank_account_change", ID: change}, nil, reason)
}

// ConfirmBankChange is the employee confirming an approved change with a passkey (the caller checks the step-up is fresh): the account takes effect, and a
// notice for the payroll approvers is queued in the same transaction (NM-PPL-04).
func ConfirmBankChange(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, change uuid.UUID, freshPasskey bool) error {
	if !freshPasskey {
		return &people.Refusal{Code: people.CodeInvalid, Message: "Confirm the change with your passkey first."}
	}
	var person uuid.UUID
	var member *uuid.UUID
	var sl []byte
	var mk string
	var fpb []byte
	var code, name *string
	err := tx.QueryRow(ctx, `SELECT c.person_id, p.member_id, c.new_sealed, c.new_masked, c.new_fingerprint, c.new_bank_code, c.new_bank_name FROM nemura.bank_account_change c
JOIN nemura.person p ON p.organisation_id = c.organisation_id AND p.id = c.person_id WHERE c.id = $1 AND c.state = 'approved'`, change).Scan(&person, &member, &sl, &mk, &fpb, &code, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return &people.Refusal{Code: people.CodeNotFound, Message: "There is no approved bank account change with that ID."}
	} else if err != nil {
		return err
	}
	if member == nil || *member != by {
		return &people.Refusal{Code: people.CodeInvalid, Message: "Only the person whose account it is confirms the change."}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.person_secret (person_id, bank_sealed, bank_masked, bank_fingerprint, bank_code, bank_name) VALUES ($1, $2, $3, $4, $6, $7)
ON CONFLICT (organisation_id, person_id) DO UPDATE SET bank_sealed = EXCLUDED.bank_sealed, bank_masked = EXCLUDED.bank_masked, bank_fingerprint = EXCLUDED.bank_fingerprint, bank_code = EXCLUDED.bank_code, bank_name = EXCLUDED.bank_name, updated_at = $5`, person, sl, mk, fpb, at, code, name); err != nil {
		return err
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.person.bank_account_set", audit.Object{Type: "person_secret", ID: person}, map[string]audit.Change{"field": {nil, "bank"}}, ""); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE nemura.bank_account_change SET state = 'confirmed', confirmed_at = $2 WHERE id = $1", change, at); err != nil {
		return err
	}
	if _, err := Enqueue(ctx, tx, outbox.NewItem{Class: outbox.ClassWorker, Kind: "people.notice", BusinessKey: "bank:" + change.String(),
		Payload: []byte(`{"notice":"bank_account_changed","person":"` + person.String() + `","change":"` + change.String() + `"}`)}); err != nil {
		return err
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.bank_account.change_confirmed", audit.Object{Type: "bank_account_change", ID: change}, nil, ""); err != nil {
		return err
	}
	// A linked organisation tells Fyniq the new version of the details, never the account, in the same transaction (NM-INT-09).
	if _, err = QueueBankEvent(ctx, tx, org, at, person); err != nil {
		return err
	}
	// and the payment lines it sent for this person, which Fyniq blocks until the new bank detail's line arrives (FR-PPL-14).
	_, err = requeuePaymentsFor(ctx, tx, org, at, person)
	return err
}

// BankChangeView is a change as it is shown: the account masked.
type BankChangeView struct {
	ID, Person    uuid.UUID
	Masked, State string
}

// BankChange reads one change (masked).
func BankChange(ctx context.Context, tx pgx.Tx, change uuid.UUID) (BankChangeView, error) {
	var v BankChangeView
	err := tx.QueryRow(ctx, "SELECT id, person_id, new_masked, state FROM nemura.bank_account_change WHERE id = $1", change).Scan(&v.ID, &v.Person, &v.Masked, &v.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, &people.Refusal{Code: people.CodeNotFound, Message: "There is no such bank account change."}
	}
	return v, err
}

// IR8AParticulars are what the IR8A asks of a person besides what Nemura holds elsewhere (IRAS data items B6 to B11): kept sealed with the NRIC, for the statutory filing only.
type IR8AParticulars struct {
	Sex         string `json:"sex"`                    // M or F
	AddressType string `json:"address_type,omitempty"` // L (local), F (foreign) or empty
	Address1    string `json:"address_1,omitempty"`
	Address2    string `json:"address_2,omitempty"`
	Address3    string `json:"address_3,omitempty"`
	PostalCode  string `json:"postal_code,omitempty"`
	Country     string `json:"country,omitempty"`
	Designation string `json:"designation,omitempty"`
}

// SetIR8AParticulars seals a person's IR8A particulars with the cipher under a context of their own (NM-PAY-14). The event says the field changed, never its value. The data items' own
// rules (an address needs its type, a local one a post code) are checked when the file is made, with IRAS's names for them.
func SetIR8AParticulars(ctx context.Context, tx pgx.Tx, c ports.Cipher, org uuid.UUID, at time.Time, by, person uuid.UUID, p IR8AParticulars) error {
	if p.Sex != "M" && p.Sex != "F" {
		return &people.Refusal{Code: people.CodeInvalid, Message: "sex: say M or F, as the IR8A takes it."}
	}
	if p.AddressType != "" && p.AddressType != "L" && p.AddressType != "F" {
		return &people.Refusal{Code: people.CodeInvalid, Message: "address_type: say L for a Singapore address or F for a foreign one, or leave it out when there is no address."}
	}
	if err := personExists(ctx, tx, person); err != nil {
		return err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	sealed, err := c.Seal(ctx, raw, aad(org, person, "ir8a"))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.person_secret (person_id, ir8a_sealed) VALUES ($1, $2)
ON CONFLICT (organisation_id, person_id) DO UPDATE SET ir8a_sealed = EXCLUDED.ir8a_sealed, updated_at = $3`, person, sealed, at); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.person.ir8a_set", audit.Object{Type: "person_secret", ID: person}, map[string]audit.Change{"field": {nil, "ir8a"}}, "")
}

// ir8aOf opens a person's IR8A particulars for a filing, auditing the read like any read of a restricted field; ok is false when none are on record.
func ir8aOf(ctx context.Context, tx pgx.Tx, c ports.Cipher, org uuid.UUID, at time.Time, by, person uuid.UUID) (IR8AParticulars, bool, error) {
	var p IR8AParticulars
	var sealed []byte
	err := tx.QueryRow(ctx, "SELECT ir8a_sealed FROM nemura.person_secret WHERE person_id = $1 AND ir8a_sealed IS NOT NULL", person).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, false, nil
	}
	if err != nil {
		return p, false, err
	}
	if err := auditRead(ctx, tx, org, at, by, person, CapNRIC, access.Full); err != nil {
		return p, false, err
	}
	plain, err := c.Open(ctx, sealed, aad(org, person, "ir8a"))
	if err != nil {
		return p, false, err
	}
	return p, true, json.Unmarshal(plain, &p)
}
