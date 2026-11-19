//go:build local

package nemura_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/nemura/access"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// The NRIC or FIN is sealed by the cipher and kept with its masked form: the database holds no plain value, the audit trail never carries it, a
// reader sees it at the level their roles give (the HR officer masked, the HR administrator, Payroll and the Owner in full, a person their own, a line
// manager and an employee nothing of anyone else's), and every read or refusal is audited (NM-PPL-02, NM-SEC-02).
// req: NM-PPL-02
// asvs: V8.2.1, V8.3.2, V14.1.1
func TestAnNRICIsSealedMaskedAndReadAtTheLevelOfTheReadersRoles(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	now := today.Add(9 * time.Hour)
	seed := bytes.Repeat([]byte{7}, 64)
	cipher, err := local.NewFieldKeys(seed)
	if err != nil {
		t.Fatal(err)
	}
	var ana uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		ana, err = nemura.AddPerson(ctx, tx, c.org, now, c.owner, people.Person{LegalName: "Ana Tan", EmploymentStart: d("2099-01-05"), MemberID: &c.pay},
			people.Job{Title: "Analyst", Worksite: "SG", EmploymentType: "full_time"}, nil)
		if err != nil {
			return err
		}
		if err := nemura.SetNRIC(ctx, tx, cipher, c.org, now, c.hr, ana, "s1234567d"); err != nil {
			return err
		}
		return nil
	})
	for name, bad := range map[string]string{"too short": "S123", "no letter": "12345678", "bad lead": "X1234567D"} {
		if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.SetNRIC(ctx, tx, cipher, c.org, now, c.hr, ana, bad)
		}); personRefusal(err) != people.CodeInvalid {
			t.Errorf("%s: %v", name, err)
		}
	}
	super := connect(t, db.SuperURL())
	var plainRows, masked int
	if err := super.QueryRow(ctx, "SELECT count(*) FROM nemura.person_secret WHERE nric_sealed::text LIKE '%1234567%'").Scan(&plainRows); err != nil || plainRows != 0 {
		t.Errorf("the plain NRIC is in the database: %d, %v", plainRows, err)
	}
	if err := super.QueryRow(ctx, "SELECT count(*) FROM nemura.person_secret WHERE nric_masked = 'S****567D'").Scan(&masked); err != nil || masked != 1 {
		t.Errorf("the masked form: %d, %v", masked, err)
	}
	read := func(who uuid.UUID, held ...authz.Role) (string, access.Level, error) {
		var got string
		var level access.Level
		err := c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			got, level, err = nemura.ReadSecret(ctx, tx, cipher, c.org, now, who, held, ana, "nric")
			if err != nil && errors.Is(err, nemura.ErrHidden) {
				return nil // the refusal's audit event is kept, as the API does
			}
			return err
		})
		if err == nil && level == access.Hidden {
			err = nemura.ErrHidden
		}
		return got, level, err
	}
	for _, tc := range []struct {
		name  string
		who   uuid.UUID
		roles []authz.Role
		want  string
		level access.Level
	}{
		{"the HR officer", c.hr, []authz.Role{authz.HROfficer}, "S****567D", access.Masked},
		{"the HR administrator", c.hr, []authz.Role{authz.HRAdmin}, "S1234567D", access.Full},
		{"the Owner", c.owner, []authz.Role{authz.Owner}, "S1234567D", access.Full},
		{"the person themselves", c.pay, []authz.Role{authz.Employee}, "S1234567D", access.Full},
	} {
		got, level, err := read(tc.who, tc.roles...)
		if err != nil || got != tc.want || level != tc.level {
			t.Errorf("%s: %q %v %v", tc.name, got, level, err)
		}
	}
	for name, roles := range map[string][]authz.Role{"a line manager": {authz.LineManager}, "an employee": {authz.Employee}, "the AI": {authz.AI}} {
		who := c.hr
		if got, _, err := read(who, roles...); !errors.Is(err, nemura.ErrHidden) || got != "" {
			t.Errorf("%s read %q: %v", name, got, err)
		}
	}
	var reads, refused, leaked int
	if err := super.QueryRow(ctx, "SELECT count(*) FILTER (WHERE action = 'nemura.restricted.read'), count(*) FILTER (WHERE action = 'nemura.restricted.refused'), count(*) FILTER (WHERE body LIKE '%1234567%') FROM nemura.audit_event WHERE organisation_id = $1", c.org).Scan(&reads, &refused, &leaked); err != nil {
		t.Fatal(err)
	}
	if reads != 4 || refused != 3 || leaked != 0 {
		t.Errorf("reads %d, refusals %d, leaks %d; want 4, 3, 0", reads, refused, leaked)
	}
	// A sealed value copied to another person does not open there (it is bound to its record).
	other := uuid.New()
	var sealed []byte
	if err := super.QueryRow(ctx, "SELECT nric_sealed FROM nemura.person_secret WHERE person_id = $1", ana).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if _, err := cipher.Open(ctx, sealed, []byte(c.org.String()+"/"+other.String()+"/nric")); err == nil {
		t.Error("a sealed NRIC opened for another record")
	}
}

