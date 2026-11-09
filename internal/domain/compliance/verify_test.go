package compliance

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	registry "github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Every verification the registry runs is built, every one built is declared in the registry, and each built one is
// named by a running check (design D4).
// req: CMP-01
func TestEveryRunningVerificationIsBuilt(t *testing.T) {
	controls, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]bool{}
	for _, c := range controls {
		for _, ch := range c.Checks {
			if kind, name := ch.Kind(); kind == registry.KindVerify && ch.Status == "running" {
				named[name] = true
				if !Built(name) {
					t.Errorf("%s runs verify %s, which is not built", c.ID, name)
				}
			}
		}
	}
	for name := range verifications {
		if _, ok := registry.Verifications[name]; !ok {
			t.Errorf("verify %s is built but not declared in registry.Verifications", name)
		}
		if !named[name] {
			t.Errorf("verify %s is built but no running check of the registry names it", name)
		}
	}
}

// A run records what it found: nothing, or how many, the first in words, and the first 50 items (design D4).
// req: CMP-01
func TestARunSaysWhatItFound(t *testing.T) {
	now := time.Date(2099, 1, 2, 19, 0, 0, 0, time.UTC)
	ch := registry.Check{Frequency: "N", Status: "running", How: "verify payments.dual-control"}
	run, err := runOf("CTL-FIN-08", ch, nil, "2 approval(s) and 1 release(s)", now)
	if err != nil || !run.OK || run.Count != 0 || string(run.Items) != "[]" ||
		run.Detail != "Nothing found in 2 approval(s) and 1 release(s) of the last day" || !run.Since.Equal(now.Add(-Window)) ||
		!run.RanAt.Equal(now) || run.Control != "CTL-FIN-08" || run.Frequency != "N" || run.Check != ch.How {
		t.Errorf("a clean run: %+v %v", run, err)
	}
	found := make([]Finding, 60)
	for i := range found {
		found[i] = Finding{Kind: "payment_batch", ID: uuid.NewString(), Detail: "Released under neither an approval nor a break-glass"}
	}
	run, err = runOf("CTL-FIN-08", ch, found, "60 release(s)", now)
	if err != nil || run.OK || run.Count != 60 ||
		run.Detail != "60 found in 60 release(s) of the last day: Released under neither an approval nor a break-glass, and more" {
		t.Errorf("a run that found 60: %+v %v", run, err)
	}
	var items []Finding
	if err := json.Unmarshal(run.Items, &items); err != nil || len(items) != 50 || items[0] != found[0] {
		t.Errorf("the items kept: %d, %v", len(items), err)
	}
}

// What breaks dual control (CTL-FIN-08, FM-1, FM-8): an approval nobody signed, or that its preparer or requester
// signed; a release under such an approval, under a break-glass its Owner did not use for the batch, or under neither.
// req: CTL-FIN-08
// req: FM-1
// asvs: V2.3.5
func TestWhatBreaksDualControl(t *testing.T) {
	at := time.Date(2099, 1, 2, 3, 0, 0, 0, time.UTC)
	approval, glass := uuid.New(), uuid.New()
	approvals := []storage.DualControlApproval{
		{ObjectType: "payee_bank_change", ObjectKey: "own", DecidedAt: at, Signatures: 1, OwnSignatures: 1},
		{ObjectType: "payment_batch", ObjectKey: "nobody", DecidedAt: at},
		{ObjectType: "claimant_bank_change", ObjectKey: "another", DecidedAt: at, Signatures: 1},
	}
	releases := []storage.DualControlRelease{
		{BatchID: uuid.New(), ReleasedAt: at, ApprovalID: &approval, Approved: true, Signatures: 1},                   // another's
		{BatchID: uuid.New(), ReleasedAt: at, ApprovalID: &approval, Approved: true, Signatures: 2, OwnSignatures: 1}, // its preparer's
		{BatchID: uuid.New(), ReleasedAt: at, ApprovalID: &approval, Approved: false, Signatures: 1},                  // not approved
		{BatchID: uuid.New(), ReleasedAt: at, BreakGlassID: &glass, BreakGlassUsed: true},                             // FM-8
		{BatchID: uuid.New(), ReleasedAt: at, BreakGlassID: &glass},                                                   // not its use
		{BatchID: uuid.New(), ReleasedAt: at},                                                                         // neither
	}
	got := dualControlFindings(approvals, releases)
	want := []Finding{
		{Kind: "payee_bank_change", ID: "own", At: day(at), Detail: "Approved with the signature of whoever prepared it or asked for it"},
		{Kind: "payment_batch", ID: "nobody", At: day(at), Detail: "Approved with no signature"},
		{Kind: "payment_batch", ID: releases[1].BatchID.String(), At: day(at), Detail: "Released under an approval that is not another person's"},
		{Kind: "payment_batch", ID: releases[2].BatchID.String(), At: day(at), Detail: "Released under an approval that is not another person's"},
		{Kind: "payment_batch", ID: releases[4].BatchID.String(), At: day(at), Detail: "Released under a break-glass its Owner did not use for this batch"},
		{Kind: "payment_batch", ID: releases[5].BatchID.String(), At: day(at), Detail: "Released under neither an approval nor a break-glass"},
	}
	if len(got) != len(want) {
		t.Fatalf("found %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("finding %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

// What breaks the second factor (CTL-IAM-01, SEC-01): a member with neither a passkey nor a sign-in that passed one, and
// a member seen in a session that passed none.
// req: CTL-IAM-01
// req: SEC-01
// asvs: V6.3.3
func TestWhatBreaksTheSecondFactor(t *testing.T) {
	at := time.Date(2099, 1, 2, 3, 0, 0, 0, time.UTC)
	none, seen, passkey, signIn := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	got := secondFactorFindings([]storage.MemberSecondFactor{
		{UserID: none},
		{UserID: seen, SessionsWith: 3, SessionsWithout: 2, LastWithout: &at},
		{UserID: passkey, Passkeys: 1},
		{UserID: signIn, SessionsWith: 1},
	})
	want := []Finding{
		{Kind: "member", ID: none.String(), Detail: "Has no second factor: no passkey, and no sign-in that passed one"},
		{Kind: "member", ID: seen.String(), At: day(at), Detail: "Seen in 2 session(s) that passed no second factor"},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("found %+v, want %+v", got, want)
	}
}
