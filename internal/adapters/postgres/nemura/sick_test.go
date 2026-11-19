//go:build local

package nemura_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

func (lt *leaveTeam) notify(a nemura.SickAsk, at string) (nemura.SickResult, error) {
	var r nemura.SickResult
	err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		r, err = nemura.NotifySick(ctx, tx, lt.c.org, d(at).Add(9*time.Hour), lt.benLogin, peoplePack(lt.c.t), a)
		return err
	})
	return r, err
}

func (lt *leaveTeam) sick(kind, from, to string) nemura.SickAsk {
	return nemura.SickAsk{Person: lt.ben, Kind: kind, From: d(from), To: d(to)}
}

func (lt *leaveTeam) sickBalance(typ string) leave.Balance {
	var b leave.Balance
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		b, err = nemura.LeaveBalance(ctx, tx, lt.ben, typ, 2026)
		return err
	})
	return b
}

// Sick leave is notified: the working days are taken against the ceiling in force on each day, outpatient and hospitalisation sharing one; days beyond it are
// recorded as unpaid and still accepted; a late notice is flagged and accepted; nothing is taken twice for days that already hold leave or sick leave.
// req: NM-LV-12, NM-LV-06
// asvs: V8.2.1
func TestSickLeaveIsNotifiedAgainstOneSharedCeiling(t *testing.T) {
	lt := newLeaveTeam(t)
	// Ben started 2026-01-05: in May he has four completed months, so 8 outpatient and 30 hospitalisation days. Mon 4 to Wed 6 May: three outpatient days.
	r, err := lt.notify(lt.sick(leave.Outpatient, "2026-05-04", "2026-05-06"), "2026-05-04")
	if err != nil || r.Days != leave.Whole(3) || r.Paid != leave.Whole(3) || r.Unpaid != 0 || r.Late {
		t.Fatalf("first notification: %+v %v", r, err)
	}
	if b := lt.sickBalance(leave.SickOutpatientType); b.Entitled != leave.Whole(8) || b.Taken != leave.Whole(3) || b.Left != leave.Whole(5) {
		t.Errorf("outpatient: %+v", b)
	}
	if b := lt.sickBalance(leave.SickHospitalType); b.Entitled != leave.Whole(30) || b.Taken != leave.Whole(3) || b.Left != leave.Whole(27) {
		t.Errorf("an outpatient day also uses the hospitalisation ceiling: %+v", b)
	}
	// Ten more outpatient days, told two days late: five paid, five unpaid; the late notice is flagged and accepted.
	r, err = lt.notify(lt.sick(leave.Outpatient, "2026-05-11", "2026-05-22"), "2026-05-13")
	if err != nil || r.Days != leave.Whole(10) || r.Paid != leave.Whole(5) || r.Unpaid != leave.Whole(5) || !r.Late {
		t.Fatalf("second notification: %+v %v", r, err)
	}
	// Hospitalisation days share what the outpatient days left. 2 June to 3 July less the observed Vesak holiday on 1 June is 24 working days, all paid
	// (the ceiling steps up to 45 on 5 June); July's 20 more are paid too (60 from 5 July).
	if r, err := lt.notify(lt.sick(leave.Hospitalisation, "2026-06-01", "2026-07-03"), "2026-06-01"); err != nil || r.Days != leave.Whole(24) || r.Paid != leave.Whole(24) {
		t.Fatalf("third notification: %+v %v", r, err)
	}
	if r, err := lt.notify(lt.sick(leave.Hospitalisation, "2026-07-06", "2026-07-31"), "2026-07-06"); err != nil || r.Days != leave.Whole(20) || r.Paid != leave.Whole(20) {
		t.Fatalf("fourth notification: %+v %v", r, err)
	}
	// 8 + 5 + 24 + 20 = ... the outpatient days used 8 of the shared 60, then 24 and 20 hospitalisation days: 52 used, so of 29 more days 8 are paid and 21 are not.
	if r, err := lt.notify(lt.sick(leave.Hospitalisation, "2026-09-01", "2026-10-09"), "2026-09-01"); err != nil || r.Days != leave.Whole(29) || r.Paid != leave.Whole(8) || r.Unpaid != leave.Whole(21) {
		t.Fatalf("beyond the shared ceiling: %+v %v", r, err)
	}
	if b := lt.sickBalance(leave.SickHospitalType); b.Taken != leave.Whole(60) || b.Left != 0 || b.Entitled != leave.Whole(60) {
		t.Errorf("the hospitalisation ceiling is used up: %+v", b)
	}
	// The same days again, a day of annual leave, a weekend, and the other year are each refused or counted as they should.
	if _, err := lt.notify(lt.sick(leave.Outpatient, "2026-05-05", "2026-05-05"), "2026-05-05"); leaveCode(err) != leave.CodeConflict {
		t.Errorf("the same day twice: %v", err)
	}
	if _, err := lt.apply(lt.ask("2026-05-04", "2026-05-04")); leaveCode(err) != leave.CodeConflict {
		t.Errorf("annual leave on a sick day: %v", err)
	}
	if _, err := lt.apply(lt.ask("2026-08-03", "2026-08-04")); err != nil {
		t.Fatal(err)
	}
	if _, err := lt.notify(lt.sick(leave.Outpatient, "2026-08-04", "2026-08-04"), "2026-08-04"); leaveCode(err) != leave.CodeConflict {
		t.Errorf("a sick day inside annual leave: %v", err)
	}
	if _, err := lt.notify(lt.sick(leave.Outpatient, "2026-05-30", "2026-05-31"), "2026-06-02"); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("a weekend costs nothing: %v", err)
	}
	if _, err := lt.notify(lt.sick("flu", "2026-09-01", "2026-09-01"), "2026-09-01"); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("an unknown kind: %v", err)
	}
	// Before three completed months service nothing is paid, and the notification is still recorded.
	early := lt.sick(leave.Outpatient, "2026-03-02", "2026-03-02")
	if r, err := lt.notify(early, "2026-03-02"); err != nil || r.Paid != 0 || r.Unpaid != leave.Whole(1) {
		t.Errorf("before three months: %+v %v", r, err)
	}
}