// A work pass is sealed and masked and its reminders fall at 90, 60 and 30 days; a renewal stops the old pass's reminders. A dependant needs a date of
// birth or the intent to adopt and its read is audited; documents are append-only evidence seen only by the roles they name (NM-PPL-03, NM-PPL-05,
// NM-PPL-07).
// req: NM-PPL-03, NM-PPL-07
// asvs: V8.2.1, V14.1.1
func TestPassesDependantsAndDocumentsAreRestrictedRecords(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	now := today.Add(9 * time.Hour)
	cipher, _ := local.NewFieldKeys(bytes.Repeat([]byte{9}, 64))
	var ana uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		ana, err = nemura.AddPerson(ctx, tx, c.org, now, c.owner, people.Person{LegalName: "Ana Tan", EmploymentStart: d("2099-01-05"), MemberID: &c.pay}, people.Job{Title: "Analyst", Worksite: "SG", EmploymentType: "full_time"}, nil)
		return err
	})
	var first uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		first, err = nemura.AddWorkPass(ctx, tx, cipher, c.org, now, c.hr, ana, "employment_pass", "ep12345678", d("2099-01-01"), d("2099-12-31"))
		return err
	})
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.AddWorkPass(ctx, tx, cipher, c.org, now, c.hr, ana, "visa", "ep12345678", d("2099-01-01"), d("2099-12-31"))
		return err
	}); personRefusal(err) != people.CodeInvalid {
		t.Errorf("an unknown pass type: %v", err)
	}
	var due []nemura.PassRow
	for _, tc := range []struct {
		on   string
		want int
	}{{"2099-10-02", 1}, {"2099-11-01", 1}, {"2099-12-01", 1}, {"2099-10-03", 0}} {
		c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			due, err = nemura.PassesDueForReminder(ctx, tx, d(tc.on))
			return err
		})
		if len(due) != tc.want {
			t.Errorf("reminders on %s: %d, want %d", tc.on, len(due), tc.want)
		}
	}
	// A renewal stops the old pass's reminders.
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.AddWorkPass(ctx, tx, cipher, c.org, now, c.hr, ana, "employment_pass", "EP87654321", d("2099-12-31"), d("2101-12-31"))
		return err
	})
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		due, _ = nemura.PassesDueForReminder(ctx, tx, d("2099-11-01"))
		list, _ := nemura.WorkPasses(ctx, tx, ana)
		if len(due) != 0 || len(list) != 2 || !strings.Contains(list[0].Number, "*") || strings.Contains(list[0].Number, "12345678") {
			t.Errorf("after a renewal: %d reminders, passes %+v", len(due), list)
		}
		return nil
	})
	super := connect(t, db.SuperURL())
	for _, sql := range []string{"UPDATE nemura.work_pass SET number_masked = '***'", "DELETE FROM nemura.work_pass"} {
		if _, err := super.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "NM127") {
			t.Errorf("%s: %v", sql, err)
		}
	}
	_ = first

	// Dependants: a date of birth or the intent to adopt; reading them is audited and the people team sees them, a line manager does not.
	bd := d("2098-05-06")
	for name, dep := range map[string]people.Dependant{"neither": {Relationship: "child", Citizenship: "citizen", DeclaredOn: d("2099-01-02")}, "a bad relation": {Relationship: "pet", Citizenship: "citizen", BirthDate: &bd, DeclaredOn: d("2099-01-02")}} {
		if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
			_, err := nemura.AddDependant(ctx, tx, c.org, now, c.hr, ana, dep, d("2099-01-02"))
			return err
		}); personRefusal(err) != people.CodeInvalid {
			t.Errorf("%s: %v", name, err)
		}
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.AddDependant(ctx, tx, c.org, now, c.hr, ana, people.Dependant{Relationship: "child", BirthDate: &bd, Citizenship: "citizen", DeclaredOn: d("2099-01-02")}, d("2099-01-02"))
		return err
	})
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		list, err := nemura.Dependants(ctx, tx, c.org, now, c.hr, []authz.Role{authz.HROfficer}, ana)
		if err != nil || len(list) != 1 || list[0].BirthDate == nil {
			t.Errorf("the HR officer's view: %+v %v", list, err)
		}
		return nil
	})
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		if _, err := nemura.Dependants(ctx, tx, c.org, now, c.hr, []authz.Role{authz.LineManager}, ana); !errors.Is(err, nemura.ErrHidden) {
			t.Errorf("a line manager reading dependants: %v", err)
		}
		return nil
	})
	var leaked int
	if err := super.QueryRow(ctx, "SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND (body LIKE '%2098-05-06%' OR body LIKE '%12345678%')", c.org).Scan(&leaked); err != nil || leaked != 0 {
		t.Errorf("a date of birth or a pass number in the trail: %d, %v", leaked, err)
	}

	// Documents: append-only, seen by the roles they name (an employee sees their own only when 'employee' is named).
	doc := uuid.New()
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.AddDocument(ctx, tx, c.org, now, c.hr, ana, doc, nemura.DocumentInput{Type: "contract", Title: "Employment contract", RetentionClass: "employment-records", BlobKey: "k/1", ContentType: "application/pdf", Size: 10, SHA256: sha256.Sum256([]byte("x")), Viewers: []string{"hr_admin", "employee"}})
	})
	for name, tc := range map[string]struct {
		roles []authz.Role
		own   bool
		want  int
	}{"HR admin": {[]authz.Role{authz.HRAdmin}, false, 1}, "the person": {[]authz.Role{authz.Employee}, true, 1}, "another employee": {[]authz.Role{authz.Employee}, false, 0}, "payroll": {[]authz.Role{authz.PayrollAdmin}, false, 0}} {
		c.must(func(ctx context.Context, tx pgx.Tx) error {
			list, err := nemura.Documents(ctx, tx, ana, tc.roles, tc.own)
			if err != nil || len(list) != tc.want {
				t.Errorf("%s sees %d documents: %v", name, len(list), err)
			}
			return nil
		})
	}
	if _, err := super.Exec(ctx, "UPDATE nemura.person_document SET title = 'x'"); err == nil || !strings.Contains(err.Error(), "NM127") {
		t.Errorf("editing a document: %v", err)
	}
}

