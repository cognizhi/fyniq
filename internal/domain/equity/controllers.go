package equity

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// overAQuarter says whether part is more than 25% of whole, exactly: part × 4 > whole (FR-EQ-25's "more than 25%").
func overAQuarter(part, whole *apd.Decimal) (bool, error) {
	if whole.Sign() <= 0 {
		return false, nil
	}
	var q apd.Decimal
	if _, err := exact.Mul(&q, part, apd.New(4, 0)); err != nil {
		return false, err
	}
	return q.Cmp(whole) > 0, nil
}

// ControllerFinding is a holder the controller test names on a day (FR-EQ-25, design D10): over 25% of the shares
// issued or of the votes, from the first day of the run they have been over it, with their percentages that day; or a
// holder whose recorded rights could confer control whatever their shares, whom only a person's conclusion settles.
type ControllerFinding struct {
	HolderID          uuid.UUID
	Shares, Votes     bool
	Ownership, Voting string // RND-DISPLAY
	Since             time.Time
	Rights            bool
}

// Over says whether the cap table alone makes the holder a controller.
func (f ControllerFinding) Over() bool { return f.Shares || f.Votes }

// ControllerTest is the cap table's part of the controller test at the end of day (FR-EQ-25): read on each day the
// register moved up to it, a holder over 25% of the shares issued or of the votes is named from the first day of their
// latest run over it. Each holder whose rights in force could confer control (InvestorRights.ConfersControl) is named
// too, as the cap table cannot settle it (G4-101). Findings are by holder ID.
func ControllerTest(day time.Time, r Reader, rights map[uuid.UUID]InvestorRights) ([]ControllerFinding, error) {
	var days []time.Time
	for _, m := range r.Moves {
		if !m.On.After(day) && !slices.ContainsFunc(days, func(d time.Time) bool { return d.Equal(m.On) }) {
			days = append(days, m.On)
		}
	}
	slices.SortFunc(days, func(a, b time.Time) int { return a.Compare(b) })
	if len(days) == 0 || !days[len(days)-1].Equal(day) {
		days = append(days, day)
	}
	since := map[uuid.UUID]time.Time{}
	var last CapTable
	for _, d := range days {
		t, err := r.At(d)
		if err != nil {
			return nil, err
		}
		for _, h := range t.Holders {
			shares, err := overAQuarter(&h.Issued, &t.Issued)
			if err != nil {
				return nil, err
			}
			votes, err := overAQuarter(&h.Votes, &t.Votes)
			if err != nil {
				return nil, err
			}
			if _, ok := since[h.Holder]; (shares || votes) && !ok {
				since[h.Holder] = d
			} else if !shares && !votes {
				delete(since, h.Holder)
			}
		}
		for id := range since {
			if !slices.ContainsFunc(t.Holders, func(h CapHolder) bool { return h.Holder == id }) {
				delete(since, id)
			}
		}
		last = t
	}
	var out []ControllerFinding
	for _, h := range last.Holders {
		f := ControllerFinding{HolderID: h.Holder, Ownership: h.Ownership, Voting: h.Voting}
		if s, ok := since[h.Holder]; ok {
			f.Since = s
			var err error
			if f.Shares, err = overAQuarter(&h.Issued, &last.Issued); err != nil {
				return nil, err
			}
			if f.Votes, err = overAQuarter(&h.Votes, &last.Votes); err != nil {
				return nil, err
			}
		}
		if x, ok := rights[h.Holder]; ok && x.ConfersControl() {
			f.Rights = true
		}
		if f.Over() || f.Rights {
			out = append(out, f)
		}
	}
	for id, x := range rights {
		if x.ConfersControl() && !slices.ContainsFunc(out, func(f ControllerFinding) bool { return f.HolderID == id }) {
			out = append(out, ControllerFinding{HolderID: id, Rights: true})
		}
	}
	slices.SortFunc(out, func(a, b ControllerFinding) int { return strings.Compare(a.HolderID.String(), b.HolderID.String()) })
	return out, nil
}

