//go:build local

package nemura_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// The IR8A / Auto-Inclusion Scheme payload of an income year (NM-PAY-14, FR-PAY-10): from the approved runs paid in the year, each person's pay items in the IR8A's boxes and the year's
// employee CPF, the NRIC from its sealed form and the sex and address from theirs (each read audited, none in the trail), a person with no particulars left out and listed with why, a
// payload that breaks a data-item rule refused with the rule named, and the manifest holding the SHA-256 of the exact bytes.
// req: NM-PAY-14
// req: FR-PAY-10
// asvs: V8.2.1, V8.3.1, V12.2.1
func TestTheIR8APayloadIsMadeFromTheYearsApprovedRuns(t *testing.T) {
	ctx := context.Background()
	lt := newLeaveTeam(t)
	now := d("2026-11-20").Add(10 * time.Hour)
	st := store(t, "nemura_api")
	packsSvc, err := nemura.NewPacksService(st, clock.NewFrozen(now))
	if err != nil {
		t.Fatal(err)
	}
	cipher, _ := local.NewFieldKeys(bytes.Repeat([]byte{7}, 64))
	svc := nemura.PayrollService{Store: st, Clock: clock.NewFrozen(now), Packs: packsSvc, Cipher: cipher}
	restricted := nemura.RestrictedService{Store: st, Clock: clock.NewFrozen(now), Cipher: cipher}
	prep := api.Session{Organisation: lt.c.org, Member: lt.c.pay}
	hr := api.Session{Organisation: lt.c.org, Member: lt.c.hr}
	approver := api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}
	birth := d("1990-05-20")
	gid, _ := svc.CreateGroup(ctx, prep, api.PayGroupInput{Name: "Monthly", PayDay: 25, CutoffDays: 7, ApprovalDays: 3})
	for _, p := range []uuid.UUID{lt.ben, lt.cy} {
		if err := svc.SetPay(ctx, prep, p, d("2026-01-05"), api.PersonPayInput{GroupID: gid, BasicMonthly: "4500.00"}); err != nil {
			t.Fatal(err)
		}
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.SetClassification(ctx, tx, lt.c.org, now, lt.c.owner, p, d("2026-01-06"), people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen", BirthDate: &birth})
		})
	}
	run, err := svc.StartRun(ctx, prep, d("2026-06-01"), mustUUID(gid))
	if err != nil {
		t.Fatal(err)
	}
	runID := mustUUID(run.ID)
	if err := svc.Approve(ctx, approver, runID, 1, run.Latest.Digest); err != nil {
		t.Fatal(err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetLetterSettings(ctx, tx, lt.c.org, now, lt.c.hr, nemura.LetterSettings{Name: "Acme Pte. Ltd.", UEN: "201912345A", Address: "1 Raffles Place, Singapore 048616"})
	})
	good := api.ExportInput{Version: 1, Kind: "ais", AuthorisedPerson: "John Adams", ContactNumber: "91234567", Email: "abc@email.com.sg"}
	// nobody has an NRIC or particulars yet
	if _, err := svc.Export(ctx, prep, runID, good); payCode(err) != payroll.CodeConflict {
		t.Errorf("nobody with an NRIC: %v", err)
	}
	for _, p := range []uuid.UUID{lt.ben, lt.cy} {
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.SetNRIC(ctx, tx, cipher, lt.c.org, now, lt.c.hr, p, map[uuid.UUID]string{lt.ben: "S7654321F", lt.cy: "S7654322B"}[p])
		})
	}
	if err := restricted.SetIR8AParticulars(ctx, hr, lt.ben, api.IR8AParticulars{Sex: "M", AddressType: "L", Address1: "1 Raffles Place", PostalCode: "048616"}); err != nil {
		t.Fatal(err)
	}
	if err := restricted.SetIR8AParticulars(ctx, hr, lt.ben, api.IR8AParticulars{Sex: "X"}); err == nil {
		t.Error("a sex of X is accepted")
	}
	// the particulars are sealed: the database holds no plain address
	su := connect(t, db.SuperURL())
	var plain int
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.person_secret WHERE organisation_id = $1 AND position('Raffles'::bytea IN ir8a_sealed) > 0`, lt.c.org).Scan(&plain); err != nil || plain != 0 {
		t.Errorf("an address in the clear: %d %v", plain, err)
	}
	f, err := svc.Export(ctx, prep, runID, good)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(f.ContentBase64)
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != f.SHA256 || f.Kind != "ais" || f.Lines != 1 || f.FileName != "ir8a-2026.json" || f.Total != "4500.00" {
		t.Errorf("manifest: %+v", f)
	}
	if len(f.Skipped) != 1 || f.Skipped[0].Person != "Cy Wong" || !strings.Contains(f.Skipped[0].Reason, "particulars") {
		t.Errorf("skipped: %+v", f.Skipped)
	}
	var got struct {
		Org    map[string]any   `json:"orgAndSubmissionInfo"`
		People []map[string]any `json:"employeePersonalParticulars"`
	}
	if err := json.Unmarshal(raw, &got); err != nil || len(got.People) != 1 {
		t.Fatalf("payload: %v %s", err, raw)
	}
	b, ir := got.People[0], got.People[0]["ir8a"].(map[string]any)
	if got.Org["incomeYear"] != float64(2026) || got.Org["employerTaxRefNumber"] != "201912345A" || got.Org["totalNumberIR8A"] != float64(1) || got.Org["amountTotalIncomeOfIR8A"] != float64(4500) ||
		b["idNumber"] != "S7654321F" || b["idType"] != "1" || b["sex"] != "M" || b["addressType"] != "L" || b["postalCode"] != "048616" || b["dateOfBirth"] != "1990-05-20" ||
		ir["salary"] != float64(4500) || ir["cpfOrDesignatedFund"] != float64(900) || ir["totalIncome"] != float64(4500) || ir["periodPaymentToDate"] != "2026-12-31" {
		t.Errorf("payload: %s", raw)
	}
	// a rule of IRAS's data items broken: the refusal names it
	if err := restricted.SetIR8AParticulars(ctx, hr, lt.ben, api.IR8AParticulars{Sex: "M", AddressType: "L", Address1: "N.A.", PostalCode: "048616"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Export(ctx, prep, runID, good); payCode(err) != payroll.CodeInvalid || !strings.Contains(err.Error(), "B8") {
		t.Errorf("an address of N.A.: %v", err)
	}
	bad := good
	bad.ContactNumber = ""
	if _, err := svc.Export(ctx, prep, runID, bad); payCode(err) != payroll.CodeInvalid || !strings.Contains(err.Error(), "A6") {
		t.Errorf("no contact number: %v", err)
	}
	// the reads are audited; the trail holds neither the NRIC nor the address
	var reads, leaks int
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND body LIKE '%NRIC or FIN%'`, lt.c.org).Scan(&reads); err != nil || reads == 0 {
		t.Errorf("audited reads: %d %v", reads, err)
	}
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND (body LIKE '%S7654321F%' OR body LIKE '%Raffles%' OR body LIKE '%048616%')`, lt.c.org).Scan(&leaks); err != nil || leaks != 0 {
		t.Errorf("personal data in the audit trail: %d %v", leaks, err)
	}
}
