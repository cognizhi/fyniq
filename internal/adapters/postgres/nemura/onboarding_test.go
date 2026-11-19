//go:build local

package nemura_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/onboarding"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

func onboardCode(err error) string {
	var r *onboarding.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// A company that invites its people to Fyniq gets one task for each new person, decided once; a company that does not gets none; the setting raises no task for people already
// added (NM-ONB-02).
// req: NM-ONB-02
func TestAnInvitationTaskIsRaisedOncePerNewPersonWhenTheCompanyAsksForIt(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	st := store(t, "nemura_api")
	svc := nemura.OnboardingService{Store: st, Clock: clock.NewFrozen(now)}
	ps := nemura.PeopleService{Store: st, Clock: clock.NewFrozen(now)}
	hr := api.Session{Organisation: lt.c.org, Member: lt.c.hr}
	owner := api.Session{Organisation: lt.c.org, Member: lt.c.owner}
	su := connect(t, db.SuperURL())
	add := func(name string) uuid.UUID {
		id := uuid.Must(uuid.NewV7())
		if err := ps.Create(ctx, owner, id, people.Person{LegalName: name, EmploymentStart: d("2026-12-01")}, people.Job{Title: "Analyst", EmploymentType: "full_time", Worksite: "SG"}, nil); err != nil {
			t.Fatal(err)
		}
		return id
	}
	_ = add("Dee Off")
	if tasks, _ := svc.Tasks(ctx, hr); len(tasks) != 0 {
		t.Errorf("a task with the setting off: %d", len(tasks))
	}
	if on, _ := svc.Settings(ctx, hr); on {
		t.Error("the setting starts on")
	}
	if err := svc.SetSettings(ctx, hr, true); err != nil {
		t.Fatal(err)
	}
	if tasks, _ := svc.Tasks(ctx, hr); len(tasks) != 0 {
		t.Errorf("switching on raised %d tasks for people already added", len(tasks))
	}
	on := add("Eve On")
	tasks, err := svc.Tasks(ctx, hr)
	if err != nil || len(tasks) != 1 || tasks[0].Person != "Eve On" || tasks[0].State != "open" || tasks[0].PersonID != on.String() {
		t.Fatalf("tasks: %+v %v", tasks, err)
	}
	// Once per person: raising again (through the store) adds nothing.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.RaiseJoinTask(ctx, tx, lt.c.org, now, lt.c.owner, on)
	})
	if tasks, _ := svc.Tasks(ctx, hr); len(tasks) != 1 {
		t.Errorf("a second task for one person: %d", len(tasks))
	}
	tid := mustUUID(tasks[0].ID)
	if err := svc.DecideTask(ctx, hr, tid, "later", ""); onboardCode(err) != onboarding.CodeInvalid {
		t.Errorf("an unknown state: %v", err)
	}
	if err := svc.DecideTask(ctx, hr, tid, "done", "Invited as an employee."); err != nil {
		t.Fatal(err)
	}
	if err := svc.DecideTask(ctx, hr, tid, "dismissed", ""); onboardCode(err) != onboarding.CodeNotFound {
		t.Errorf("deciding twice: %v", err)
	}
	if _, err := su.Exec(ctx, "UPDATE nemura.join_task SET state = 'open', decided_at = NULL, decided_by = NULL WHERE id = $1", tid); err == nil || !strings.Contains(err.Error(), "NM170") {
		t.Errorf("a decided task reopened: %v", err)
	}
	if tasks, _ := svc.Tasks(ctx, hr); tasks[0].State != "done" || tasks[0].Note != "Invited as an employee." {
		t.Errorf("the decided task: %+v", tasks[0])
	}
}