// MaxExemptMembers is the most members an exempt private company has (Companies Act s4(1)) [accountant to confirm].
const MaxExemptMembers = 20

// ExemptFinding is the register's part of the exempt-private-company test on a day (FR-EQ-25, FR-EQ-11; design D10):
// its members (holders of shares issued), those recorded as entities, those with no holder record to say, and whether
// it is decided.
type ExemptFinding struct {
	Members  int
	Entities []uuid.UUID
	Unknown  []uuid.UUID
	Decided  bool
	Exempt   bool
}

// ExemptTest tests the members on the cap table t: more than 20 members, or a member whose holder record in force says
// entity, ends exempt status; with neither, the company is exempt once every member's record says individual, and
// undecided while one has none. kinds are the holder records in force by holder: individual or entity. The Act's finer
// points, such as a corporation's beneficial interest held through a nominee, are not tested [accountant to confirm].
func ExemptTest(t CapTable, kinds map[uuid.UUID]string) ExemptFinding {
	var f ExemptFinding
	for _, h := range t.Holders {
		if h.Issued.Sign() <= 0 {
			continue
		}
		f.Members++
		switch kinds[h.Holder] {
		case "entity":
			f.Entities = append(f.Entities, h.Holder)
		case "individual":
		default:
			f.Unknown = append(f.Unknown, h.Holder)
		}
	}
	switch {
	case f.Members > MaxExemptMembers || len(f.Entities) > 0:
		f.Decided, f.Exempt = true, false
	case len(f.Unknown) == 0:
		f.Decided, f.Exempt = true, true
	}
	return f
}

// BusinessDaysAfter is the day n weekdays after day. Singapore's public holidays are not counted out yet, so the day it
// gives is never later than the law's [verify: a public-holiday calendar].
func BusinessDaysAfter(day time.Time, n int) time.Time {
	d := day
	for n > 0 {
		d = d.AddDate(0, 0, 1)
		if wd := d.Weekday(); wd != time.Saturday && wd != time.Sunday {
			n--
		}
	}
	return d
}

// The register's days (FR-EQ-25; ACRA's registrable-controllers guidance [verify against the guidance in force]): the
// company's register within 7 days, ACRA's central register within 2 business days after it, a notice to each
// controller at least once a year, and a reply within 30 days.
const (
	CompanyRegisterDays = 7
	CentralRegisterDays = 2
	ReplyDays           = 30
)

// The standings of a holder the controller test names.
const (
	StandingToEnter       = "to_enter"         // not yet in the company's register
	StandingToLodge       = "to_lodge"         // entered; ACRA's central register not updated yet
	StandingRegistered    = "registered"       // in both
	StandingNotAssessed   = "not_assessed"     // rights to weigh and no conclusion recorded (G4-101)
	StandingNotController = "not_a_controller" // concluded not to be one
)

// ControllerStanding is a holder the controller test names, with the entry in force and where the register stands:
// what is due and when, the yearly notice due, and a notice unanswered past its 30 days.
type ControllerStanding struct {
	ControllerFinding
	Entry              *Recorded[ControllerEntry]
	Standing           string
	CompanyRegisterDue *time.Time
	CentralRegisterDue *time.Time
	NoticeDue          *time.Time
	Unanswered         *ControllerNotice
}

// ExemptStanding is the exempt test against the status recorded in force (FR-EQ-11): Differs when the register decides
// otherwise.
type ExemptStanding struct {
	Finding  ExemptFinding
	Recorded *director.RecordedStatus
	Differs  bool
}

// StatutoryTests are the cap table's statutory tests on a day (FR-EQ-25): the holders the controller test names, the
// entries in force for controllers it no longer names (to record their ceasing), and the exempt test.
type StatutoryTests struct {
	Controllers []ControllerStanding
	Lapsed      []Recorded[ControllerEntry]
	Exempt      ExemptStanding
}

