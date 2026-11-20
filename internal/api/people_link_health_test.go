//go:build local

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/linksend"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// fakeLog is Nemura's event log by identity as Fyniq reads it.
type fakeLog struct {
	epoch  int
	events []linksend.SyncEvent
	err    error
}

func (f *fakeLog) Sync(context.Context, storage.PeopleLinkAskKey, string) (linksend.SyncPage, error) {
	if f.err != nil {
		return linksend.SyncPage{}, f.err
	}
	var highest int64
	for _, e := range f.events {
		highest = max(highest, e.Sequence)
	}
	return linksend.SyncPage{Epoch: f.epoch, HighestSequence: highest, Events: f.events}, nil
}

// longLog is a log of sent events numbered 1 to highest, served in pages of 500 after a cursor.
type longLog struct{ highest int64 }

func (f longLog) Sync(_ context.Context, _ storage.PeopleLinkAskKey, cursor string) (linksend.SyncPage, error) {
	after, _ := strconv.ParseInt(cursor, 10, 64)
	page := linksend.SyncPage{Epoch: 1, HighestSequence: f.highest}
	for n := after + 1; n <= f.highest && len(page.Events) < 500; n++ {
		page.Events = append(page.Events, linksend.SyncEvent{Sequence: n, EventID: uuid.NewString(), Type: "people.person.upserted", ObjectType: "person", ObjectID: uuid.NewString(), ObjectVersion: 1, State: "sent"})
	}
	if n := len(page.Events); n == 500 && page.Events[n-1].Sequence < f.highest {
		page.NextCursor = strconv.FormatInt(page.Events[n-1].Sequence, 10)
	}
	return page, nil
}

