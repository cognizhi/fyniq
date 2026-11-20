//go:build local

package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/apiclient"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/linksend"
	"github.com/cognizhi/fyniq/internal/passkey/passkeytest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

const nemuraOrigin = "https://people.example.test"

type runWorld struct {
	readonly            string // the world's database, read-only: what the test reads the outbox with
	e                   *env
	org                 uuid.UUID
	nemura              string
	linkKey             ed25519.PrivateKey
	tok                 string
	approver, preparer  peoplelink.Party
	pass                *passkeytest.Authenticator
	dept1, dept2        string
	mappingVersion      int
	approverUser        uuid.UUID
	code                map[uuid.UUID]string
	accountant, payroll string
	desk, phone         *browser // Mei Ling and Wei Jie, signed in through the sessions' server
	origin, rpHost      string   // the sessions' server as the browsers see it, and its relying party's host
}

// payrollWorld is an organisation with its chart, periods, departments and mapping, linked to a Nemura organisation, whose approver (Wei Jie) has a
// passkey enrolled in Fyniq: the setup every pay-run test starts from. The link is made in the store; its approval flow is TestThePeopleLink...'s.
func payrollWorld(t *testing.T) *runWorld {
	t.Helper()
	ctx := context.Background()
	s := startSessions(t)
	desk, phone, orgText := s.founders(t)
	// The people sign in through the sessions' server; Nemura's calls and the test's own principals go through a second API on the same database
	// and relying party, which takes the test's tokens.
	te := *s.env
	mux := http.NewServeMux()
	te.srv = httptest.NewServer(mux)
	t.Cleanup(te.srv.Close)
	te.api = New(mux, Deps{Store: te.store, Installation: te.store, Clock: te.clock, Auth: te.tokens, Log: slog.New(slog.DiscardHandler), Version: "0.0.0-test",
		Origin: strings.Replace(s.srv.URL, "127.0.0.1", "localhost", 1)})
	e := &te
	org := uuid.MustParse(orgText)
	origin := strings.Replace(s.srv.URL, "127.0.0.1", "localhost", 1)
	host, _ := url.Parse(origin)
	pass := passkeytest.New(t, host.Hostname(), origin)
	r := phone.call(http.MethodPost, "/v1/session/passkeys/challenge", "same-origin", nil)
	if reg := phone.call(http.MethodPost, "/v1/session/passkeys", "same-origin", map[string]any{"challenge_id": r.str("challenge_id"), "name": "Phone",
		"credential": credential(t, pass.Create(passkeytest.Challenge(t, options(t, r))))}); reg.Status != http.StatusCreated {
		t.Fatalf("registering the approver's passkey: %d %s", reg.Status, reg.Raw)
	}
	pass.Origin = nemuraOrigin // from now on the approver signs in Nemura's web app, at its own origin of the same relying party
	party := func(b *browser) (peoplelink.Party, uuid.UUID) {
		for _, x := range b.call(http.MethodGet, "/v1/members", "same-origin", nil).Body["items"].([]any) {
			m := x.(map[string]any)
			if m["display_name"] == map[bool]string{true: "Wei Jie Lim", false: "Mei Ling Tan"}[b == phone] {
				user := uuid.MustParse(m["user_id"].(string))
				ids, err := e.store.Identities(ctx, user)
				if err != nil || len(ids) == 0 {
					t.Fatalf("identities: %v %v", ids, err)
				}
				return peoplelink.Party{Issuer: ids[0].Issuer, Subject: ids[0].Subject}, user
			}
		}
		t.Fatal("member not found")
		return peoplelink.Party{}, uuid.Nil
	}
	w := &runWorld{readonly: s.readonly, e: e, org: org, nemura: uuid.NewString(), code: map[uuid.UUID]string{}, pass: pass, desk: desk, phone: phone, origin: origin, rpHost: host.Hostname()}
	var owner uuid.UUID
	w.approver, w.approverUser = party(phone)
	w.preparer, owner = party(desk)
	accounts := map[string]storage.AccountType{"1250": storage.Asset, "2100": storage.Liability, "2110": storage.Liability, "2150": storage.Liability, "6011": storage.Expense,
		"6013": storage.Expense, "6020": storage.Expense, "6030": storage.Expense}
	roles := map[string]storage.AccountRole{"2100": "salaries-payable", "2110": "cpf-sdl-payable", "2150": "expense-claims-payable", "6020": "employer-cpf", "6030": "sdl"}
	ids := map[string]uuid.UUID{}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	w.linkKey = priv
	if err := e.store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		for code, typ := range accounts {
			acc, err := u.Accounts().Create(ctx, storage.NewAccount{Code: code, Name: "Payroll " + code, Type: typ, Role: roles[code]})
			if err != nil {
				return err
			}
			ids[code], w.code[acc.ID] = acc.ID, code
		}
		if _, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), EndsOn: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)}); err != nil {
			return err
		}
		if _, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), EndsOn: time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)}); err != nil {
			return err
		}
		approval := uuid.New()
		client, err := u.APIClients().Create(ctx, storage.APIClient{Kind: storage.APIClientLink, ClientID: "link-" + uuid.NewString(), Name: "Nemura link", Class: authn.ClassIntegration,
			OwnerID: owner, Scopes: apiclient.LinkScopes, IPAllow: []string{}, KeyThumbprint: strings.Repeat("t", 43), ExpiresAt: e.clock.Now().AddDate(0, 6, 0), ApprovalID: &approval,
			CreatedBy: "user:" + owner.String(), CreatedAt: e.clock.Now()})
		if err != nil {
			return err
		}
		linked, err := u.PeopleLinks().Link(ctx, storage.PeopleLinkRow{NemuraOrganisationID: uuid.MustParse(w.nemura), NemuraOrigin: nemuraOrigin, LinkKey: pub, ClientID: client.ID,
			ApprovalID: approval, CreatedBy: "user:" + owner.String(), CreatedAt: e.clock.Now()})
		if err != nil {
			return err
		}
		keys, secrets, err := newLinkKeys(linked.ID, "fyniq-link-"+w.nemura) // so the link can answer: what Nemura is told of each run is queued with the run
		if err != nil {
			return err
		}
		if err := u.PeopleLinks().SaveKeys(ctx, keys, secrets); err != nil {
			return err
		}
		w.tok = e.principal(authn.Principal{Client: client.ClientID, Class: authn.ClassIntegration, Organisation: org, Scoped: true, Link: true, Scopes: apiclient.LinkScopes})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	person := func(r authz.Role) string {
		return e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: org, Roles: []authz.Role{r}})
	}
	w.accountant = person(authz.Accountant)
	w.payroll = person(authz.PayrollAdmin)
	dim := e.call(http.MethodPost, "/v1/dimensions", w.accountant, map[string]any{"kind": "department", "name": "Department"})
	if dim.Status != 201 {
		t.Fatalf("the Department dimension: %d %s", dim.Status, dim.Raw)
	}
	value := func(code string) string {
		v := e.call(http.MethodPost, "/v1/dimension-values", w.accountant, map[string]any{"dimension_id": dim.str("id"), "code": code, "name": code})
		if v.Status != 201 {
			t.Fatalf("department %s: %d %s", code, v.Status, v.Raw)
		}
		return v.str("id")
	}
	w.dept1, w.dept2 = value("GA"), value("RD")
	set := e.call(http.MethodPost, "/v1/payroll-mapping", w.payroll, map[string]any{"entries": map[string]string{"department:" + w.dept1: ids["6013"].String(),
		"department:" + w.dept2: ids["6011"].String(), "deduction:advance_recovery": ids["1250"].String()}}, "Idempotency-Key", uuid.NewString())
	if set.Status != http.StatusOK || set.Body["version"] != float64(1) {
		t.Fatalf("setting the mapping: %d %s", set.Status, set.Raw)
	}
	w.mappingVersion = 1
	return w
}

