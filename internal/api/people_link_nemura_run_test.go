//go:build local

package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/linksend"
	nemuralink "github.com/cognizhi/fyniq/internal/nemura/link"
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