// standingOf is where the register stands for a finding on today, with the entry in force (nil, or one ceased by today,
// counting as none).
func standingOf(f ControllerFinding, e *Recorded[ControllerEntry], today time.Time) ControllerStanding {
	out := ControllerStanding{ControllerFinding: f, Entry: e}
	if e != nil && e.Value.Ceased != nil && !e.Value.Ceased.After(today) {
		e = nil
	}
	became := f.Since
	switch {
	case e == nil && !f.Over():
		out.Standing = StandingNotAssessed
		return out
	case e != nil && !e.Value.Controller && !f.Over():
		out.Standing = StandingNotController
		return out
	case e != nil && e.Value.Controller && !f.Over():
		became = e.Value.Since
	}
	if e == nil || !e.Value.Controller || e.Value.CompanyRegister == nil {
		due := became.AddDate(0, 0, CompanyRegisterDays)
		out.Standing, out.CompanyRegisterDue = StandingToEnter, &due
		return out
	}
	if e.Value.CentralRegister == nil {
		due := BusinessDaysAfter(*e.Value.CompanyRegister, CentralRegisterDays)
		out.Standing, out.CentralRegisterDue = StandingToLodge, &due
		return out
	}
	out.Standing = StandingRegistered
	last := *e.Value.CompanyRegister
	for i, n := range e.Value.Notices {
		last = n.Sent
		if n.Replied == nil && n.Sent.AddDate(0, 0, ReplyDays).Before(today) {
			out.Unanswered = &e.Value.Notices[i]
		}
	}
	due := last.AddDate(1, 0, 0)
	out.NoticeDue = &due
	return out
}

// StatutoryTestsIn are the cap table's statutory tests in u at the end of today: the controller test over the rights in
// force, each finding's standing against the controller entry in force, the lapsed entries, and the exempt test over the
// holder records in force against the status recorded.
func StatutoryTestsIn(ctx context.Context, u storage.UnitOfWork, today time.Time) (StatutoryTests, error) {
	var out StatutoryTests
	r, err := ReaderIn(ctx, u)
	if err != nil {
		return out, err
	}
	allRights, err := RightsOf(ctx, u)
	if err != nil {
		return out, err
	}
	rights := map[uuid.UUID]InvestorRights{}
	for id, versions := range allRights {
		if cur := InForce(versions); cur != nil {
			rights[id] = cur.Value
		}
	}
	findings, err := ControllerTest(today, r, rights)
	if err != nil {
		return out, err
	}
	entries, err := ControllersOf(ctx, u)
	if err != nil {
		return out, err
	}
	for _, f := range findings {
		out.Controllers = append(out.Controllers, standingOf(f, InForce(entries[f.HolderID]), today))
	}
	for id, versions := range entries {
		cur := InForce(versions)
		if cur == nil || !cur.Value.Controller || cur.Value.Ceased != nil && !cur.Value.Ceased.After(today) {
			continue
		}
		if !slices.ContainsFunc(findings, func(f ControllerFinding) bool { return f.HolderID == id }) {
			out.Lapsed = append(out.Lapsed, *cur)
		}
	}
	slices.SortFunc(out.Lapsed, func(a, b Recorded[ControllerEntry]) int {
		return strings.Compare(a.Value.HolderID.String(), b.Value.HolderID.String())
	})
	table, err := r.At(today)
	if err != nil {
		return out, err
	}
	records, err := HolderRecords(ctx, u)
	if err != nil {
		return out, err
	}
	kinds := map[uuid.UUID]string{}
	for id, versions := range records {
		if cur := InForce(versions); cur != nil {
			kinds[id] = cur.Value.Kind
		}
	}
	out.Exempt.Finding = ExemptTest(table, kinds)
	statuses, err := director.ExemptStatuses(ctx, u)
	if err != nil {
		return out, err
	}
	out.Exempt.Recorded = director.ExemptOn(statuses, today)
	out.Exempt.Differs = out.Exempt.Finding.Decided && out.Exempt.Recorded != nil && out.Exempt.Recorded.Status.Exempt != out.Exempt.Finding.Exempt
	return out, nil
}