// approved is an approval envelope as Nemura would state it for October 2026: G&A 10,000.00 and R&D 20,000.00 (rd) gross, the contributions, an advance
// recovered and claims paid with salary. edit changes it before it is signed.
func (w *runWorld) approved(run string, version int, rd string, employerCPF string, edit func(*peoplelink.RunEnvelope)) map[string]any {
	env := peoplelink.RunEnvelope{Organisation: w.nemura, SourceSystem: "nemura", RunID: run, Version: version, Supersedes: version - 1, WageMonth: "2026-10",
		Departments: []peoplelink.RunDepartment{{DepartmentID: w.dept1, Gross: "10000.00"}, {DepartmentID: w.dept2, Gross: rd}}, EmployeeCPF: "3000.00", EmployerCPF: employerCPF,
		SDL: "75.00", SHG: "20.00", Deductions: []peoplelink.RunDeduction{{Kind: "advance_recovery", Amount: "500.00"}}, Reimbursements: "300.00", PackVersion: "sg-payroll@2026.1",
		PolicyVersion: "policy@1", MappingVersion: w.mappingVersion, Preparer: w.preparer, Approver: w.approver, ApproverRight: peoplelink.PayrollRightApprove}
	if edit != nil {
		edit(&env)
	}
	raw, _ := json.Marshal(env)
	canonical, err := peoplelink.Canonical(raw)
	if err != nil {
		w.e.t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	return w.signed(raw, sum[:], w.pass)
}

// signed is a submission: the envelope, its digest and the approver's passkey assertion over it, made with pass.
func (w *runWorld) signed(envelope []byte, digest []byte, pass *passkeytest.Authenticator) map[string]any {
	var cred struct {
		Response struct{ ClientDataJSON, AuthenticatorData, Signature string } `json:"response"`
	}
	if err := json.Unmarshal(pass.Get(digest), &cred); err != nil {
		w.e.t.Fatal(err)
	}
	var env map[string]any
	_ = json.Unmarshal(envelope, &env)
	return map[string]any{"envelope": env, "envelope_sha256": hexOf(digest), "approval": map[string]any{"authenticator_data": cred.Response.AuthenticatorData,
		"client_data": cred.Response.ClientDataJSON, "signature": cred.Response.Signature}}
}

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, x := range b {
		out = append(out, digits[x>>4], digits[x&15])
	}
	return string(out)
}

