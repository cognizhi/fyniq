//go:build local

package nemura_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/nemura/selfservice"
)

func selfCode(err error) string {
	var r *selfservice.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// A person changes contact details and emergency contacts at once (audited without the values); a change of legal name or residency waits for evidence and another person's
// approval and applies on approval; a change of NRIC or FIN holds no number; nobody decides their own change or answers their own question; a question is answered once; the
// person follows every request in one list (NM-ESS-03, NM-ESS-04).
// req: NM-ESS-03, NM-ESS-04
// asvs: V8.2.1, V8.3.1
func TestAPersonChangesTheirOwnDetailsAndFollowsTheirRequests(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	blobs, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := nemura.SelfService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now), Blobs: blobs}
	ben := api.Session{Organisation: lt.c.org, Member: lt.benLogin}
	ana := api.Session{Organisation: lt.c.org, Member: lt.c.owner}
	nobody := api.Session{Organisation: lt.c.org, Member: lt.c.pay}
	su := connect(t, db.SuperURL())

	// Direct changes.
	if err := svc.SetDetails(ctx, ben, api.MyDetails{Phone: "call me"}); selfCode(err) != selfservice.CodeInvalid {
		t.Errorf("a bad phone: %v", err)
	}
	if err := svc.SetDetails(ctx, ben, api.MyDetails{Phone: "+65 9123 4567", PersonalEmail: "ben@example.com", Address: "1 Raffles Place #20-01", PostalCode: "048616"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetEmergency(ctx, ben, []api.EmergencyContact{{Name: "Mei Lim", Relationship: "spouse", Phone: "9123 4567"}, {Name: "Tan Ah Kow", Relationship: "father", Phone: "9876 5432"}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetEmergency(ctx, ben, []api.EmergencyContact{{Name: "Mei Lim", Relationship: "spouse", Phone: "9123 4567"}}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Details(ctx, ben)
	if err != nil || got.Phone != "+65 9123 4567" || got.PersonalEmail != "ben@example.com" || got.PostalCode != "048616" || len(got.Emergency) != 1 || got.Emergency[0].Name != "Mei Lim" {
		t.Fatalf("details: %+v %v", got, err)
	}
	if _, err := svc.Details(ctx, nobody); err == nil {
		t.Error("a login with no person has details")
	}
	// The patterns carry a space or a letter, which a hash never does, so a run of hexadecimal digits cannot match.
	var leaks int
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND (body LIKE '%9123 4567%' OR body LIKE '%ben@example%' OR body LIKE '%Raffles%' OR body LIKE '%Mei Lim%' OR body LIKE '%Tan Ah Kow%' OR body LIKE '%"048616"%')`, lt.c.org).Scan(&leaks); err != nil || leaks != 0 {
		t.Errorf("the audit trail holds %d personal details: %v", leaks, err)
	}
	var events int
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND action IN ('nemura.person.phone_changed', 'nemura.person.contact_changed', 'nemura.person.emergency_contacts_changed')`, lt.c.org).Scan(&events); err != nil || events != 4 {
		t.Errorf("events: %d %v", events, err)
	}
	if err := svc.SetDetails(ctx, ben, api.MyDetails{Phone: "+65 9123 4567", PersonalEmail: "ben@example.com", Address: "1 Raffles Place #20-01", PostalCode: "048616"}); err != nil {
		t.Fatal(err)
	}
	_ = su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND action IN ('nemura.person.phone_changed', 'nemura.person.contact_changed', 'nemura.person.emergency_contacts_changed')`, lt.c.org).Scan(&events)
	if events != 4 {
		t.Errorf("an unchanged save was audited: %d", events)
	}
	if _, err := su.Exec(ctx, "INSERT INTO nemura.emergency_contact (organisation_id, person_id, position, contact_name, relationship, contact_phone, updated_at) VALUES ($1, $2, 4, 'x', 'y', '123', now())", lt.c.org, lt.ben); err == nil {
		t.Error("a fourth emergency contact")
	}

	// A change of legal name: evidence and a note, decided by someone else, applied on approval.
	evidence := []byte("%PDF-1.4 deed poll")
	ask := api.DetailsChange{Kind: "legal_name", LegalName: "Ben Lim Wei", Note: "Changed by deed poll.", Evidence: api.ChangeEvidence{Title: "Deed poll", ContentType: "application/pdf"}}
	if _, _, err := (nemura.SelfService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now)}).RequestChange(ctx, ben, ask, evidence); !errors.Is(err, api.ErrUnavailable) {
		t.Errorf("evidence with no file store: %v", err)
	}
	if _, _, err := svc.RequestChange(ctx, ben, api.DetailsChange{Kind: "legal_name", Note: "x", Evidence: ask.Evidence}, evidence); selfCode(err) != selfservice.CodeInvalid {
		t.Errorf("a name change with no name: %v", err)
	}
	id, due, err := svc.RequestChange(ctx, ben, ask, evidence)
	if err != nil || due != "2026-11-27" {
		t.Fatalf("change: %s %s %v", id, due, err)
	}
	cid := mustUUID(id)
	queue, err := svc.Queue(ctx, ana)
	if err != nil || len(queue) != 1 || queue[0].Person != "Ben Lim" || queue[0].ChangeKind != "legal_name" || queue[0].EvidenceID == "" || queue[0].Step != "received" {
		t.Fatalf("queue: %+v %v", queue, err)
	}
	if err := svc.Decide(ctx, ben, cid, true, ""); selfCode(err) != selfservice.CodeRole {
		t.Errorf("deciding one's own change: %v", err)
	}
	if err := svc.Decide(ctx, ana, cid, false, " "); selfCode(err) != selfservice.CodeInvalid {
		t.Errorf("a refusal with no reason: %v", err)
	}
	if err := svc.Decide(ctx, ana, cid, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.Decide(ctx, ana, cid, true, ""); selfCode(err) != selfservice.CodeConflict {
		t.Errorf("deciding twice: %v", err)
	}
	var name string
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT legal_name FROM nemura.person WHERE id = $1", lt.ben).Scan(&name)
	})
	if name != "Ben Lim Wei" {
		t.Errorf("the legal name is %q", name)
	}
	if _, err := su.Exec(ctx, "UPDATE nemura.details_change SET note = 'other' WHERE id = $1", id); err == nil || !strings.Contains(err.Error(), "NM165") {
		t.Errorf("a decided request changed: %v", err)
	}
	// Evidence is on the person's file, for them and HR.
	docs, err := nemura.RestrictedService{Store: store(t, "nemura_api"), Blobs: blobs, Clock: clock.NewFrozen(now)}.Documents(ctx, ben, lt.ben)
	if err != nil || len(docs) != 1 || docs[0].Title != "Deed poll" {
		t.Errorf("the person's documents: %+v %v", docs, err)
	}

	// A residency change applies from the decision day as a new classification; a refusal keeps the record.
	pr := "2024-03-01"
	rid, _, err := svc.RequestChange(ctx, ben, api.DetailsChange{Kind: "residency", Residency: "permanent_resident", PRStart: pr, Note: "Became a PR.", Evidence: ask.Evidence}, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Decide(ctx, ana, mustUUID(rid), true, ""); err != nil {
		t.Fatal(err)
	}
	var res string
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		c, ok, err := nemura.ClassificationOn(ctx, tx, lt.ben, now.AddDate(0, 0, 2))
		if !ok || err != nil {
			t.Fatalf("classification: %v %v", ok, err)
		}
		res = c.Residency
		return nil
	})
	if res != "permanent_resident" {
		t.Errorf("residency %q", res)
	}
	nid, _, err := svc.RequestChange(ctx, ben, api.DetailsChange{Kind: "nric_fin", Note: "My FIN became an NRIC.", Evidence: ask.Evidence}, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.RequestChange(ctx, ben, api.DetailsChange{Kind: "nric_fin", LegalName: "S1234567D", Note: "x", Evidence: ask.Evidence}, evidence); selfCode(err) != selfservice.CodeInvalid {
		t.Errorf("an NRIC change with a number: %v", err)
	}
	if err := svc.Decide(ctx, ana, mustUUID(nid), false, "The copy is not legible: attach a clearer one."); err != nil {
		t.Fatal(err)
	}

	// A question is answered once, by someone else; a withdrawn one cannot be answered.
	qid, qdue, err := svc.Ask(ctx, ben, "How many days of annual leave do I get next year?")
	if err != nil || qdue != "2026-11-25" {
		t.Fatalf("ask: %s %v", qdue, err)
	}
	if err := svc.Reply(ctx, ben, mustUUID(qid), "Eight."); selfCode(err) != selfservice.CodeRole {
		t.Errorf("answering one's own question: %v", err)
	}
	if err := svc.Reply(ctx, ana, mustUUID(qid), "Eight days from your second year."); err != nil {
		t.Fatal(err)
	}
	if err := svc.Reply(ctx, ana, mustUUID(qid), "Again."); selfCode(err) != selfservice.CodeConflict {
		t.Errorf("answering twice: %v", err)
	}
	q2, _, _ := svc.Ask(ctx, ben, "Can I carry leave forward?")
	if err := svc.Withdraw(ctx, nobody, mustUUID(q2)); err == nil {
		t.Error("withdrawing someone else's question")
	}
	if err := svc.Withdraw(ctx, ben, mustUUID(q2)); err != nil {
		t.Fatal(err)
	}
	if err := svc.Withdraw(ctx, ben, mustUUID(q2)); selfCode(err) != selfservice.CodeNotFound {
		t.Errorf("withdrawing twice: %v", err)
	}
	if err := svc.Reply(ctx, ana, mustUUID(q2), "Yes."); selfCode(err) != selfservice.CodeConflict {
		t.Errorf("answering a withdrawn question: %v", err)
	}
	// One list, every kind, with its step and reply.
	list, err := svc.Requests(ctx, ben)
	if err != nil || len(list) != 5 {
		t.Fatalf("requests: %d %v", len(list), err)
	}
	steps := map[string]string{}
	for _, r := range list {
		steps[r.Title+"/"+r.State] = r.Step
		if r.Title == "nric_fin" && !strings.Contains(r.Reply, "not legible") {
			t.Errorf("the reason is not on the request: %+v", r)
		}
		if r.Kind == "question" && r.State == "answered" && r.Reply != "Eight days from your second year." {
			t.Errorf("the reply: %+v", r)
		}
	}
	if steps["legal_name/approved"] != "ready" || steps["nric_fin/refused"] != "closed" || steps["question/answered"] != "ready" || steps["question/withdrawn"] != "closed" {
		t.Errorf("steps: %v", steps)
	}
	if q, _ := svc.Queue(ctx, ana); len(q) != 0 {
		t.Errorf("the queue still holds %d", len(q))
	}
}

// After a person's last day their account is a leaver's: it holds no role whatever it held, it is marked as a leaver's with the day it ends, and it ends with the record-keeping
// period (NM-ESS-06, NM-SEC-04).
// req: NM-ESS-06
// asvs: V8.2.1, V8.3.1
func TestALeaversAccountHoldsNoRoleAndEndsWithTheRecordKeepingPeriod(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	api := store(t, "nemura_api")
	var p nemura.PersonRow
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		p, err = nemura.PersonOn(ctx, tx, lt.ben, now)
		return err
	})
	last := d("2099-12-15")
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.UpdatePerson(ctx, tx, lt.c.org, now, lt.c.owner, lt.ben, people.Person{LegalName: p.LegalName, EmploymentStart: p.Start, LeavingDate: &last, LeavingReason: "resigned"})
	})
	at := func(day string) (nemura.Session, []string, error) {
		moment := d(day).Add(9 * time.Hour)
		var token string
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			token, err = nemura.StartSession(ctx, tx, lt.c.org, moment, lt.c.hr)
			return err
		})
		s, err := api.Authenticate(ctx, token, moment)
		roles, _ := api.RolesAt(ctx, lt.c.org, lt.c.hr, moment)
		out := make([]string, len(roles))
		for i, r := range roles {
			out[i] = string(r)
		}
		return s, out, err
	}
	if s, roles, err := at("2099-12-15"); err != nil || s.Leaver || len(roles) == 0 {
		t.Errorf("on the last day: %+v %v %v", s, roles, err)
	}
	s, roles, err := at("2099-12-16")
	if err != nil || !s.Leaver || len(roles) != 0 || s.LeaverUntil == nil || s.LeaverUntil.Format(time.DateOnly) != "2100-12-15" {
		t.Errorf("the day after: %+v %v %v", s, roles, err)
	}
	if s, _, err := at("2100-12-14"); err != nil || !s.Leaver {
		t.Errorf("the day before it ends: %+v %v", s, err)
	}
	if _, _, err := at("2100-12-15"); err == nil {
		t.Error("the account still opens when the record-keeping period has ended")
	}
	// A person who stays holds their roles whatever happens to a leaver.
	var token string
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		token, err = nemura.StartSession(ctx, tx, lt.c.org, d("2099-12-16").Add(9*time.Hour), lt.c.owner)
		return err
	})
	if s, err := api.Authenticate(ctx, token, d("2099-12-16").Add(9*time.Hour)); err != nil || s.Leaver {
		t.Errorf("the owner: %+v %v", s, err)
	}
}