// The link's health and its repair (PRD-001 FR-PPL-08, FR-PPL-15; PRD-002 NM-INT-04, NM-INT-10; owner's decision of 21 Nov 2026: link.sync.read): Fyniq sets Nemura's log by identity against its inbox and says what
// waits, fails or was refused and which runs of sequence numbers Nemura delivered and Fyniq never received; repairing asks Nemura, signed, for each run once (a repeat with the same key does nothing, a preview
// queues nothing, a run is asked for no wider than Nemura accepts); what cannot be read is said in plain words and never guessed (no link, a link not active, no ask key, Nemura not answering, another epoch);
// and only People and HR's right reads or repairs it.
// part of FR-PPL-08 (the 24-hour notice and the alert near pay day are open)
// part of NM-INT-10
// asvs: V8.2.1, V4.2.1, V13.2.1
func TestTheLinksHealthFindsAGapAndRepairingAsksNemuraForEachRunOnce(t *testing.T) {
	w := payrollWorld(t)
	e := w.e
	ctx := context.Background()
	// The world's link has no ask key yet: Fyniq cannot read Nemura's log and says so.
	health := func(tok string) response { return e.call(http.MethodGet, "/v1/people-link/health", tok, nil) }
	admin := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: w.org, Roles: []authz.Role{authz.Administrator}})
	if r := health(admin); r.Status != http.StatusOK || r.Body["reachable"] != false || r.str("note") == "" {
		t.Fatalf("no ask key: %d %s", r.Status, r.Raw)
	}
	if err := e.store.Do(ctx, w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		l, err := u.PeopleLinks().Live(ctx)
		if err != nil {
			return err
		}
		k, err := newAskKey(l.ID, "fyniq-ask-test")
		if err != nil {
			return err
		}
		return u.PeopleLinks().SaveAskKey(ctx, k)
	}); err != nil {
		t.Fatal(err)
	}
	log := &fakeLog{epoch: 1}
	e.api.deps.LinkLog = log
	ev := func(seq int64, state string) linksend.SyncEvent {
		return linksend.SyncEvent{Sequence: seq, EventID: uuid.NewString(), Type: "people.person.upserted", ObjectType: "person", ObjectID: uuid.NewString(), ObjectVersion: 1, State: state}
	}
	log.events = []linksend.SyncEvent{ev(1, "sent"), ev(2, "sent"), ev(3, "sent"), ev(4, "sent"), ev(5, "queued"), ev(6, "failed"), ev(7, "refused"), ev(8, "sent"), ev(9, "sent"), ev(10, "sent")}
	// Fyniq's inbox holds 1, 2, 3 and 8 (Nemura's sequence numbers; an event's version is its sequence here).
	who := peoplelink.Party{Issuer: "https://idp.example.test", Subject: "someone"}
	for _, seq := range []int{1, 2, 3, 8} {
		if r := w.nemuraSays(peoplelink.TypeAccessChanged, "identity", uuid.NewString(), seq, map[string]any{"issuer": who.Issuer, "subject": who.Subject, "people_access": true, "payroll_approver": false}, nil); r.Status != http.StatusOK {
			t.Fatalf("Nemura's event %d: %d %s", seq, r.Status, r.Raw)
		}
	}
	r := health(admin)
	var got PeopleLinkHealth
	if err := json.Unmarshal([]byte(r.Raw), &got); err != nil || r.Status != http.StatusOK {
		t.Fatalf("the health: %d %s", r.Status, r.Raw)
	}
	if !got.Reachable || got.NemuraHighestSequence != 10 || got.ReceivedHighestSequence != 8 || got.Waiting != 1 || got.Failing != 1 || got.Refused != 1 ||
		len(got.Missing) != 2 || got.Missing[0] != (SequenceRange{4, 4}) || got.Missing[1] != (SequenceRange{9, 10}) {
		t.Errorf("the health: %+v", got)
	}
	asked := func() (out []linksend.ReplayRequested) {
		for _, q := range w.queued(t) {
			if q.Type == linksend.TypeReplayRequested {
				var p linksend.ReplayRequested
				if json.Unmarshal(q.Payload, &p) != nil || q.ObjectType != "link" {
					t.Fatalf("a replay request: %+v", q)
				}
				out = append(out, p)
			}
		}
		return out
	}
	// Nothing is asked for until someone repairs; a preview shows the runs and queues none.
	if len(asked()) != 0 {
		t.Fatal("a replay was requested before anyone repaired")
	}
	if p := e.call(http.MethodPost, "/v1/people-link/repair", admin, nil, "Prefer", "return=preview"); p.Status != http.StatusOK || len(p.Body["requested"].([]any)) != 2 || len(asked()) != 0 {
		t.Errorf("the preview: %d %s (asked %d)", p.Status, p.Raw, len(asked()))
	}
	key := uuid.NewString()
	rep := e.call(http.MethodPost, "/v1/people-link/repair", admin, nil, "Idempotency-Key", key)
	if rep.Status != http.StatusOK || len(rep.Body["requested"].([]any)) != 2 {
		t.Fatalf("the repair: %d %s", rep.Status, rep.Raw)
	}
	if got := asked(); len(got) != 2 || got[0] != (linksend.ReplayRequested{FromSequence: 4, ToSequence: 4}) || got[1] != (linksend.ReplayRequested{FromSequence: 9, ToSequence: 10}) {
		t.Errorf("what Nemura was asked: %+v", got)
	}
	if again := e.call(http.MethodPost, "/v1/people-link/repair", admin, nil, "Idempotency-Key", key); again.Status != http.StatusOK || again.Header.Get("Idempotent-Replayed") != "true" || len(asked()) != 2 {
		t.Errorf("the same key again: %d %s (asked %d)", again.Status, again.Raw, len(asked()))
	}
	// Nemura answers a replay at new sequence numbers, so the numbers asked for never arrive: the health says they were asked for and not missing, and a second repair asks for nothing; after a day they are missing again.
	if after := health(admin); after.Status != http.StatusOK || len(after.Body["missing"].([]any)) != 0 || len(after.Body["requested"].([]any)) != 2 {
		t.Errorf("the health after the repair: %s", after.Raw)
	}
	if again := e.call(http.MethodPost, "/v1/people-link/repair", admin, nil, "Idempotency-Key", uuid.NewString()); again.Status != http.StatusOK || len(again.Body["requested"].([]any)) != 0 || len(asked()) != 2 {
		t.Errorf("a second repair of what was asked for: %d %s (asked %d)", again.Status, again.Raw, len(asked()))
	}
	e.clock.Set(e.clock.Now().Add(25 * time.Hour))
	if later := health(admin); len(later.Body["missing"].([]any)) != 2 || len(later.Body["requested"].([]any)) != 0 {
		t.Errorf("the health a day later: %s", later.Raw)
	}
	// Whatever cannot be read is said and never guessed, and asks for nothing.
	log.err = errors.New("down")
	if h := health(admin); h.Body["reachable"] != false || h.str("note") == "" {
		t.Errorf("Nemura not answering: %s", h.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/people-link/repair", admin, nil, "Idempotency-Key", uuid.NewString()); r.Status != http.StatusOK || len(r.Body["requested"].([]any)) != 0 || len(asked()) != 2 {
		t.Errorf("a repair while Nemura cannot be read: %d %s (asked %d)", r.Status, r.Raw, len(asked()))
	}
	log.err, log.epoch = nil, 2
	if h := health(admin); h.Body["reachable"] != false || h.str("note") == "" {
		t.Errorf("another epoch's log: %s", h.Raw)
	}
	e.api.deps.LinkLog = nil
	if h := health(admin); h.Body["reachable"] != false {
		t.Errorf("a deployment that does not know where Nemura is: %s", h.Raw)
	}
	// Who may: People and HR's right alone.
	for _, role := range []authz.Role{authz.Employee, authz.Bookkeeper, authz.Approver} {
		tok := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: w.org, Roles: []authz.Role{role}})
		if r := health(tok); r.Status != http.StatusForbidden {
			t.Errorf("%s read the link's health: %d", role, r.Status)
		}
		if r := e.call(http.MethodPost, "/v1/people-link/repair", tok, nil, "Idempotency-Key", uuid.NewString()); r.Status != http.StatusForbidden {
			t.Errorf("%s repaired the link: %d", role, r.Status)
		}
	}
	// A link's client reaches neither.
	if r := health(w.tok); r.Status != http.StatusForbidden && r.Status != http.StatusUnauthorized {
		t.Errorf("the link's client read the health: %d", r.Status)
	}
}