// The certificate follows on return and is given once; HR verifies it once, and nobody verifies their own; a notification is evidence, never edited or deleted;
// a sick notification is read by the person and the people team only.
// req: NM-LV-06, NM-LV-12
// asvs: V8.2.1, V8.3.1
func TestACertificateIsGivenOnceAndVerifiedByAnotherPerson(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	r, err := lt.notify(lt.sick(leave.Outpatient, "2026-05-04", "2026-05-04"), "2026-05-04")
	if err != nil {
		t.Fatal(err)
	}
	at := d("2026-05-05").Add(9 * time.Hour)
	// A document stands for the certificate (the restricted document of the person).
	doc := uuid.Must(uuid.NewV7())
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.AddDocument(ctx, tx, lt.c.org, at, lt.benLogin, lt.ben, doc, nemura.DocumentInput{Type: "certificate", Title: "Medical certificate", RetentionClass: "employment-records",
			BlobKey: "x", ContentType: "application/pdf", Size: 3, Viewers: []string{"hr_officer", "hr_admin"}})
	})
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.VerifySick(ctx, tx, lt.c.org, at, lt.c.owner, r.ID)
	}); leaveCode(err) != leave.CodeConflict {
		t.Errorf("verifying with no certificate: %v", err)
	}
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.AttachSickCertificate(ctx, tx, lt.c.org, at, lt.benLogin, r.ID, doc, "")
	}); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("a certificate with no issuer: %v", err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.AttachSickCertificate(ctx, tx, lt.c.org, at, lt.benLogin, r.ID, doc, "Raffles Medical Clinic")
	})
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.AttachSickCertificate(ctx, tx, lt.c.org, at, lt.benLogin, r.ID, doc, "Another clinic")
	}); leaveCode(err) != leave.CodeConflict {
		t.Errorf("a second certificate: %v", err)
	}
	// Ben's own login cannot verify his certificate (the database refuses); Ana can, once.
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.VerifySick(ctx, tx, lt.c.org, at, lt.benLogin, r.ID)
	}); leaveCode(err) != leave.CodeRole {
		t.Errorf("self-verification: %v", err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.VerifySick(ctx, tx, lt.c.org, at, lt.c.owner, r.ID)
	})
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.VerifySick(ctx, tx, lt.c.org, at, lt.c.owner, r.ID)
	}); leaveCode(err) != leave.CodeConflict {
		t.Errorf("verifying twice: %v", err)
	}
	list := func(s nemura.SickScope) []nemura.SickRow {
		var rows []nemura.SickRow
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			rows, err = nemura.Sick(ctx, tx, s, nil)
			return err
		})
		return rows
	}
	if rows := list(nemura.SickScope{Member: lt.benLogin}); len(rows) != 1 || rows[0].Issuer != "Raffles Medical Clinic" || !rows[0].Verified || rows[0].Kind != leave.Outpatient {
		t.Errorf("the person's own: %+v", rows)
	}
	if rows := list(nemura.SickScope{Member: lt.c.owner}); len(rows) != 0 {
		t.Errorf("a manager reads %d sick notifications of a report", len(rows))
	}
	if rows := list(nemura.SickScope{Member: lt.c.pay}); len(rows) != 0 {
		t.Errorf("a stranger reads %d", len(rows))
	}
	if rows := list(nemura.SickScope{Member: lt.c.hr, All: true}); len(rows) != 1 {
		t.Errorf("the people team reads %d", len(rows))
	}
	if rows := list(nemura.SickScope{Member: lt.c.hr, All: true, Unverified: true}); len(rows) != 0 {
		t.Errorf("a verified one is still waiting: %d", len(rows))
	}
	su := connect(t, db.SuperURL())
	for _, q := range []string{"UPDATE nemura.sick_notification SET days = 9", "DELETE FROM nemura.sick_notification", "UPDATE nemura.sick_notification SET issuer = 'x'"} {
		if _, err := su.Exec(ctx, q); err == nil {
			t.Errorf("%s was allowed", q)
		}
	}
}