// The salary bank account changes only by a proposal from the person themselves, approved by an HR or Payroll administrator or the Owner who is neither
// the proposer nor the account's person, and confirmed by that person with a passkey: each step in order, each audited, the account never in an event;
// the payroll approvers' notice is queued in the confirming transaction (NM-PPL-04, NM-ROLE-05).
// req: NM-PPL-04
// asvs: V8.2.1, V6.5.1, V14.1.1
func TestABankAccountChangesOnlyByProposalApprovalAndPasskeyConfirmation(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	now := today.Add(9 * time.Hour)
	cipher, _ := local.NewFieldKeys(bytes.Repeat([]byte{5}, 64))
	var ana uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		ana, err = nemura.AddPerson(ctx, tx, c.org, now, c.owner, people.Person{LegalName: "Ana Tan", EmploymentStart: d("2099-01-05"), MemberID: &c.pay}, people.Job{Title: "Analyst", Worksite: "SG", EmploymentType: "full_time"}, nil)
		return err
	})
	propose := func(who uuid.UUID, acct string) (uuid.UUID, error) {
		var id uuid.UUID
		err := c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			id, err = nemura.ProposeBankChange(ctx, tx, cipher, c.org, now, who, ana, acct)
			return err
		})
		return id, err
	}
	if _, err := propose(c.hr, "123-456-789"); personRefusal(err) != people.CodeInvalid {
		t.Errorf("someone else proposing: %v", err)
	}
	if _, err := propose(c.pay, "12ab"); personRefusal(err) != people.CodeInvalid {
		t.Errorf("a bad account: %v", err)
	}
	change, err := propose(c.pay, "123-456-7890")
	if err != nil {
		t.Fatal(err)
	}
	decide := func(who uuid.UUID, approve bool, reason string, roles ...authz.Role) error {
		return c.do(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.DecideBankChange(ctx, tx, c.org, now, who, roles, change, approve, reason)
		})
	}
	confirm := func(who uuid.UUID, fresh bool) error {
		return c.do(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.ConfirmBankChange(ctx, tx, c.org, now, who, change, fresh)
		})
	}
	// Before approval nothing confirms; the proposer and the account's person never decide; an employee or a manager cannot.
	if err := confirm(c.pay, true); personRefusal(err) != people.CodeNotFound {
		t.Errorf("confirming before approval: %v", err)
	}
	for name, tc := range map[string]struct {
		who   uuid.UUID
		roles []authz.Role
	}{"the account's own person with payroll rights": {c.pay, []authz.Role{authz.PayrollAdmin}}, "a line manager": {c.hr, []authz.Role{authz.LineManager}}, "an employee": {c.hr, []authz.Role{authz.Employee}}} {
		if err := decide(tc.who, true, "", tc.roles...); personRefusal(err) != people.CodeInvalid {
			t.Errorf("%s deciding: %v", name, err)
		}
	}
	if err := decide(c.hr, false, "", authz.HRAdmin); personRefusal(err) != people.CodeInvalid {
		t.Errorf("a refusal with no reason: %v", err)
	}
	if err := decide(c.hr, true, "", authz.HRAdmin); err != nil {
		t.Fatal(err)
	}
	if err := decide(c.owner, true, "", authz.Owner); personRefusal(err) != people.CodeNotFound {
		t.Errorf("deciding twice: %v", err)
	}
	// Approved is not yet in force: confirming needs the person and a fresh passkey.
	if err := confirm(c.pay, false); personRefusal(err) != people.CodeInvalid {
		t.Errorf("confirming with no passkey: %v", err)
	}
	if err := confirm(c.hr, true); personRefusal(err) != people.CodeInvalid {
		t.Errorf("someone else confirming: %v", err)
	}
	var shown string
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, _, err := nemura.ReadSecret(ctx, tx, cipher, c.org, now, c.pay, []authz.Role{authz.Employee}, ana, "bank")
		if !errors.Is(err, nemura.ErrHidden) {
			t.Errorf("a bank account in force before confirmation: %v", err)
		}
		return nil
	})
	if err := confirm(c.pay, true); err != nil {
		t.Fatal(err)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		shown, _, err = nemura.ReadSecret(ctx, tx, cipher, c.org, now, c.pay, []authz.Role{authz.Employee}, ana, "bank")
		if err != nil || shown != "1234567890" {
			t.Errorf("the person reads their account: %q %v", shown, err)
		}
		masked, level, err := nemura.ReadSecret(ctx, tx, cipher, c.org, now, c.hr, []authz.Role{authz.HROfficer}, ana, "bank")
		if err != nil || masked != "****7890" || level != access.Masked {
			t.Errorf("the HR officer reads %q at %v: %v", masked, level, err)
		}
		return nil
	})
	super := connect(t, db.SuperURL())
	var notices, leaked int
	if err := super.QueryRow(ctx, "SELECT count(*) FROM nemura.outbox WHERE organisation_id = $1 AND kind = 'people.notice' AND business_key = $2", c.org, "bank:"+change.String()).Scan(&notices); err != nil || notices != 1 {
		t.Errorf("the payroll approvers' notice: %d, %v", notices, err)
	}
	if err := super.QueryRow(ctx, "SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND (body LIKE '%1234567890%' OR body LIKE '%123-456-7890%')", c.org).Scan(&leaked); err != nil || leaked != 0 {
		t.Errorf("the account in the trail: %d, %v", leaked, err)
	}
	// The change only moves forward, below the code.
	for _, sql := range []string{"UPDATE nemura.bank_account_change SET state = 'proposed'", "UPDATE nemura.bank_account_change SET new_masked = '****0000'", "DELETE FROM nemura.bank_account_change"} {
		if _, err := super.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "NM126") {
			t.Errorf("%s: %v", sql, err)
		}
	}
	if _, err := store(t, "nemura_worker").VerifyAudit(ctx, c.org); err != nil {
		t.Errorf("the chain: %v", err)
	}
}