// A log longer than one look reads is read from a watermark, and the oldest part is the part left out (the health says truncated and the runs start after it).
// part of NM-INT-10
func TestAHealthLookReadsTheNewestPartOfALongLog(t *testing.T) {
	w := payrollWorld(t)
	e := w.e
	if err := e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		l, err := u.PeopleLinks().Live(ctx)
		if err != nil {
			return err
		}
		k, err := newAskKey(l.ID, "fyniq-ask-test")
		if err != nil {
			return err
		}
		return u.PeopleLinks().SaveAskKey(ctx, k)
	}); err != nil {
		t.Fatal(err)
	}
	e.api.deps.LinkLog = longLog{highest: 25000}
	admin := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: w.org, Roles: []authz.Role{authz.Administrator}})
	r := e.call(http.MethodGet, "/v1/people-link/health", admin, nil)
	var got PeopleLinkHealth
	if err := json.Unmarshal([]byte(r.Raw), &got); err != nil || !got.Reachable || !got.Truncated || got.NemuraHighestSequence != 25000 || len(got.Missing) != 2 || got.Missing[0].From != 5001 || got.Missing[1].To != 25000 {
		t.Errorf("the health of a long log: %d %+v", r.Status, got)
	}
}

// missingRuns are only what Nemura says it sent and the inbox lacks, in runs no wider than a replay request may ask for.
// part of NM-INT-10
func TestMissingRunsAreWhatWasSentAndNotReceivedInRunsOfAnAllowedWidth(t *testing.T) {
	if got := missingRuns([]int64{1, 2, 3, 5, 6}, []int64{1, 3}); len(got) != 2 || got[0] != (SequenceRange{2, 2}) || got[1] != (SequenceRange{5, 6}) {
		t.Errorf("runs: %+v", got)
	}
	if got := missingRuns(nil, []int64{1}); len(got) != 0 || got == nil {
		t.Errorf("nothing sent: %+v", got)
	}
	wide := make([]int64, 0, 2*linksend.MaxReplaySpan+5)
	for i := int64(1); i <= int64(2*linksend.MaxReplaySpan+5); i++ {
		wide = append(wide, i)
	}
	runs := missingRuns(wide, nil)
	if len(runs) != 3 || runs[0].To-runs[0].From+1 > linksend.MaxReplaySpan || runs[1].To-runs[1].From+1 > linksend.MaxReplaySpan || runs[2].To != int64(2*linksend.MaxReplaySpan+5) {
		t.Errorf("a gap wider than a request may ask for is asked in runs: %+v", runs)
	}
}

