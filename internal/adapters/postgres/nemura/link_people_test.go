//go:build local

package nemura_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/link"
	"github.com/cognizhi/fyniq/internal/nemura/outbox"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// fyniqInMiniature is a Fyniq that takes the link's token and the envelope, for the sender's tests: it records each call and answers with what the test sets.
type fyniqInMiniature struct {
	srv    *httptest.Server
	calls  []link.Envelope
	status int    // the answer's status (200 when zero)
	code   string // a problem's code for a refusal
}

func newFyniqInMiniature(t *testing.T, offer nemura.LinkOffer, paths ...string) *fyniqInMiniature {
	t.Helper()
	f := &fyniqInMiniature{}
	var clientKey jose.JSONWebKey
	if err := json.Unmarshal(offer.ClientKey, &clientKey); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		jws, err := jose.ParseSigned(r.PostForm.Get("client_assertion"), []jose.SignatureAlgorithm{jose.ES256})
		if err == nil {
			_, err = jws.Verify(clientKey.Key)
		}
		if err != nil {
			http.Error(w, "not this client", 401)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"token-` + r.PostForm.Get("scope") + `","expires_in":3600,"token_type":"bearer"}`))
	})
	for _, p := range paths {
		mux.HandleFunc("POST "+p, func(w http.ResponseWriter, r *http.Request) {
			var env link.Envelope
			_ = json.NewDecoder(r.Body).Decode(&env)
			f.calls = append(f.calls, env)
			if f.status != 0 && f.status/100 != 2 {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(f.status)
				_, _ = w.Write([]byte(`{"code":"` + f.code + `","detail":"a payload I must not repeat"}`))
				return
			}
			_, _ = w.Write([]byte(`{"event_id":"x","outcome":"applied"}`))
		})
	}
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fyniqInMiniature) sender(t *testing.T, now time.Time) *nemura.LinkSender {
	return &nemura.LinkSender{Store: store(t, "nemura_worker"), Clock: clock.NewFrozen(now), HTTP: f.srv.Client(), APIURL: f.srv.URL, TokenURL: f.srv.URL + "/oauth2/token", Audience: "fyniq-api"}
}

// queuedRows reads the link's outbox rows in the order they were queued.
func queuedRows(t *testing.T, lt *leaveTeam) []outbox.Row {
	t.Helper()
	var rows []outbox.Row
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		rs, err := tx.Query(ctx, `SELECT id, payload FROM nemura.outbox WHERE kind = $1 ORDER BY created_at, id`, nemura.KindLinkSend)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var r outbox.Row
			if err := rs.Scan(&r.ID, &r.Payload); err != nil {
				return err
			}
			rows = append(rows, r)
		}
		return rs.Err()
	})
	return rows
}

type loggedEvent struct {
	Type, State, Code string
	Version           int
	Sequence          int64
	Object            string
}

func eventLog(t *testing.T, lt *leaveTeam) []loggedEvent {
	t.Helper()
	var out []loggedEvent
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		rs, err := tx.Query(ctx, `SELECT type, state, coalesce(code, ''), object_version, sequence, object_id FROM nemura.link_event ORDER BY sequence`)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var e loggedEvent
			if err := rs.Scan(&e.Type, &e.State, &e.Code, &e.Version, &e.Sequence, &e.Object); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rs.Err()
	})
	return out
}

// The people Nemura tells Fyniq of, and their bank details, are queued with the change and sent with what accounting needs and nothing else (NM-INT-01, 05, 09; FR-PPL-06, 14): a standalone
// organisation queues nothing; activating the link queues the people, bank-detail versions and holds that already exist; each change after it is the next version of that person; the payload is the
// contract's fields exactly (no NRIC, birth date, salary, classification, leaving reason or account), with Fyniq's department where Fyniq told Nemura of it; a bank event carries the record and its
// version and never the number; the envelope verifies with Nemura's key; and a refusal that asking again cannot change finishes the row with Fyniq's code.
// part of NM-INT-01 (leave and the run are sent separately)
// part of NM-INT-09 (the batch's lines are Fyniq's)
// asvs: V8.2.1, V13.2.1, V14.2.4, V16.2.5
func TestPeopleAndBankDetailsAreQueuedWithTheirChangeAndSentWithOnlyWhatAccountingNeeds(t *testing.T) {
	ctx := context.Background()
	now := today.Add(10 * time.Hour)
	cipher, _ := local.NewFieldKeys(bytes.Repeat([]byte{7}, 64))

	standalone := newLeaveTeam(t)
	if got := eventLog(t, standalone); len(got) != 0 {
		t.Fatalf("a standalone organisation logged %d events for Fyniq", len(got))
	}
	// Ben had a salary account before the link: its version goes ahead with the link.
	lt := newLeaveTeam(t)
	var change uuid.UUID
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		change, err = nemura.ProposeBankChange(ctx, tx, cipher, lt.c.org, now, lt.benLogin, lt.ben, "123-456-7890")
		return err
	})
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideBankChange(ctx, tx, lt.c.org, now, lt.c.owner, []authz.Role{authz.Owner}, change, true, "")
	})
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ConfirmBankChange(ctx, tx, lt.c.org, now, lt.benLogin, change, true)
	})
	if got := eventLog(t, lt); len(got) != 0 {
		t.Fatalf("an unlinked organisation logged %d events", len(got))
	}
	offer, _, _ := linked(t, lt, now, &fakeOAuth{})
	log := eventLog(t, lt)
	count := map[string]int{}
	for _, e := range log {
		count[e.Type]++
		if e.Version != 1 || e.State != "queued" {
			t.Errorf("a snapshot event: %+v", e)
		}
	}
	if count[nemura.TypePersonUpserted] != 3 || count[nemura.TypeBankDetailsChanged] != 1 || len(log) != 4 {
		t.Fatalf("activating the link queues the three people and Ben's one bank-detail version: %v", count)
	}

	// Fyniq's departments, as it told Nemura of them: Engineering is a value of Fyniq's own.
	engineering, cost := uuid.New(), uuid.New()
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.link_mapping (mapping_version, event_id, received_at) VALUES (3, $1, $2)`, uuid.New(), now); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO nemura.link_department (mapping_version, department_id, name, cost_centre_id) VALUES (3, $1, ' engineering ', $2)`, engineering, cost)
		return err
	})
	// A new joiner, then a change of the record, then of the job: versions 1, 2 and 3 of that person.
	var dee uuid.UUID
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		dee, err = nemura.AddPerson(ctx, tx, lt.c.org, now, lt.c.owner, people.Person{LegalName: "Dee Ng", PreferredName: "Dee", WorkEmail: "dee@example.test", EmploymentStart: d("2026-02-02")},
			people.Job{Title: "Analyst", Department: "Engineering", Worksite: "SG", EmploymentType: "full_time", Manager: &lt.ana}, &people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen"})
		return err
	})
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.UpdatePerson(ctx, tx, lt.c.org, now, lt.c.owner, dee, people.Person{LegalName: "Dee Ng", PreferredName: "Dee", WorkEmail: "dee@example.test", Phone: "91234567",
			ProbationEnd: ptr(d("2026-05-02")), LeavingDate: ptr(d("2099-09-30")), LeavingReason: "resigned for a medical reason"})
	})
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ChangeJob(ctx, tx, lt.c.org, now, lt.c.owner, dee, d("2026-03-02"), people.Job{Title: "Senior analyst", Department: "Sales", Worksite: "SG", EmploymentType: "full_time", Manager: &lt.ana})
	})
	var deeEvents []loggedEvent
	for _, e := range eventLog(t, lt) {
		if e.Object == dee.String() {
			deeEvents = append(deeEvents, e)
		}
	}
	if len(deeEvents) != 3 || deeEvents[0].Version != 1 || deeEvents[1].Version != 2 || deeEvents[2].Version != 3 {
		t.Fatalf("Dee's events number themselves: %+v", deeEvents)
	}

	// Send them all to a Fyniq in miniature, in the order they were queued.
	fy := newFyniqInMiniature(t, offer, "/v1/people-link/projection-events", "/v1/people-link/hold-events")
	work := fy.sender(t, now).Work()
	rows := queuedRows(t, lt)
	if len(rows) != 7 {
		t.Fatalf("queued rows: %d", len(rows))
	}
	for _, r := range rows {
		if err := work(ctx, lt.c.org, r); err != nil {
			t.Fatal(err)
		}
	}
	if len(fy.calls) != 7 {
		t.Fatalf("Fyniq took %d events", len(fy.calls))
	}
	for _, e := range eventLog(t, lt) {
		if e.State != "sent" {
			t.Errorf("a sent event is logged as %+v", e)
		}
	}
	forbidden := []string{"S1234567", "123-456-7890", "1234567890", "resigned", "medical", "91234567", "1990", "service", "citizen", "4500", "****", "Ana Tan", "Ben Lim", "Cy Wong", "Engineer"}
	var deeAt = map[int]map[string]any{}
	var bank map[string]any
	for _, env := range fy.calls {
		if err := env.Verify(link.Link{Key: offer.LinkKey, Epoch: 1, SourceOrganisation: lt.c.org.String(), TargetOrganisation: env.Header.TargetOrganisation, SourceSystem: "nemura"}, now); err != nil {
			t.Errorf("%s does not verify with Nemura's key: %v", env.Header.Type, err)
		}
		var p map[string]any
		_ = json.Unmarshal(env.Payload, &p)
		switch env.Header.Type {
		case nemura.TypePersonUpserted:
			if len(p) != 10 {
				t.Errorf("a person event is the contract's ten fields: %s", env.Payload)
			}
			for _, bad := range forbidden[:9] {
				if strings.Contains(string(env.Payload), bad) {
					t.Errorf("a person event carries %q: %s", bad, env.Payload)
				}
			}
			if env.Header.ObjectID == dee.String() {
				deeAt[env.Header.ObjectVersion] = p
			}
		case nemura.TypeBankDetailsChanged:
			bank = p
			if len(p) != 2 || p["person_id"] != lt.ben.String() || p["bank_detail_id"] != change.String() || env.Header.ObjectVersion != 1 {
				t.Errorf("the bank-details event is the person and the record and nothing else: %s at version %d", env.Payload, env.Header.ObjectVersion)
			}
			for _, bad := range []string{"123", "7890", "***", "masked", "fingerprint", "sealed"} {
				if strings.Contains(string(env.Payload), bad) {
					t.Errorf("a bank-details event carries %q: %s", bad, env.Payload)
				}
			}
		}
	}
	if bank == nil || len(deeAt) != 3 {
		t.Fatalf("the events sent: bank %v, Dee at %v", bank, deeAt)
	}
	// The payload is the record as it is when it is sent: all three of Dee's events carry the newest figures (Sales has no department at Fyniq).
	for v, p := range deeAt {
		if p["display_name"] != "Dee" || p["work_email"] != "dee@example.test" || p["department_id"] != nil || p["left_on"] != "2099-09-30" || p["status"] != "active" || p["line_manager_person_id"] != lt.ana.String() || p["deleted"] != false {
			t.Errorf("Dee's event %d: %v", v, p)
		}
	}
	// With Fyniq's Engineering, the department and cost centre are Fyniq's own values.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ChangeJob(ctx, tx, lt.c.org, now, lt.c.owner, dee, d("2026-04-01"), people.Job{Title: "Senior analyst", Department: "ENGINEERING", Worksite: "SG", EmploymentType: "full_time", Manager: nil})
	})
	rows = queuedRows(t, lt)
	if err := work(ctx, lt.c.org, rows[len(rows)-1]); err != nil {
		t.Fatal(err)
	}
	var last map[string]any
	_ = json.Unmarshal(fy.calls[len(fy.calls)-1].Payload, &last)
	if last["department_id"] != engineering.String() || last["cost_centre_id"] != cost.String() || last["line_manager_person_id"] != nil || fy.calls[len(fy.calls)-1].Header.ObjectVersion != 4 {
		t.Errorf("a department Fyniq told Nemura of, a person with no manager, version 4: %v", last)
	}

	// A refusal that asking again cannot change finishes the row, settled as refused with Fyniq's code; a refusal that might pass, or no answer, leaves it to be tried again.
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.UpdatePerson(ctx, tx, lt.c.org, now, lt.c.owner, dee, people.Person{LegalName: "Dee Ng", PreferredName: "Dee Ng"})
	})
	rows = queuedRows(t, lt)
	newest := rows[len(rows)-1]
	fy.status, fy.code = 503, "UNAVAILABLE"
	if err := work(ctx, lt.c.org, newest); err == nil || strings.Contains(err.Error(), "must not repeat") {
		t.Fatalf("an unavailable Fyniq: %v", err)
	}
	if log := eventLog(t, lt); log[len(log)-1].State != "failed" || log[len(log)-1].Code != "UNAVAILABLE" {
		t.Errorf("a failed attempt is logged with Fyniq's code: %+v", log[len(log)-1])
	}
	fy.status, fy.code = 422, "PPL-09"
	if err := work(ctx, lt.c.org, newest); err != nil {
		t.Fatalf("a refusal that cannot change finishes the row: %v", err)
	}
	if log := eventLog(t, lt); log[len(log)-1].State != "refused" || log[len(log)-1].Code != "PPL-09" {
		t.Errorf("a final refusal is logged: %+v", log[len(log)-1])
	}
	fy.status = 0
	before := len(fy.calls)
	if err := work(ctx, lt.c.org, newest); err != nil || len(fy.calls) != before+1 {
		t.Errorf("a settled event sent again (a retry after a lost answer) is not logged twice: %v", err)
	}
	if log := eventLog(t, lt); log[len(log)-1].State != "refused" {
		t.Errorf("a settled event stays settled: %+v", log[len(log)-1])
	}
	// The log cannot be rewritten, and the API cannot settle what it queued.
	for _, x := range []struct{ role, stmt string }{
		{"nemura_migrator", "UPDATE nemura.link_event SET object_version = 99"},
		{"nemura_migrator", "DELETE FROM nemura.link_event"},
		{"nemura_api", "UPDATE nemura.link_event SET state = 'sent'"},
		{"nemura_worker", "UPDATE nemura.link_event SET state = 'queued' WHERE state = 'sent'"},
	} {
		err := store(t, x.role).InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		if err == nil {
			t.Errorf("%s ran %q", x.role, x.stmt)
		}
	}
	// A person who is not there is a tombstone, never an error.
	gone := uuid.New()
	var tomb nemura.PersonEvent
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		tomb, err = nemura.PersonEventOf(ctx, tx, gone, now)
		return err
	})
	if !tomb.Deleted || tomb.PersonID != gone || tomb.DisplayName != "" {
		t.Errorf("a removed person: %+v", tomb)
	}
}

// The payload structs are the contract's fields and no others (NM-INT-05): each field of Appendix E's rows, read from the contract's README, is a field of the struct and the other way round, so
// a field added to an event without the contract is caught here and refused there (PPL-09).
// part of NM-INT-05 (the other events' payloads are pinned in their own tests)
// asvs: V14.2.4, V8.2.1
func TestThePeopleEventsCarryExactlyTheContractsFields(t *testing.T) {
	raw, err := os.ReadFile("../../../../contracts/peoplelink/README.md")
	if err != nil {
		t.Fatal(err)
	}
	fields := func(eventType string) []string {
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, "| `"+eventType+"` |") {
				continue
			}
			cells := strings.Split(line, "|")
			cell := regexp.MustCompile(`\([^)]*\)`).ReplaceAllString(cells[len(cells)-2], "") // words in brackets are not fields
			var out []string
			for _, m := range regexp.MustCompile("`([a-z_]+)`").FindAllStringSubmatch(cell, -1) {
				if m[1] != "object_version" { // the envelope's header, not the payload
					out = append(out, m[1])
				}
			}
			return out
		}
		t.Fatalf("the README has no row for %s", eventType)
		return nil
	}
	tags := func(v any) []string {
		var out []string
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			out = append(out, strings.Split(rt.Field(i).Tag.Get("json"), ",")[0])
		}
		return out
	}
	same := func(a, b []string) bool {
		m := map[string]bool{}
		for _, x := range a {
			m[x] = true
		}
		for _, x := range b {
			if !m[x] {
				return false
			}
			delete(m, x)
		}
		return len(m) == 0
	}
	// The README lists "(a tombstone)" and "(YYYY-MM-DD)" as words, not fields: only backticked names are fields.
	if got, want := tags(nemura.PersonEvent{}), fields("people.person.upserted"); !same(got, want) {
		t.Errorf("the person event's fields %v are not the contract's %v", got, want)
	}
	if got, want := tags(nemura.BankDetailsEvent{}), fields("people.bank_details.changed"); !same(got, want) {
		t.Errorf("the bank-details event's fields %v are not the contract's %v", got, want)
	}
	if got, want := tags(nemura.TaxHoldEvent{}), fields("people.tax_hold.changed"); !same(got, want) {
		t.Errorf("the hold event's fields %v are not the contract's %v", got, want)
	}
}