// The sick leave service notifies as the signed-in person, stores the certificate as a restricted document the people team sees and the manager does not, and lists
// by scope; without a file store the certificate says it is unavailable and the notification still works.
// req: NM-LV-06, NM-LV-12
// asvs: V8.2.1, V14.1.1
func TestTheSickServiceStoresTheCertificateAsARestrictedDocument(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-05-05").Add(9 * time.Hour)
	blobs, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	packs, err := nemura.NewPacksService(store(t, "nemura_api"), clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	svc := nemura.SickService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now), Packs: packs, Blobs: blobs}
	noFiles := nemura.SickService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now), Packs: packs}
	ben := api.Session{Organisation: lt.c.org, Member: lt.benLogin}
	ana := api.Session{Organisation: lt.c.org, Member: lt.c.owner}
	n, err := svc.Notify(ctx, ben, api.SickAsk{Kind: "outpatient", From: "2026-05-04", To: "2026-05-04"})
	if err != nil || n.Paid != "1.00" || n.Late {
		t.Fatalf("notify: %+v %v", n, err)
	}
	id := uuid.MustParse(n.ID)
	if err := noFiles.Certificate(ctx, ben, id, false, "Clinic", "image/jpeg", []byte("x")); err != api.ErrUnavailable {
		t.Errorf("no file store: %v", err)
	}
	if err := svc.Certificate(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.pay}, id, false, "Clinic", "image/jpeg", []byte("x")); leaveCode(err) != leave.CodeNotFound {
		t.Errorf("a stranger gave a certificate: %v", err)
	}
	if err := svc.Certificate(ctx, ben, id, false, "Raffles Medical Clinic", "image/jpeg", []byte("jpeg bytes")); err != nil {
		t.Fatal(err)
	}
	// The certificate is a document of Ben that the HR officer and administrator may see and Ben and his manager may not.
	for member, want := range map[uuid.UUID]int{lt.benLogin: 0, lt.c.owner: 0} {
		var rows int
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			docs, err := nemura.Documents(ctx, tx, lt.ben, nil, member == lt.benLogin)
			rows = len(docs)
			return err
		})
		if rows != want {
			t.Errorf("member %s reads %d certificates, want %d", member, rows, want)
		}
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		docs, err := nemura.Documents(ctx, tx, lt.ben, []authz.Role{authz.HROfficer}, false)
		if err != nil || len(docs) != 1 || docs[0].Type != "certificate" {
			t.Errorf("the HR officer's documents: %+v %v", docs, err)
		}
		return nil
	})
	own, err := svc.List(ctx, ben, false, false)
	if err != nil || len(own) != 1 || !own[0].Certificate || own[0].Issuer != "Raffles Medical Clinic" || own[0].Verified {
		t.Fatalf("own list: %+v %v", own, err)
	}
	if list, _ := svc.List(ctx, ana, false, false); len(list) != 0 {
		t.Errorf("the manager reads %d", len(list))
	}
	waiting, err := svc.List(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.hr}, true, true)
	if err != nil || len(waiting) != 1 {
		t.Fatalf("waiting: %+v %v", waiting, err)
	}
	if err := svc.Verify(ctx, ana, id); err != nil {
		t.Fatal(err)
	}
	if again, _ := svc.List(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.hr}, true, true); len(again) != 0 {
		t.Errorf("still waiting: %d", len(again))
	}
	if _, err := svc.Notify(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.pay}, api.SickAsk{Kind: "outpatient", From: "2026-05-11", To: "2026-05-11"}); personRefusal(err) != people.CodeNotFound {
		t.Errorf("a login with no person: %v", err)
	}
}
