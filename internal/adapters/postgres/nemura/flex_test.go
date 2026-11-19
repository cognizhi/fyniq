//go:build local

package nemura_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/checks"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// A formal flexible work request keeps its receipt date and its deadline two months on; it is answered once, in writing, with its reason for a refusal, by someone other
// than the person; it is withdrawn only while it waits; nothing is edited or deleted; and the check warns before the deadline (NM-LV-10, N.1 #5).
// req: NM-LV-10
// asvs: V8.2.1
func TestAFlexibleWorkRequestIsAnsweredInWritingBeforeItsDeadline(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	v := peoplePack(t)
	at := d("2026-11-20").Add(10 * time.Hour)
	var id uuid.UUID
	var due time.Time
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, due, err = nemura.RequestFlex(ctx, tx, lt.c.org, at, lt.benLogin, v, nemura.FlexAsk{Person: lt.ben, Arrangement: leave.FlexiTime, From: d("2027-01-04"), Reason: "School run on Tuesdays."})
		return err
	})
	if due.Format(time.DateOnly) != "2027-01-20" {
		t.Errorf("deadline %s", due.Format(time.DateOnly))
	}
	for _, bad := range []nemura.FlexAsk{{Person: lt.ben, Arrangement: "remote", From: d("2027-01-04"), Reason: "x"}, {Person: lt.ben, Arrangement: leave.FlexiPlace, From: d("2027-01-04")}} {
		if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			_, _, err := nemura.RequestFlex(ctx, tx, lt.c.org, at, lt.benLogin, v, bad)
			return err
		}); leaveCode(err) != leave.CodeInvalid {
			t.Errorf("a bad request: %v", err)
		}
	}
	facts := func() []checks.FlexRequest {
		var f []checks.FlexRequest
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			f, err = nemura.FlexFacts(ctx, tx)
			return err
		})
		return f
	}
	if got := checks.FlexReplies(facts(), d("2027-01-05")); len(got) != 0 {
		t.Errorf("15 days before: %+v", got)
	}
	if got := checks.FlexReplies(facts(), d("2027-01-06")); len(got) != 1 || got[0].Check != 5 || !strings.Contains(got[0].Message, "in 14 days") {
		t.Errorf("14 days before: %+v", got)
	}
	if got := checks.FlexReplies(facts(), d("2027-01-21")); len(got) != 1 || !strings.Contains(got[0].Message, "had to be answered in writing by 2027-01-20") {
		t.Errorf("after the deadline: %+v", got)
	}
	reply := func(by uuid.UUID, outcome, text string) error {
		return lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.ReplyFlex(ctx, tx, lt.c.org, at.Add(24*time.Hour), by, id, outcome, text)
		})
	}
	// Not the person, and not without a reason.
	if err := reply(lt.benLogin, leave.Accepted, "Approved from January."); leaveCode(err) != leave.CodeRole {
		t.Errorf("answering one's own request: %v", err)
	}
	if err := reply(lt.c.owner, leave.Rejected, "No."); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("a refusal with no reason: %v", err)
	}
	if err := reply(lt.c.owner, leave.Alternative, "We can offer a 9am start on Tuesdays from 1 February while the release team is in place."); err != nil {
		t.Fatal(err)
	}
	if err := reply(lt.c.owner, leave.Accepted, "Changed my mind, approved."); leaveCode(err) != leave.CodeConflict {
		t.Errorf("answering twice: %v", err)
	}
	if err := lt.c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.WithdrawFlex(ctx, tx, lt.c.org, at, lt.benLogin, id)
	}); leaveCode(err) != leave.CodeConflict {
		t.Errorf("withdrawing an answered request: %v", err)
	}
	if got := checks.FlexReplies(facts(), d("2027-01-21")); len(got) != 0 {
		t.Errorf("an answered request still warns: %+v", got)
	}
	// Lists: the person reads their own, the people team everyone's, a stranger nothing.
	list := func(s nemura.FlexScope) []nemura.FlexRow {
		var rows []nemura.FlexRow
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			rows, err = nemura.FlexRequests(ctx, tx, s)
			return err
		})
		return rows
	}
	if rows := list(nemura.FlexScope{Member: lt.benLogin}); len(rows) != 1 || rows[0].State != "replied" || rows[0].Outcome != "alternative" || !strings.Contains(rows[0].Reply, "9am") {
		t.Errorf("own: %+v", rows)
	}
	if len(list(nemura.FlexScope{Member: lt.c.hr, All: true})) != 1 || len(list(nemura.FlexScope{Member: lt.c.pay})) != 0 || len(list(nemura.FlexScope{Member: lt.c.owner})) != 0 {
		t.Error("scopes")
	}
	// A request may be withdrawn while it waits.
	var id2 uuid.UUID
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id2, _, err = nemura.RequestFlex(ctx, tx, lt.c.org, at, lt.benLogin, v, nemura.FlexAsk{Person: lt.ben, Arrangement: leave.FlexiLoad, From: d("2027-03-01"), Reason: "Part time."})
		return err
	})
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.WithdrawFlex(ctx, tx, lt.c.org, at, lt.benLogin, id2)
	})
	su := connect(t, db.SuperURL())
	for _, q := range []string{"UPDATE nemura.flex_request SET received_on = received_on + 30", "UPDATE nemura.flex_request SET reply_due = reply_due + 30", "DELETE FROM nemura.flex_request", "UPDATE nemura.flex_request SET reason = 'x'"} {
		if _, err := su.Exec(ctx, q); err == nil {
			t.Errorf("%s was allowed", q)
		}
	}
}