// A leaving keeps its dates apart, sets the leaving date on the record, shows the leave to the last day, where the service letter is and when the account narrows; it is changed
// and withdrawn before the last day and not after (NM-ONB-03).
// req: NM-ONB-03
func TestALeavingKeepsItsDatesApartAndSettlesTheLeaveToTheLastDay(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	st := store(t, "nemura_api")
	packs, err := nemura.NewPacksService(st, clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	svc := nemura.OnboardingService{Store: st, Clock: clock.NewFrozen(now), Packs: packs}
	hr := api.Session{Organisation: lt.c.org, Member: lt.c.hr}
	su := connect(t, db.SuperURL())
	in := api.OffboardingInput{Kind: "resignation", NoticeGiven: "2027-05-29", LastDay: "2027-06-30", EquipmentReturned: "2027-06-30"}
	if err := svc.SetOffboarding(ctx, hr, lt.cy, api.OffboardingInput{Kind: "resignation", LastDay: "soon"}); onboardCode(err) != onboarding.CodeInvalid {
		t.Errorf("a last day that is not a date: %v", err)
	}
	if err := svc.SetOffboarding(ctx, hr, lt.cy, api.OffboardingInput{Kind: "resignation", RetrenchmentNotified: "2027-05-01", LastDay: "2027-06-30"}); onboardCode(err) != onboarding.CodeInvalid {
		t.Errorf("a retrenchment date for a resignation: %v", err)
	}
	if err := svc.SetOffboarding(ctx, hr, lt.cy, in); err != nil {
		t.Fatal(err)
	}
	var leaving *time.Time
	var reason string
	_ = su.QueryRow(ctx, "SELECT leaving_date, coalesce(leaving_reason, '') FROM nemura.person WHERE id = $1", lt.cy).Scan(&leaving, &reason)
	if leaving == nil || leaving.Format(time.DateOnly) != "2027-06-30" || reason != "resignation" {
		t.Errorf("the record: %v %q", leaving, reason)
	}
	v, ok, err := svc.Offboarding(ctx, hr, lt.cy)
	if err != nil || !ok || v.Person != "Cy Wong" || v.NoticeGiven != "2027-05-29" || v.LastDay != "2027-06-30" || v.NemuraAccessNarrows != "2027-07-01" || v.ServiceLetter != "none" {
		t.Fatalf("view: %+v %v %v", v, ok, err)
	}
	if v.RetrenchmentNotified != "" || v.EquipmentReturned != "2027-06-30" {
		t.Errorf("the dates are not kept apart: %+v", v)
	}
	// The leave to the last day is the pack's rule on the record as it now stands: nothing is written.
	if v.Settlement == nil || v.Settlement.Year != 2027 || len(v.Settlement.Basis) == 0 {
		t.Fatalf("the settlement: %+v", v.Settlement)
	}
	var want string
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		pv := peoplePack(t)
		e, err := nemura.AnnualEntitlement(ctx, tx, lt.cy, 2027, now, pv, nil)
		want = e.Days.String()
		return err
	})
	if v.Settlement.Entitled != want || v.Settlement.Taken != "0.00" || v.Settlement.Untaken != want {
		t.Errorf("settlement %+v, want %s", v.Settlement, want)
	}
	// Changing the last day moves the record; the list holds the active leaving.
	in.LastDay = "2027-07-31"
	if err := svc.SetOffboarding(ctx, hr, lt.cy, in); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.Offboardings(ctx, hr); len(list) != 1 || list[0].LastDay != "2027-07-31" {
		t.Errorf("the list: %+v", list)
	}
	// Withdrawn before the last day: the record's leaving date is cleared. After the last day it cannot be.
	if err := svc.CancelOffboarding(ctx, hr, lt.cy); err != nil {
		t.Fatal(err)
	}
	_ = su.QueryRow(ctx, "SELECT leaving_date FROM nemura.person WHERE id = $1", lt.cy).Scan(&leaving)
	if leaving != nil {
		t.Errorf("the leaving date stays: %v", leaving)
	}
	if err := svc.CancelOffboarding(ctx, hr, lt.cy); onboardCode(err) != onboarding.CodeNotFound {
		t.Errorf("withdrawing twice: %v", err)
	}
	past := api.OffboardingInput{Kind: "termination", LastDay: "2026-11-10"}
	if err := svc.SetOffboarding(ctx, hr, lt.cy, past); err != nil {
		t.Fatal(err)
	}
	if err := svc.CancelOffboarding(ctx, hr, lt.cy); onboardCode(err) != onboarding.CodeConflict {
		t.Errorf("withdrawing after the last day: %v", err)
	}
}