// send wraps a submission in the link's envelope and posts it as the link's client.
func (w *runWorld) send(submission map[string]any, edit func(*peoplelink.Header)) response {
	return w.sendAs(w.tok, submission, edit)
}

func (w *runWorld) sendAs(tok string, submission map[string]any, edit func(*peoplelink.Header)) response {
	w.e.t.Helper()
	raw, _ := json.Marshal(submission)
	now := w.e.clock.Now().UTC().Format(time.RFC3339)
	h := peoplelink.Header{EventID: uuid.NewString(), Type: peoplelink.TypePayrollRunApproved, SchemaVersion: 1, SourceSystem: "nemura", SourceOrganisation: w.nemura,
		TargetOrganisation: w.org.String(), Epoch: 1, Sequence: 1, ObjectType: "payroll_run", ObjectID: submission["envelope"].(map[string]any)["run_id"].(string), ObjectVersion: 1,
		EffectiveAt: now, RecordedAt: now, SignedAt: now, CorrelationID: uuid.NewString()}
	if edit != nil {
		edit(&h)
	}
	env, err := peoplelink.Sign(w.linkKey, h, raw)
	if err != nil {
		w.e.t.Fatal(err)
	}
	b, _ := json.Marshal(env)
	var body map[string]any
	_ = json.Unmarshal(b, &body)
	return w.e.call(http.MethodPost, "/v1/people-link/payroll-runs", tok, body)
}

// entryLines are an entry's lines as "code amount", by code, for the run's entry at a version.
func (w *runWorld) entryLines(run string, version int) string {
	var out []string
	if err := w.e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		e, err := u.Ledger().ByIdentity(ctx, ledger.PayrollIdentity("nemura", uuid.MustParse(run), version, ledger.RulePayrollRun))
		if err != nil {
			return err
		}
		for _, l := range e.Lines {
			out = append(out, w.code[l.AccountID]+" "+string(l.BaseAmount))
		}
		return nil
	}); err != nil {
		w.e.t.Fatal(err)
	}
	sort.Strings(out)
	return strings.Join(out, "; ")
}

func (w *runWorld) entries() int {
	n := 0
	_ = w.e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		es, err := u.Ledger().Entries(ctx, 0, 1000)
		n = len(es)
		return err
	})
	return n
}

