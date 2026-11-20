//go:build local

package nemura_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/hiring"
)

// A change of occupation, salary, vacancies or UEN needs a new advert: the old one is archived as evidence, the new day starts the 14 days again, a renewal needs a later day and a post with an advert,
// and an advert closed more than 3 months ago cannot be used (NM-REC-02).
// req: NM-REC-02
// asvs: V8.3.1
func TestANewAdvertAfterAMaterialChangeArchivesTheOldAndAnAdvertClosedOver3MonthsAgoIsRefused(t *testing.T) {
	c := newCompany(t)
	now := today.Add(11 * time.Hour)
	in := api.RequisitionInput{Title: "Analyst", Department: "Finance", Reason: "new", SalaryMin: "5000.00", SalaryMax: "6000.00", TargetStart: "2099-06-01", PassNeeded: true}
	var req uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		if req, err = nemura.RequestRequisition(ctx, tx, c.org, now, c.hr, in); err != nil {
			return
		}
		if err = nemura.DecideRequisition(ctx, tx, c.org, now, c.owner, req, true, ""); err != nil {
			return
		}
		if err = nemura.WriteJobPost(ctx, tx, c.org, now, c.hr, req, "Analyst", "You will analyse the books."); err != nil {
			return
		}
		return nemura.PublishJobPost(ctx, tx, c.org, now, c.hr, req, "")
	})
	view := func() api.JobPost {
		var r api.Requisition
		c.must(func(ctx context.Context, tx pgx.Tx) (err error) { r, err = nemura.OneRequisition(ctx, tx, req); return })
		return *r.JobPost
	}
	renew := func(change, day string) error {
		return c.do(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.RenewJobAdvert(ctx, tx, c.org, now, c.hr, req, api.AdvertInput{Change: change, PostedOn: day, Reference: "MCF-NEW", Note: "The top of the range rose."})
		})
	}
	if err := renew("salary", "2099-02-20"); hireCode(err) != hiring.CodeConflict {
		t.Errorf("a renewal of a post with no advert: %v", err)
	}
	// An advert that came down more than 3 months ago cannot be used: "now" is 2099-03-01's day in the test clock's terms, so one closed on 2098-11-01 is stale.
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.RecordJobAdvert(ctx, tx, c.org, now, c.hr, req, api.AdvertInput{PostedOn: "2098-10-01", ClosedOn: "2098-10-20"})
	}); hireCode(err) != hiring.CodeInvalid {
		t.Errorf("an advert closed over 3 months ago: %v", err)
	}
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.RecordJobAdvert(ctx, tx, c.org, now, c.hr, req, api.AdvertInput{PostedOn: "2099-01-05", ClosedOn: "2099-01-02"})
	}); hireCode(err) != hiring.CodeInvalid {
		t.Errorf("an advert that came down before it went up: %v", err)
	}
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.RecordJobAdvert(ctx, tx, c.org, now, c.hr, req, api.AdvertInput{PostedOn: "2099-02-01", ClosedOn: "2099-02-15", Reference: "MCF-OLD"})
	}); err != nil {
		t.Fatal(err)
	}
	if p := view(); p.AdvertClosedOn != "2099-02-15" || p.AdvertUsableUntil != "2099-05-15" || p.AdvertRenewals != 0 {
		t.Errorf("%+v", p)
	}
	if err := renew("colour", "2099-02-20"); hireCode(err) != hiring.CodeInvalid {
		t.Errorf("a change that is not one of the four: %v", err)
	}
	if err := renew("salary", "2099-01-20"); hireCode(err) != hiring.CodeInvalid {
		t.Errorf("a new advert dated before the old one: %v", err)
	}
	if err := renew("salary", "2099-02-20"); err != nil {
		t.Fatal(err)
	}
	p := view()
	if p.AdvertPostedOn != "2099-02-20" || p.AdvertReference != "MCF-NEW" || p.AdvertRenewals != 1 || p.AdvertClosedOn != "" || p.OfferFrom != "2099-03-06" {
		t.Errorf("the new advert starts the 14 days again: %+v", p)
	}
	var kept int
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM nemura.job_advert WHERE change_kind = 'salary' AND posted_on = '2099-02-01' AND reference = 'MCF-OLD' AND closed_on = '2099-02-15'`).Scan(&kept)
	})
	if kept != 1 {
		t.Errorf("the replaced advert is kept as evidence: %d rows", kept)
	}
	// Straight to the database: a replaced advert never changes, the date is not rewritten without a renewal, and a renewal is counted only with a later day.
	for name, q := range map[string]string{
		"an archived advert changed":      `UPDATE nemura.job_advert SET reference = 'x'`,
		"an archived advert deleted":      `DELETE FROM nemura.job_advert`,
		"the advert date rewritten":       `UPDATE nemura.job_post SET advert_posted_on = '2099-02-25'`,
		"a renewal counted without a day": `UPDATE nemura.job_post SET advert_renewals = advert_renewals + 1`,
	} {
		if err := c.do(func(ctx context.Context, tx pgx.Tx) error { _, err := tx.Exec(ctx, q); return err }); pgErr(err) != "NM213" && pgErr(err) != "42501" { // the trigger refuses a change; the grant already gives no UPDATE or DELETE on the archive
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The careers page lists only published, approved posts of a page that is switched on, with no salary and no person; another company cannot take an address, and a page that is off, unknown or another
// company's shows nothing of ours (NM-REC-02).
// req: NM-REC-02
// asvs: V8.3.1, V4.2.1
func TestTheCareersPageListsOnlyPublishedPostsOfAPageThatIsOnAndAddressesAreNotShared(t *testing.T) {
	c := newCompany(t)
	other := newCompany(t)
	now := today.Add(11 * time.Hour)
	svc := nemura.CareersService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(now)}
	sess := func(co *company, m uuid.UUID) api.Session { return api.Session{Organisation: co.org, Member: m} }
	slug := "acme-" + c.org.String()[:8]
	if _, err := svc.Save(context.Background(), sess(c, c.hr), api.CareersSettings{Slug: "Bad_Slug", Company: "Acme"}); hireCode(err) != hiring.CodeInvalid {
		t.Errorf("a malformed address: %v", err)
	}
	// Two requisitions: one published and approved, one only a draft.
	in := api.RequisitionInput{Title: "Designer", Department: "Product", Reason: "new", SalaryMin: "5000.00", SalaryMax: "6000.00", TargetStart: "2099-06-01"}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		a, err := nemura.RequestRequisition(ctx, tx, c.org, now, c.hr, in)
		if err != nil {
			return err
		}
		b, err := nemura.RequestRequisition(ctx, tx, c.org, now, c.hr, in)
		if err != nil {
			return err
		}
		for _, r := range []uuid.UUID{a, b} {
			if err := nemura.DecideRequisition(ctx, tx, c.org, now, c.owner, r, true, ""); err != nil {
				return err
			}
		}
		if err := nemura.WriteJobPost(ctx, tx, c.org, now, c.hr, a, "Designer", "You will design."); err != nil {
			return err
		}
		if err := nemura.PublishJobPost(ctx, tx, c.org, now, c.hr, a, ""); err != nil {
			return err
		}
		return nemura.WriteJobPost(ctx, tx, c.org, now, c.hr, b, "Secret draft", "Not yet.")
	})
	read := func() api.CareersView {
		v, err := svc.Public(context.Background(), slug)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := read(); v.Found || len(v.Posts) != 0 {
		t.Errorf("a page that was never set up: %+v", v)
	}
	if _, err := svc.Save(context.Background(), sess(c, c.hr), api.CareersSettings{Slug: slug, Enabled: false, Company: "Acme Pte. Ltd.", Intro: "Come and work with us."}); err != nil {
		t.Fatal(err)
	}
	if v := read(); v.Found || len(v.Posts) != 0 {
		t.Errorf("a page that is switched off: %+v", v)
	}
	if _, err := svc.Save(context.Background(), sess(c, c.hr), api.CareersSettings{Slug: slug, Enabled: true, Company: "Acme Pte. Ltd.", Intro: "Come and work with us."}); err != nil {
		t.Fatal(err)
	}
	v := read()
	if !v.Found || v.Company != "Acme Pte. Ltd." || v.Intro != "Come and work with us." || len(v.Posts) != 1 || v.Posts[0].Title != "Designer" || v.Posts[0].Department != "Product" {
		t.Errorf("only the published post is listed: %+v", v)
	}
	if _, err := svc.Save(context.Background(), sess(other, other.hr), api.CareersSettings{Slug: slug, Enabled: true, Company: "Rival"}); hireCode(err) != hiring.CodeConflict {
		t.Errorf("another company takes the address: %v", err)
	}
	if got, err := svc.Settings(context.Background(), sess(other, other.hr)); err != nil || got.Configured {
		t.Errorf("another company reads our page's settings: %+v %v", got, err)
	}
}