// A payment recorded by hand to a person Nemura holds for tax clearance is refused too (PRD-001 FR-PPL-13, FR-PAY-10; p1-payroll-link 6.3): a claim's reimbursement and a cash advance go through the one check
// every release of money to a person uses, so a claimant or a member mapped to a held person is refused with PPL-21 naming them and no figure, a person nobody mapped is not asked about, and the same
// payment goes ahead to its own checks once Nemura lifts the hold.
// part of FR-PPL-13 (a director's repayment is not recorded by hand: it is paid through a settlement's approval)
// asvs: V8.2.1, V8.3.1
func TestAPaymentRecordedByHandToAHeldPersonIsRefusedAndGoesAheadOnceTheHoldIsLifted(t *testing.T) {
	w := payrollWorld(t)
	e := w.e
	ctx := context.Background()
	mei, err := e.store.UserForIdentity(ctx, w.preparer.Issuer, w.preparer.Subject)
	if err != nil {
		t.Fatal(err)
	}
	person := uuid.NewString()
	if r := w.nemuraSays(peoplelink.TypePersonUpserted, "person", person, 1, map[string]any{"person_id": person, "display_name": "Wei Jie Ong", "work_email": "weijie@example.com", "status": "active", "deleted": false}, nil); r.Status != http.StatusOK {
		t.Fatalf("the person: %d %s", r.Status, r.Raw)
	}
	admin := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: w.org, Roles: []authz.Role{authz.Administrator}})
	if r := e.call(http.MethodPost, "/v1/people-link/people/"+person+"/user", admin, map[string]any{"user_id": w.approverUser.String()}); r.Status != http.StatusOK {
		t.Fatalf("mapping: %d %s", r.Status, r.Raw)
	}
	hold := func(version int, status, evidence string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"person_id": person, "hold_id": "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e62", "effective_from": "2026-10-01", "status": status, "evidence_ref": evidence})
		now := e.clock.Now().UTC().Format(time.RFC3339)
		env, err := peoplelink.Sign(w.linkKey, peoplelink.Header{EventID: uuid.NewString(), Type: peoplelink.TypeTaxHoldChanged, SchemaVersion: 1, SourceSystem: "nemura", SourceOrganisation: w.nemura,
			TargetOrganisation: w.org.String(), Epoch: 1, Sequence: int64(version), ObjectType: "tax_hold", ObjectID: person, ObjectVersion: version, EffectiveAt: now, RecordedAt: now, SignedAt: now, CorrelationID: uuid.NewString()}, raw)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(env)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		if r := e.call(http.MethodPost, "/v1/people-link/hold-events", w.tok, body); r.Status != http.StatusOK {
			t.Fatalf("the hold: %d %s", r.Status, r.Raw)
		}
	}
	ap := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: w.org, Roles: []authz.Role{authz.Accountant, authz.PaymentReleaser}})
	reimburse := func(claimant uuid.UUID) response {
		return e.call(http.MethodPost, "/v1/claim-reimbursements", ap, map[string]any{"claimant_id": claimant.String(), "money_account_id": uuid.NewString(), "date": "2026-10-20", "claims": []map[string]any{}}, "Idempotency-Key", uuid.NewString())
	}
	advance := func(member uuid.UUID) response {
		return e.call(http.MethodPost, "/v1/cash-advances", ap, map[string]any{"person_id": member.String(), "money_account_id": uuid.NewString(), "date": "2026-10-20", "amount": map[string]any{"amount": "100.00", "currency": "SGD"}}, "Idempotency-Key", uuid.NewString())
	}
	hold(1, "held", "")
	for what, r := range map[string]response{"a reimbursement": reimburse(w.approverUser), "an advance": advance(w.approverUser)} {
		if r.problemCode() != peoplelink.CodeHold || strings.Contains(r.Raw, "100.00") {
			t.Errorf("%s to a held person: %d %s", what, r.Status, r.Raw)
		}
	}
	for what, r := range map[string]response{"a reimbursement": reimburse(mei), "an advance": advance(mei)} {
		if r.problemCode() == peoplelink.CodeHold || r.problemCode() == peoplelink.CodeHoldUnknown {
			t.Errorf("%s to a person nobody mapped is asked about: %d %s", what, r.Status, r.Raw)
		}
	}
	hold(2, "released", uuid.NewString())
	for what, r := range map[string]response{"a reimbursement": reimburse(w.approverUser), "an advance": advance(w.approverUser)} {
		if r.problemCode() == peoplelink.CodeHold || r.problemCode() == peoplelink.CodeHoldUnknown {
			t.Errorf("%s once the hold was lifted: %d %s", what, r.Status, r.Raw)
		}
	}
}

// The people projection only narrows an Approver's department scope (design D23): the API's reading is the database's.
// req: FR-ADM-01
func TestTheProjectionOnlyNarrowsAnApproversScopeInTheAPIToo(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	scope := authn.DepartmentScope{Narrowed: true, Values: []uuid.UUID{a, b}}
	if got := narrowedByProjection(scope, a); !got.Narrowed || len(got.Values) != 1 || got.Values[0] != a {
		t.Errorf("a department in the scope: %+v", got)
	}
	if got := narrowedByProjection(scope, c); !got.Narrowed || len(got.Values) != 0 {
		t.Errorf("a department outside it widened or kept the scope: %+v", got)
	}
	if got := narrowedByProjection(scope, uuid.Nil); len(got.Values) != 2 {
		t.Errorf("no word of the projection: %+v", got)
	}
	if got := narrowedByProjection(authn.DepartmentScope{Narrowed: true}, a); len(got.Values) != 0 {
		t.Errorf("a scope with no values was given one: %+v", got)
	}
	if got := narrowedByProjection(authn.DepartmentScope{}, a); got.Narrowed || len(got.Values) != 0 {
		t.Errorf("an unscoped role was narrowed: %+v", got)
	}
}