// An approved Nemura pay run posts only when its authority verifies (FR-PPL-04, 09, 10, 11; PRD-002 NM-INT-06, NM-INT-07): the first version posts October
// by department with employer CPF, SDL, 2110, 2100, the advance recovered and the claims paid with salary, no person on a line, attributed to the run,
// and its second version posts only the difference; a repeat of the event does nothing, a version no newer is stale and an envelope is used once. A
// run that Nemura's valid token and link key sent, but whose authority does not hold, is refused with its own code each time, posts nothing, and is audited:
// altered totals under an earlier approval, the preparer approving, an approver without the right, another organisation's approval, another mapping version,
// a passkey that is not the approver's and an approver with no identity here. A closed wage month's difference posts in the open month.
func TestAnApprovedPayRunPostsOnlyAfterItsAuthorityVerifies(t *testing.T) {
	w := payrollWorld(t)
	e := w.e
	run := uuid.NewString()
	v1 := w.approved(run, 1, "20000.00", "5100.00", nil)
	first := w.send(v1, nil)
	if first.Status != http.StatusOK || first.str("outcome") != "posted" || first.str("posting_date") != "2026-10-31" {
		t.Fatalf("version 1: %d %s", first.Status, first.Raw)
	}
	want1 := "1250 -500.0000; 2100 -26780.0000; 2110 -8195.0000; 2150 300.0000; 6011 20000.0000; 6013 10000.0000; 6020 5100.0000; 6030 75.0000"
	if got := w.entryLines(run, 1); got != want1 {
		t.Fatalf("the entry: %s\nwant %s", got, want1)
	}
	// Nemura is told what became of the run, in the same unit of work: the posting's reference, at the link's next sequence number.
	answers := w.answers(t)
	if len(answers) != 1 || answers[0].Sequence != 2 || answers[0].ObjectID != run || answers[0].ObjectVersion != 1 || answers[0].Type != linksend.TypeRunPosted {
		t.Fatalf("the answer queued for Nemura: %+v", answers)
	}
	var told linksend.RunPosted
	_ = json.Unmarshal(answers[0].Payload, &told)
	if told.Status != "posted" || told.RunID != run || told.Version != 1 || told.PostingRef == "" || told.Code != "" {
		t.Errorf("the answer: %+v", told)
	}
	// The same event again does nothing; a new event with the same envelope is the envelope used twice; version 1 again is stale.
	if r := w.sendAs(w.tok, v1, func(h *peoplelink.Header) { h.EventID = first.Body["event_id"].(string) }); r.Status != http.StatusOK || r.str("outcome") != "duplicate" {
		t.Errorf("the event again: %d %s", r.Status, r.Raw)
	}
	e.problemIs(w.send(v1, nil), "submitPayrollRun", 409, peoplelink.CodeRunUsed)
	e.problemIs(w.send(w.approved(run, 1, "20000.00", "5100.00", func(x *peoplelink.RunEnvelope) { x.PolicyVersion = "policy@2" }), nil), "submitPayrollRun", 409, peoplelink.CodeRunStale)
	count := w.entries()

	// Version 2 states the month's full figures again: only the difference posts.
	v2 := w.approved(run, 2, "20200.00", "5134.00", nil)
	if r := w.send(v2, nil); r.Status != http.StatusOK || r.str("outcome") != "posted" {
		t.Fatalf("version 2: %d %s", r.Status, r.Raw)
	}
	if got, want := w.entryLines(run, 2), "2100 -200.0000; 2110 -34.0000; 6011 200.0000; 6020 34.0000"; got != want {
		t.Errorf("the difference: %s, want %s", got, want)
	}
	// A third version with the same figures changes nothing.
	if r := w.send(w.approved(run, 3, "20200.00", "5134.00", nil), nil); r.Status != http.StatusOK || r.str("outcome") != "unchanged" {
		t.Errorf("version 3: %d %s", r.Status, r.Raw)
	}
	count = w.entries()

	// A repeat of a used envelope is not answered (it says nothing about a run that is waiting); the version that changed nothing is answered with the entry the run's first version posted.
	if answers := w.answers(t); len(answers) != 3 {
		t.Errorf("answers queued after version 1, a repeat, a stale version, version 2 and version 3: %d, want 3 (the repeat and the stale version are not answered)", len(answers))
	}

	// Each forged authority is refused with its own code and creates nothing.
	otherRun := uuid.NewString()
	forged := w.approved(otherRun, 1, "20000.00", "5100.00", nil)
	forged["envelope"].(map[string]any)["departments"].([]any)[1].(map[string]any)["gross"] = "90000.00" // altered totals under the earlier approval's digest
	e.problemIs(w.send(forged, nil), "submitPayrollRun", 422, peoplelink.CodeRunEnvelope)
	e.problemIs(w.send(w.approved(otherRun, 1, "20000.00", "5100.00", func(x *peoplelink.RunEnvelope) { x.Approver = x.Preparer }), nil), "submitPayrollRun", 403, peoplelink.CodeRunSegregate)
	e.problemIs(w.send(w.approved(otherRun, 1, "20000.00", "5100.00", func(x *peoplelink.RunEnvelope) { x.ApproverRight = "payroll.prepare" }), nil), "submitPayrollRun", 403, peoplelink.CodeRunRight)
	e.problemIs(w.send(w.approved(otherRun, 1, "20000.00", "5100.00", func(x *peoplelink.RunEnvelope) { x.Organisation = uuid.NewString() }), nil), "submitPayrollRun", 403, peoplelink.CodeRunOrg)
	e.problemIs(w.send(w.approved(otherRun, 1, "20000.00", "5100.00", func(x *peoplelink.RunEnvelope) { x.MappingVersion = 7 }), nil), "submitPayrollRun", 409, peoplelink.CodeRunMapping)
	// A passkey that is not the approver's: the preparer's own, or an authenticator Fyniq never saw.
	stranger := passkeytest.New(t, w.pass.RPID, nemuraOrigin)
	good := w.approved(otherRun, 1, "20000.00", "5100.00", nil)
	rawEnv, _ := json.Marshal(good["envelope"])
	canonical, _ := peoplelink.Canonical(rawEnv)
	sum := sha256.Sum256(canonical)
	e.problemIs(w.send(w.signed(rawEnv, sum[:], stranger), nil), "submitPayrollRun", 403, peoplelink.CodeRunPasskey)
	// An approval made at the wrong origin, and one over another digest.
	wrongOrigin := *w.pass
	wrongOrigin.Origin = "https://evil.example.test"
	e.problemIs(w.send(w.signed(rawEnv, sum[:], &wrongOrigin), nil), "submitPayrollRun", 403, peoplelink.CodeRunPasskey)
	other := sha256.Sum256([]byte("another envelope"))
	over := w.signed(rawEnv, sum[:], w.pass)
	over["approval"] = w.signed(rawEnv, other[:], w.pass)["approval"]
	e.problemIs(w.send(over, nil), "submitPayrollRun", 403, peoplelink.CodeRunPasskey)
	// An approver with no sign-in identity in Fyniq.
	e.problemIs(w.send(w.approved(otherRun, 1, "20000.00", "5100.00", func(x *peoplelink.RunEnvelope) {
		x.Approver = peoplelink.Party{Issuer: "urn:nemura", Subject: "nobody"}
	}), nil),
		"submitPayrollRun", 403, peoplelink.CodeRunApprover)
	// Nemura's valid link signature does not make a run authoritative: but a bad link signature, an old epoch and a stale time are refused first.
	e.problemIs(w.send(good, func(h *peoplelink.Header) { h.Epoch = 2 }), "submitPayrollRun", 403, peoplelink.CodeEpoch)
	e.problemIs(w.send(good, func(h *peoplelink.Header) { h.SignedAt = e.clock.Now().Add(-time.Hour).UTC().Format(time.RFC3339) }), "submitPayrollRun", 403, peoplelink.CodeReplay)
	if w.entries() != count {
		t.Errorf("a refused run posted: %d entries, were %d", w.entries(), count)
	}
	// What was refused after the link's signature held is answered to Nemura with the refusal's code and plain words, never a figure; what was refused before (a bad link signature, an old epoch, a stale
	// time), an envelope used before and a version no newer than one posted are not answered: they say nothing about a run that is waiting.
	refusals := map[string]bool{}
	for _, q := range w.answers(t)[3:] {
		var r linksend.RunPosted
		_ = json.Unmarshal(q.Payload, &r)
		refusals[r.Code] = true
		if r.Status != "refused" || r.Message == "" || strings.Contains(r.Message, "20000") || strings.Contains(r.Message, "90000") || strings.Contains(r.Message, "5100") {
			t.Errorf("an answer to Nemura: %+v", r)
		}
	}
	for _, code := range []string{peoplelink.CodeRunEnvelope, peoplelink.CodeRunOrg, peoplelink.CodeRunSegregate, peoplelink.CodeRunRight, peoplelink.CodeRunMapping, peoplelink.CodeRunPasskey, peoplelink.CodeRunApprover} {
		if !refusals[code] {
			t.Errorf("a refusal %s was not answered to Nemura: %v", code, refusals)
		}
	}
	for _, code := range []string{peoplelink.CodeEpoch, peoplelink.CodeReplay, peoplelink.CodeSignature, peoplelink.CodeRunUsed, peoplelink.CodeRunStale} {
		if refusals[code] {
			t.Errorf("a refusal %s was answered to Nemura: it says nothing about a run that is waiting", code)
		}
	}
	// Only the link's client reaches it.
	for _, tok := range []string{w.accountant, w.payroll, e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: w.org, Roles: []authz.Role{authz.Owner}})} {
		if r := w.sendAs(tok, good, nil); r.Status != http.StatusForbidden {
			t.Errorf("a person's token reached the payroll operation: %d %s", r.Status, r.Raw)
		}
	}
	var refused int
	_ = e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		evs, err := u.Audit().Events(ctx, 0, 100000)
		for _, ev := range evs {
			if strings.Contains(ev.Body, `"payroll_run.refused"`) {
				refused++
				if strings.Contains(ev.Body, "90000") || strings.Contains(ev.Body, "20200") {
					t.Errorf("a refusal's audit event carries a figure: %s", ev.Body)
				}
			}
		}
		return err
	})
	if refused < 8 {
		t.Errorf("each refusal is audited: %d events", refused)
	}

	// A closed wage month: the difference posts in the current open month (FR-PPL-11).
	if err := e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		ps, err := u.Periods().List(ctx)
		for _, p := range ps {
			if p.StartsOn.Month() == time.October {
				_, err = u.Periods().Close(ctx, p.ID)
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e.clock.Set(time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC))
	v4 := w.approved(run, 4, "20300.00", "5134.00", nil)
	r4 := w.send(v4, nil)
	if r4.Status != http.StatusOK || r4.str("posting_date") != "2026-11-05" {
		t.Fatalf("version 4 in a closed month: %d %s", r4.Status, r4.Raw)
	}
	if got, want := w.entryLines(run, 4), "2100 -100.0000; 6011 100.0000"; got != want {
		t.Errorf("the closed month's difference: %s, want %s", got, want)
	}
}

