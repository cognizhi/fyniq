package notify

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func member(user uuid.UUID, status string, roles ...string) storage.Member {
	m := storage.Member{Membership: storage.Membership{UserID: user, Status: status}}
	for _, r := range roles {
		m.Roles = append(m.Roles, storage.RoleAssignment{Role: r})
	}
	return m
}

// An event concerns the users it names and the active members whose roles hold its right on its row, except those it
// leaves out: a journal's approval waits for the Controller, not the Owner, who only views journals (Appendix D), the
// Accountant who asked, or a removed member. Any of several rights will do: C or A on users and roles is the
// Administrator and the Owner.
// req: FR-NTF-01
// req: FR-NTF-05
func TestAnEventConcernsItsAudience(t *testing.T) {
	owner, controller, accountant, gone, named := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	members := []storage.Member{member(owner, "active", "owner"), member(controller, "active", "controller"), member(accountant, "active", "accountant", "controller"),
		member(gone, "removed", "owner"), member(named, "active", "employee")}
	got := Recipients(Audience{Users: []uuid.UUID{named}, Capability: "Journals", Need: "A", Except: []uuid.UUID{accountant}}, members)
	if len(got) != 2 || got[0] != controller || got[1] != named {
		t.Fatalf("recipients %v", got)
	}
	admin := uuid.New()
	members = append(members, member(admin, "active", "administrator"))
	if got := Recipients(Audience{Capability: "Users, roles, team mode", Need: "CA"}, members); len(got) != 2 || got[0] != owner || got[1] != admin {
		t.Fatalf("any of the rights: %v", got)
	}
}

// A type goes by email as the person chose, else as recommended; a security type as it says, whatever they chose: a
// role change at once, a sign-in never, since the sign-in alert emails it.
// req: FR-NTF-02
func TestAPersonsCadence(t *testing.T) {
	approvals, _ := Lookup("approval.requested")
	roles, _ := Lookup("security.roles")
	signIn, _ := Lookup("security.sign_in")
	prefs := []storage.NotificationPreference{{Type: "approval.requested", Email: storage.EmailDaily}, {Type: "security.roles", Email: storage.EmailOff},
		{Type: "security.sign_in", Email: storage.EmailImmediate}}
	if Cadence(approvals, nil) != storage.EmailImmediate || Cadence(approvals, prefs) != storage.EmailDaily || Cadence(roles, prefs) != storage.EmailImmediate ||
		Cadence(signIn, prefs) != storage.EmailOff {
		t.Fatal("cadences")
	}
}

// Security and approvals go by email at once, everything else in the daily digest, as the owner decided, but a test
// notification, whose point is checking the email, at once; every type
// names a known category and severity, and a locked type is a security one.
// req: FR-NTF-02
func TestTheCatalogueFollowsTheOwnersDecisions(t *testing.T) {
	for _, ty := range Catalogue {
		switch {
		case ty.Category == Approvals && ty.Email != storage.EmailImmediate,
			ty.Category == Security && ty.Email != storage.EmailImmediate && ty.Note == "",
			ty.Category != Security && ty.Category != Approvals && ty.Key != TestType && ty.Email != storage.EmailDaily,
			ty.Key == TestType && ty.Email != storage.EmailImmediate,
			ty.Locked != (ty.Category == Security),
			!slices.Contains([]string{"info", "warning", "critical"}, ty.Severity):
			t.Errorf("%s: %+v", ty.Key, ty)
		}
	}
}

// Quiet hours are Singapore time and may run past midnight.
// req: FR-NTF-02
func TestQuietHours(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 24, h, m, 0, 0, appclock.Singapore) }
	from, to := 22*60, 7*60
	prefs := []storage.NotificationPreference{{Type: storage.QuietHours, QuietFrom: &from, QuietTo: &to}}
	for _, c := range []struct {
		h, m  int
		quiet bool
	}{{23, 0, true}, {6, 59, true}, {7, 0, false}, {12, 0, false}, {22, 0, true}} {
		if Quiet(prefs, at(c.h, c.m)) != c.quiet {
			t.Errorf("%02d:%02d quiet=%v", c.h, c.m, !c.quiet)
		}
	}
	day, end := 9*60, 17*60
	if !Quiet([]storage.NotificationPreference{{Type: storage.QuietHours, QuietFrom: &day, QuietTo: &end}}, at(12, 0)) || Quiet(nil, at(23, 0)) {
		t.Error("daytime hours, or none")
	}
}

// The daily digest carries what came before today's 08:00 Singapore time, once it has passed; the weekly, what came
// before this Monday's 08:00; an immediate email held in quiet hours is due at once.
// req: FR-NTF-02
func TestWhenADigestIsDue(t *testing.T) {
	sgt := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, appclock.Singapore) } // 26 Oct 2026 is a Monday
	for _, c := range []struct {
		cadence      string
		created, now time.Time
		want         bool
	}{
		{storage.EmailDaily, sgt(23, 20), sgt(24, 7), false},
		{storage.EmailDaily, sgt(23, 20), sgt(24, 8), true},
		{storage.EmailDaily, sgt(24, 9), sgt(24, 10), false},
		{storage.EmailWeekly, sgt(24, 9), sgt(25, 9), false},
		{storage.EmailWeekly, sgt(24, 9), sgt(26, 8), true},
		{storage.EmailWeekly, sgt(26, 9), sgt(27, 9), false},
		{storage.EmailImmediate, sgt(24, 23), sgt(24, 23), true},
	} {
		if got := due(c.cadence, c.created, c.now); got != c.want {
			t.Errorf("%s made %s, at %s: %v", c.cadence, c.created, c.now, got)
		}
	}
}
