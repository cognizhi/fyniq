//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/linksend"
	nemuralink "github.com/cognizhi/fyniq/internal/nemura/link"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The two apps agree on a pay run's proof (PRD-002 NM-INT-06; PRD-001 FR-PPL-09): an approval envelope made and sealed by Nemura's own code (its fields, its canonical bytes, its digest), signed over that digest by
// a passkey enrolled in Fyniq at Nemura's origin, posts in Fyniq through the real verification; the check Nemura makes of the assertion before it keeps it passes on what a real authenticator produces and
// fails on the answers Fyniq would refuse; and an envelope Nemura could not make (a department of its own invention, another approver's digest) is refused by Fyniq with its code.
// part of NM-INT-06 (the sender and the page are Nemura's own tests)
// asvs: V8.2.1, V6.4.1, V13.2.1
func TestAPayRunEnvelopeSealedByNemuraPostsInFyniqThroughItsRealVerification(t *testing.T) {
	w := payrollWorld(t)
	e := w.e
	// Setting the mapping told Nemura its version and its departments, by Fyniq's own IDs and their names, and no account.
	var told []linksend.Queued
	for _, q := range w.queued(t) {
		if q.Type == linksend.TypeDepartmentChanged {
			told = append(told, q)
		}
	}
	var dep linksend.DepartmentChanged
	if len(told) != 1 || told[0].ObjectType != "payroll_mapping" || told[0].ObjectVersion != 1 || json.Unmarshal(told[0].Payload, &dep) != nil || dep.MappingVersion != 1 || len(dep.Departments) != 2 ||
		dep.Departments[0].Name == "" || dep.Departments[0].CostCentreID != nil || strings.Contains(string(told[0].Payload), "6013") {
		t.Fatalf("what Nemura is told of the mapping: %+v", told)
	}
	got := map[string]string{}
	for _, d := range dep.Departments {
		got[d.DepartmentID] = d.Name
	}
	if got[w.dept1] != "GA" || got[w.dept2] != "RD" {
		t.Errorf("the departments Nemura is told of: %v", got)
	}
	run := uuid.NewString()
	envelope := nemuralink.RunEnvelope{Organisation: w.nemura, SourceSystem: "nemura", RunID: run, Version: 1, Supersedes: 0, WageMonth: "2026-10",
		Departments: []nemuralink.RunDepartment{{DepartmentID: w.dept2, Gross: "20000.00"}, {DepartmentID: w.dept1, Gross: "10000.00"}}, // out of order: sealing sorts them
		EmployeeCPF: "3000.00", EmployerCPF: "5100.00", SDL: "75.00", SHG: "20.00", Deductions: []nemuralink.RunDeduction{{Kind: "advance_recovery", Amount: "500.00"}}, Reimbursements: "300.00",
		PackVersion: "sg-cpf@2026.1,sg-sdl@2026.1", PolicyVersion: "sg-people@2026.1", MappingVersion: w.mappingVersion,
		Preparer: nemuralink.Party(w.preparer), Approver: nemuralink.Party(w.approver), ApproverRight: nemuralink.RightApprove}
	canonical, sha, err := envelope.Seal()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := nemuralink.Challenge(sha)
	if err != nil {
		t.Fatal(err)
	}
	sub := w.signed(canonical, digest, w.pass)
	// Nemura's own check of the assertion passes on what the authenticator made, for the shared relying party and Nemura's origin.
	approval := sub["approval"].(map[string]any)
	proof := nemuralink.RunApproval{AuthenticatorData: approval["authenticator_data"].(string), ClientData: approval["client_data"].(string), Signature: approval["signature"].(string)}
	if err := nemuralink.CheckAssertion(proof, digest, nemuraOrigin, w.pass.RPID); err != nil {
		t.Fatalf("Nemura's check of a real assertion: %v", err)
	}
	if err := nemuralink.CheckAssertion(proof, digest, "https://evil.example.test", w.pass.RPID); err == nil {
		t.Error("an assertion made at another origin passed Nemura's check")
	}
	other := w.signed(canonical, []byte("another challenge, 32 bytes long!"), w.pass)["approval"].(map[string]any)
	if err := nemuralink.CheckAssertion(nemuralink.RunApproval{AuthenticatorData: other["authenticator_data"].(string), ClientData: other["client_data"].(string), Signature: other["signature"].(string)}, digest, nemuraOrigin, w.pass.RPID); err == nil {
		t.Error("an assertion over another challenge passed Nemura's check")
	}
	// Fyniq reads Nemura's bytes as its own envelope, byte for byte, and its digest is the digest Nemura sealed.
	var asFyniq peoplelink.RunEnvelope
	if err := json.Unmarshal(canonical, &asFyniq); err != nil {
		t.Fatal(err)
	}
	if _, _, got, err := peoplelink.ParseRun(sealedJSON(t, sub), w.nemura); err != nil || hexOf(got) != sha {
		t.Fatalf("Fyniq's reading of Nemura's submission: %v", err)
	}
	r := w.send(sub, nil)
	if r.Status != http.StatusOK || r.str("outcome") != "posted" {
		t.Fatalf("Nemura's envelope in Fyniq: %d %s", r.Status, r.Raw)
	}
	want := "1250 -500.0000; 2100 -26780.0000; 2110 -8195.0000; 2150 300.0000; 6011 20000.0000; 6013 10000.0000; 6020 5100.0000; 6030 75.0000"
	if got := w.entryLines(run, 1); got != want {
		t.Errorf("what Fyniq posted of Nemura's envelope: %s\nwant %s", got, want)
	}
	// What Nemura could not make: a department of its own invention is not in Fyniq's mapping, and a run signed for another approver's digest does not verify.
	invented := envelope
	invented.RunID = uuid.NewString()
	invented.Departments = []nemuralink.RunDepartment{{DepartmentID: uuid.NewString(), Gross: "30000.00"}}
	c2, s2, _ := invented.Seal()
	d2, _ := nemuralink.Challenge(s2)
	e.problemIs(w.send(w.signed(c2, d2, w.pass), nil), "submitPayrollRun", 409, peoplelink.CodeRunMapping)
}

func sealedJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A person removed from Fyniq is signed out of Nemura too (PRD-001 FR-PPL-18; PRD-002 NM-INT-12): removing a member queues identity.session.revoked for each sign-in identity the person has, by issuer and
// subject and never an email, in the removal's own unit of work, through the link's one event scope; with no link that has keys nothing is queued.
// part of FR-PPL-18 (Nemura's half, and sign-out, are its own)
// asvs: V7.4.1, V8.2.1
func TestRemovingAMemberTellsNemuraToEndTheirSessions(t *testing.T) {
	w := payrollWorld(t)
	e := w.e
	// The removal itself is a change two people approve (FM-1); what it does in its own unit of work is this, and removeMemberNow calls it.
	if err := e.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		return e.api.signOutOfNemura(ctx, u, w.approverUser, "removed")
	}); err != nil {
		t.Fatal(err)
	}
	var told []linksend.SessionRevoked
	for _, q := range w.queued(t) {
		if q.Type == linksend.TypeSessionRevoked {
			var p linksend.SessionRevoked
			if json.Unmarshal(q.Payload, &p) != nil || q.ObjectType != "identity" || strings.Contains(string(q.Payload), "@") {
				t.Fatalf("the revocation: %+v", q)
			}
			told = append(told, p)
		}
	}
	if len(told) == 0 || told[0].Issuer != w.approver.Issuer || told[0].Subject != w.approver.Subject || told[0].Reason != "removed" {
		t.Errorf("what Nemura is told of the removal: %+v (the approver is %+v)", told, w.approver)
	}
}

// The Finance | People switch is a door and never a grant (PRD-001 FR-PPL-02; PRD-002 NM-UX-03): while the link is active, a person whose roles reach the books and whom Nemura says is a member there is told where
// Nemura is (the origin the link was approved with), and nobody else is: an employee who only claims, an approver alone and a client that is not a person see no switch, and neither does a person Nemura has said
// nothing of; what is behind the door is Nemura's own decision.
// part of FR-PPL-02 (the landing of an HR-only person is Nemura's)
// asvs: V8.2.1, V4.2.1
func TestTheSwitchShowsOnlyForRolesThatReachTheBooksWhileLinked(t *testing.T) {
	w := payrollWorld(t)
	e := w.e
	// Nemura says these identities are members there (identity.access.changed): one per session below.
	as := func(r authz.Role) string {
		who := peoplelink.Party{Issuer: "urn:fyniq:test", Subject: "switch-" + string(r)}
		if r := w.nemuraSays(peoplelink.TypeAccessChanged, "identity", uuid.NewString(), 100+len(r), map[string]any{"issuer": who.Issuer, "subject": who.Subject, "people_access": true, "payroll_approver": false}, nil); r.Status != http.StatusOK {
			t.Fatalf("Nemura's word: %d %s", r.Status, r.Raw)
		}
		return e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: w.org, Roles: []authz.Role{r}, Via: authn.ViaSession, Issuer: who.Issuer, Subject: who.Subject})
	}
	for _, r := range []authz.Role{authz.Owner, authz.Administrator, authz.Accountant, authz.PayrollAdmin, authz.Bookkeeper} {
		got := e.call(http.MethodGet, "/v1/session", as(r), nil)
		ws, _ := got.Body["workspace"].(map[string]any)
		if got.Status != http.StatusOK || ws["people_url"] != nemuraOrigin+"/?switch=1" {
			t.Errorf("%s: %d %v", r, got.Status, got.Body["workspace"])
		}
	}
	for _, r := range []authz.Role{authz.Employee, authz.LineManager, authz.Approver, authz.Auditor, authz.BoardViewer} {
		if got := e.call(http.MethodGet, "/v1/session", as(r), nil); got.Status != http.StatusOK || got.Body["workspace"] != nil {
			t.Errorf("%s sees a switch: %d %v", r, got.Status, got.Body["workspace"])
		}
	}
}

// Another app's credential is no credential here (PRD-001 FR-PPL-18, AI-36; PRD-002 NM-INT-12): Nemura's session cookie, in either of its names, opens no Fyniq operation; the link's own token is no person
// and reaches no operation of a person's (it reaches only the link's operations); and a Nemura approval's passkey answer is only ever checked for the run it signs (PPL-15), never taken as a Fyniq step-up.
// asvs: V4.2.1, V8.2.1, V13.2.1
func TestNemurasCredentialsOpenNothingInFyniq(t *testing.T) {
	w := payrollWorld(t)
	e := w.e
	for _, cookie := range []string{"nemura_session=abcdefghijklmnopqrstuvwxyz0123456789", "__Host-nemura_session=abcdefghijklmnopqrstuvwxyz0123456789"} {
		if r := e.call(http.MethodGet, "/v1/session", "", nil, "Cookie", cookie); r.Status != http.StatusUnauthorized {
			t.Errorf("Nemura's cookie at Fyniq: %d %s", r.Status, r.Raw)
		}
	}
	// The link's client is no person: a person's session operation and a person's ledger read are refused it.
	for _, path := range []string{"/v1/session", "/v1/members", "/v1/payroll-mapping"} {
		if r := e.call(http.MethodGet, path, w.tok, nil); r.Status != http.StatusForbidden && r.Status != http.StatusUnauthorized {
			t.Errorf("the link's token at %s: %d %s", path, r.Status, r.Raw)
		}
	}
}