// matrixPayrollRuns are the authorisation matrix's rows of the pay-run mapping, read and set under Pay runs' rights, and of the payroll operation, refused to
// every role (only the link's client calls it).
var matrixPayrollRuns = map[string]func(e *env, tok string) (response, func() bool){
	"getPayrollMapping": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/payroll-mapping", tok, nil), nil
	},
	"setPayrollMapping": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/payroll-mapping", tok, map[string]any{"entries": map[string]string{"deduction:advance_recovery": e.books.Bank.ID.String()}},
			"Prefer", "return=preview"), nil
	},
	"submitPayrollRun": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/people-link/payroll-runs", tok, map[string]any{}), nil
	},
}

// A person Nemura told Fyniq about is mapped to a Fyniq user explicitly, one user to one person, only a member, and no role comes with it (FR-PPL-18,
// NM-INT-12); an assertion that approved one run cannot approve another (its challenge is the run's digest), nor the same run again.
func TestAPersonIsMappedToAUserExplicitlyAndAnAssertionApprovesOnlyItsRun(t *testing.T) {
	w := payrollWorld(t)
	e := w.e
	person, other := uuid.NewString(), uuid.NewString()
	upsert := func(id string) {
		now := e.clock.Now().UTC().Format(time.RFC3339)
		raw, _ := json.Marshal(map[string]any{"person_id": id, "display_name": "Wei Jie Ong", "work_email": "weijie@example.com", "status": "active", "deleted": false})
		env, err := peoplelink.Sign(w.linkKey, peoplelink.Header{EventID: uuid.NewString(), Type: peoplelink.TypePersonUpserted, SchemaVersion: 1, SourceSystem: "nemura",
			SourceOrganisation: w.nemura, TargetOrganisation: w.org.String(), Epoch: 1, Sequence: 1, ObjectType: "person", ObjectID: id, ObjectVersion: 1, EffectiveAt: now,
			RecordedAt: now, SignedAt: now, CorrelationID: uuid.NewString()}, raw)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(env)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		if r := e.call(http.MethodPost, "/v1/people-link/projection-events", w.tok, body); r.Status != 200 {
			t.Fatalf("the person: %d %s", r.Status, r.Raw)
		}
	}
	upsert(person)
	upsert(other)
	owner := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: w.org, Roles: []authz.Role{authz.Administrator}})
	if l := e.call(http.MethodGet, "/v1/people-link/people", owner, nil); l.Status != 200 || len(l.Body["items"].([]any)) != 2 {
		t.Fatalf("the projection: %d %s", l.Status, l.Raw)
	}
	path := func(id string) string { return "/v1/people-link/people/" + id + "/user" }
	e.problemIs(e.call(http.MethodPost, path(person), owner, map[string]any{"user_id": uuid.NewString()}), "mapPersonToUser", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodPost, path(uuid.NewString()), owner, map[string]any{"user_id": w.approverUser.String()}), "mapPersonToUser", 404, CodeNotFound)
	if r := e.call(http.MethodPost, path(person), owner, map[string]any{"user_id": w.approverUser.String()}); r.Status != 200 || r.str("user_id") != w.approverUser.String() {
		t.Fatalf("mapping: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, path(other), owner, map[string]any{"user_id": w.approverUser.String()}); r.Status == 200 {
		t.Errorf("one user mapped to two people: %s", r.Raw)
	}
	for _, role := range []authz.Role{authz.Bookkeeper, authz.Employee} {
		tok := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: w.org, Roles: []authz.Role{role}})
		if r := e.call(http.MethodPost, path(person), tok, map[string]any{"user_id": w.approverUser.String()}); r.Status != http.StatusForbidden {
			t.Errorf("%s mapped a person: %d", role, r.Status)
		}
	}
	// An assertion made for one run's digest approves that run only.
	run := uuid.NewString()
	one := w.approved(run, 1, "20000.00", "5100.00", nil)
	if r := w.send(one, nil); r.Status != 200 {
		t.Fatalf("the first run: %d %s", r.Status, r.Raw)
	}
	replay := w.approved(run, 2, "20200.00", "5134.00", nil)
	replay["approval"] = one["approval"] // the first run's assertion on the second run's envelope
	e.problemIs(w.send(replay, nil), "submitPayrollRun", 403, peoplelink.CodeRunPasskey)
	e.problemIs(w.send(one, nil), "submitPayrollRun", 409, peoplelink.CodeRunUsed)
}

