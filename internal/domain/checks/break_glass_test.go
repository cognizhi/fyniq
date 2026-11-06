//go:build local

package checks_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/checks"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// owner joins b's organisation as an Owner, by invitation, and returns their user.
func (b books) owner(t *testing.T, n byte) uuid.UUID {
	t.Helper()
	hash := make([]byte, 32)
	hash[0], hash[1], hash[2] = 53, n, b.org[15]
	if err := b.api.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.MemberInvitations().Create(ctx, storage.NewMemberInvitation{CodeHash: hash, DisplayName: "Founder", Roles: []string{"owner"},
			ExpiresAt: now.Add(time.Hour), CreatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	j, err := b.api.JoinOrganisation(ctx, storage.NewJoin{CodeHash: hash, DisplayName: "Founder", Issuer: "urn:fyniq:checks", Subject: uuid.NewString(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return j.UserID
}

// C.2 #53: a break-glass still unreviewed 7 days after it ended is ✖, which refuses the close (FM-4); its review by
// another Owner clears it. One whose review is not due yet is fine (FM-8; the owner's decision of 6 Nov 2026).
// req: C.2 #53
// req: FM-8
// req: FM-4
// req: controls/platform-checks Break-glass not reviewed in time
// req: controls/platform-checks Break-glass reviewed
// req: access/team-modes Unreviewed break-glass
func TestAnUnreviewedBreakGlassRefusesTheClose(t *testing.T) {
	b := newBooks(t, true)
	weiLing, ahKow := b.owner(t, 1), b.owner(t, 2)
	const key = "controls.break-glass-unreviewed"
	if s := want(t, b.run(t), key, checks.OK); s.Detail != "No break-glass was used" {
		t.Errorf("with none: %q", s.Detail)
	}

	// Started on 1 Nov, it expired on 2 Nov, and its review fell due on 9 Nov: on 10 Nov it is overdue.
	started := now.Add(-9 * 24 * time.Hour)
	var g storage.BreakGlass
	var entry uuid.UUID
	if err := b.api.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if g, err = u.BreakGlasses().Start(ctx, storage.NewBreakGlass{StartedBy: weiLing, Reason: "Ah Kow is abroad; the rent is due",
			StartedAt: started, Currency: "SGD", Cap: "20000.00"}); err != nil {
			return err
		}
		entry, err = u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewBreakGlass, ObjectType: "break_glass", ObjectKey: g.ID.String(),
			Summary: "Wei Ling started break-glass", Actor: &weiLing, LoggedAt: started})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s := want(t, b.run(t), key, checks.Error)
	if items := itemsOf(t, s); len(items) != 1 || items[0].Kind != "break_glass" || items[0].ID != g.ID.String() {
		t.Errorf("the items: %+v", items)
	}
	var more []closing.Check
	if err := b.api.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		more, err = checks.ForClose(ctx, u, b.oct, porttest.Day(11, 10))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(more, func(c closing.Check) bool { return c.Key == "checks."+key && c.Severity == closing.Failed }) {
		t.Errorf("the close is not refused: %+v", more)
	}

	if err := b.api.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.ReviewLog().Review(ctx, entry, ahKow, now, "Checked the payments against the bills")
	}); err != nil {
		t.Fatal(err)
	}
	want(t, b.run(t), key, checks.OK)

	// A second one, ended an hour ago, is not due for its review yet.
	if err := b.api.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) error {
		at := now.Add(-25 * time.Hour)
		g, err := u.BreakGlasses().Start(ctx, storage.NewBreakGlass{StartedBy: ahKow, Reason: "Wei Ling is ill", StartedAt: at, Currency: "SGD", Cap: "20000.00"})
		if err != nil {
			return err
		}
		_, err = u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewBreakGlass, ObjectType: "break_glass", ObjectKey: g.ID.String(),
			Summary: "Ah Kow started break-glass", Actor: &ahKow, LoggedAt: at})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if s := want(t, b.run(t), key, checks.OK); s.Detail != "Every break-glass is reviewed, or not due for review yet" {
		t.Errorf("not due yet: %q", s.Detail)
	}
}