// The service makes a request for the signed-in person with the deadline the pack gives, lists by scope, lets the people team answer and the person withdraw only their
// own.
// req: NM-LV-10
func TestTheFlexServiceMakesAnswersAndWithdrawsRequests(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(9 * time.Hour)
	packs, err := nemura.NewPacksService(store(t, "nemura_api"), clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	svc := nemura.FlexService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now), Packs: packs}
	ben := api.Session{Organisation: lt.c.org, Member: lt.benLogin}
	ana := api.Session{Organisation: lt.c.org, Member: lt.c.owner}
	id, due, err := svc.Request(ctx, ben, api.FlexAsk{Arrangement: "flexi_place", From: "2027-01-04", To: "2027-06-30", Reason: "Work from home on Fridays."})
	if err != nil || due != "2027-01-20" {
		t.Fatalf("request: %s %s %v", id, due, err)
	}
	if _, _, err := svc.Request(ctx, api.Session{Organisation: lt.c.org, Member: lt.c.pay}, api.FlexAsk{Arrangement: "flexi_place", From: "2027-01-04", Reason: "x"}); personRefusal(err) != people.CodeNotFound {
		t.Errorf("a login with no person: %v", err)
	}
	if _, _, err := svc.Request(ctx, ben, api.FlexAsk{Arrangement: "flexi_place", From: "soon", Reason: "x"}); leaveCode(err) != leave.CodeInvalid {
		t.Errorf("a bad date: %v", err)
	}
	mine, err := svc.List(ctx, ben, false)
	if err != nil || len(mine) != 1 || mine[0].To != "2027-06-30" || mine[0].State != "received" {
		t.Fatalf("own: %+v %v", mine, err)
	}
	if list, _ := svc.List(ctx, ana, false); len(list) != 0 {
		t.Errorf("a manager lists %d of a report's requests as their own", len(list))
	}
	if all, _ := svc.List(ctx, ana, true); len(all) != 1 {
		t.Errorf("the people team lists %d", len(all))
	}
	rid := uuid.MustParse(id)
	if err := svc.Withdraw(ctx, ana, rid); leaveCode(err) != leave.CodeNotFound {
		t.Errorf("withdrawing someone else's request: %v", err)
	}
	if err := svc.Reply(ctx, ana, rid, "accepted", "Approved for Fridays from January."); err != nil {
		t.Fatal(err)
	}
	if all, _ := svc.List(ctx, ana, true); all[0].State != "replied" || all[0].Outcome != "accepted" || all[0].RepliedAt == "" {
		t.Errorf("after the reply: %+v", all[0])
	}
}
