//go:build local

package nemura_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/hiring"
)

// A report of records due for deletion is listed by kind and count with no name, is reviewed once by a person, and only after that does the nightly job delete and mark it deleted; a report not reviewed by
// its date is overdue (NM-SEC-05).
// req: NM-SEC-05, NM-REC-05
// asvs: V8.3.1
func TestADeletionReportIsListedReviewedOnceAndDeletedOnlyAfterTheReview(t *testing.T) {
	c := newHiringCo(t)
	ctx := context.Background()
	hr := c.sess(c.hr)
	cand, err := c.svc.Add(ctx, hr, c.req, api.CandidateInput{Name: "Gone Candidate", Email: "gone@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.Move(ctx, hr, mustID(t, cand.ID), "not_progressing", "Not a fit."); err != nil {
		t.Fatal(err)
	}
	worker := store(t, "nemura_worker")
	due := c.now.AddDate(0, 6, 1)
	if _, rep, err := worker.RunCandidateRetention(ctx, c.org, due); err != nil || rep != 1 {
		t.Fatalf("%d %v", rep, err)
	}
	svc := nemura.DeletionReportService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(due)}
	l, err := svc.List(ctx, c.sess(c.owner))
	if err != nil || len(l) != 1 || l[0].RecordType != hiring.CandidatesRecordType || l[0].Due != 1 || l[0].State != "pending" || l[0].Overdue {
		t.Fatalf("%+v %v", l, err)
	}
	late := nemura.DeletionReportService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(due.AddDate(0, 0, 31))}
	if l2, _ := late.List(ctx, c.sess(c.owner)); len(l2) != 1 || !l2[0].Overdue {
		t.Errorf("a report not reviewed by its date is overdue: %+v", l2)
	}
	// N.1 #11 and #12: the waiting report is a ⚠ for Hiring and for Records, a ✖ for Records once its date has passed, and nothing once it is reviewed.
	checksAt := func(on time.Time) string {
		ps, err := nemura.NewPacksService(store(t, "nemura_api"), clock.NewFrozen(on))
		if err != nil {
			t.Fatal(err)
		}
		all, err := ps.Checks(ctx, c.sess(c.owner), on)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, f := range all {
			if f.Check == 11 || f.Check == 12 {
				got = append(got, fmt.Sprintf("#%d %s", f.Check, f.Status))
			}
		}
		return strings.Join(got, ", ")
	}
	if got := checksAt(due); got != "#11 warn, #12 warn" {
		t.Errorf("Checks with a report waiting: %q", got)
	}
	if got := checksAt(due.AddDate(0, 0, 31)); got != "#11 warn, #12 fail" {
		t.Errorf("Checks with a report overdue: %q", got)
	}
	id := mustID(t, l[0].ID)
	if err := svc.Review(ctx, c.sess(c.owner), id); err != nil {
		t.Fatal(err)
	}
	if err := svc.Review(ctx, c.sess(c.owner), id); err == nil {
		t.Error("a report reviewed twice")
	}
	if got := checksAt(due.AddDate(0, 0, 31)); got != "" {
		t.Errorf("Checks after the review: %q", got)
	}
	if l, _ = svc.List(ctx, c.sess(c.owner)); l[0].State != "reviewed" || l[0].ReviewedBy != "Owen Owner" {
		t.Errorf("%+v", l[0])
	}
	if del, _, err := worker.RunCandidateRetention(ctx, c.org, due); err != nil || del != 1 {
		t.Fatalf("%d %v", del, err)
	}
	if l, _ = svc.List(ctx, c.sess(c.owner)); l[0].State != "deleted" {
		t.Errorf("%+v", l[0])
	}
}