var matrixPeopleMapping = map[string]func(e *env, tok string) (response, func() bool){
	"listPeopleProjection": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/people-link/people", tok, nil), nil
	},
	"mapPersonToUser": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/people-link/people/"+uuid.NewString()+"/user", tok, map[string]any{"user_id": matrixApprover(e)}, "Prefer", "return=preview"), nil
	},
}

// answers are the answers to runs queued for Nemura so far, oldest first, as the worker would read them; the mapping's own events (people.department.changed) are listed by queued.
func (w *runWorld) answers(t *testing.T) []linksend.Queued {
	t.Helper()
	var out []linksend.Queued
	for _, q := range w.queued(t) {
		if q.Type == linksend.TypeRunPosted {
			out = append(out, q)
		}
	}
	return out
}

// queued are every event queued for Nemura so far, oldest first.
func (w *runWorld) queued(t *testing.T) []linksend.Queued {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, w.readonly) // the world's own database, which startSessions made fresh
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	tx, err := c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.org_id', $1, true)", w.org.String()); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `SELECT payload FROM fyniq.outbox WHERE kind = $1 ORDER BY created_at, id`, linksend.Kind)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []linksend.Queued
	for rows.Next() {
		var raw []byte
		var q linksend.Queued
		if err := rows.Scan(&raw); err != nil || json.Unmarshal(raw, &q) != nil {
			t.Fatalf("an answer row: %v", err)
		}
		out = append(out, q)
	}
	return out
}
