//go:build local

package nemura_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

// The CPF EZPay file of an approved version (NM-PAY-13, FR-PAY-09): the CPF Board's layout from the run's own figures, the employer's UEN from the letter settings, a person's CPF
// account number from their sealed NRIC with the read audited; a person with no NRIC is left out and listed with why; a version nobody approved, a company with no UEN, a key service
// that is missing and details out of form are refused; and the manifest holds the SHA-256 of the exact bytes and says the file is not proof of submission.
// req: NM-PAY-13
// req: FR-PAY-09
// asvs: V8.2.1, V8.3.1, V12.2.1
func TestTheCPFFileIsMadeFromAnApprovedVersionWithTheEmployersOwnDetails(t *testing.T) {
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
	noKeys := nemura.PayrollService{Store: st, Clock: clock.NewFrozen(now), Packs: packsSvc}
	prep := api.Session{Organisation: lt.c.org, Member: lt.c.pay}
	approver := api.Session{Organisation: lt.c.org, Member: lt.c.owner, StepUpAt: &now}
	birth := d("1990-05-20")
	gid, _ := svc.CreateGroup(ctx, prep, api.PayGroupInput{Name: "Monthly", PayDay: 25, CutoffDays: 7, ApprovalDays: 3})
	for _, p := range []uuid.UUID{lt.ben, lt.cy} {
		if err := svc.SetPay(ctx, prep, p, d("2026-01-05"), api.PersonPayInput{GroupID: gid, BasicMonthly: "4500.00", SHGFund: "CDAC"}); err != nil {
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
	good := api.ExportInput{Version: 1, Kind: "cpf", PaymentType: "PTE", Serial: "01", AdviceCode: "01"}
	if _, err := svc.Export(ctx, prep, runID, good); payCode(err) != payroll.CodeConflict {
		t.Errorf("a CPF file of a version nobody approved: %v", err)
	}
	if err := svc.Approve(ctx, approver, runID, 1, run.Latest.Digest); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Export(ctx, prep, runID, good); payCode(err) != payroll.CodeInvalid || !strings.Contains(err.Error(), "UEN") {
		t.Errorf("a company with no UEN: %v", err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetLetterSettings(ctx, tx, lt.c.org, now, lt.c.hr, nemura.LetterSettings{Name: "Acme Pte. Ltd.", UEN: "201912345A", Address: "1 Raffles Place, Singapore 048616"})
	})
	if _, err := noKeys.Export(ctx, prep, runID, good); err == nil || !strings.Contains(err.Error(), "NM_UNAVAILABLE") {
		t.Errorf("no key service: %v", err)
	}
	if _, err := svc.Export(ctx, prep, runID, api.ExportInput{Version: 1, Kind: "cpf", PaymentType: "pt", Serial: "01", AdviceCode: "01"}); payCode(err) != payroll.CodeInvalid {
		t.Errorf("a payment type of two letters: %v", err)
	}
	// nobody has an NRIC yet: nobody is put in the file
	if _, err := svc.Export(ctx, prep, runID, good); payCode(err) != payroll.CodeConflict {
		t.Errorf("nobody with an NRIC: %v", err)
	}
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetNRIC(ctx, tx, cipher, lt.c.org, now, lt.c.hr, lt.ben, "S7654321F")
	})
	f, err := svc.Export(ctx, prep, runID, good)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(f.ContentBase64)
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != f.SHA256 || f.Kind != "cpf" || f.Lines != 1 || !strings.Contains(f.Note, "not proof") || f.FileName != "201912345APTE01JUN202601.DTL" || f.Month != "2026-06-01" {
		t.Errorf("manifest: %+v", f)
	}
	if len(f.Skipped) != 1 || f.Skipped[0].Person != "Cy Wong" || !strings.Contains(f.Skipped[0].Reason, "No NRIC") {
		t.Errorf("skipped: %+v", f.Skipped)
	}
	recs := strings.Split(strings.TrimSuffix(string(raw), "\r\n"), "\r\n")
	for i, r := range recs {
		if len(r) != payroll.RecordLength {
			t.Errorf("record %d is %d bytes", i+1, len(r))
		}
	}
	// header, seven summaries, Ben's CPF record, his CDAC record, trailer
	if len(recs) != 11 || !strings.HasPrefix(recs[0], "F 201912345APTE01 0120261120"[:18]+"01") || recs[0][20:28] != "20261120" {
		t.Fatalf("the file has %d records: %q", len(recs), recs[0])
	}
	detail := recs[8]
	if detail[:2] != "F1" || detail[2:12] != "201912345A" || detail[20:26] != "202606" || detail[26:28] != "01" || detail[28:37] != "S7654321F" || detail[69:70] != "E" || strings.TrimRight(detail[70:136], " ") != "BEN LIM" {
		t.Errorf("Ben's detail record is %q", detail[:100])
	}
	// the amount is the run's own CPF for Ben, whole dollars in cents (4500.00 of ordinary wages at 20% + 17% is 1665)
	if detail[37:49] != "000000166500" || detail[49:59] != "0000450000" || detail[59:69] != "0000000000" {
		t.Errorf("Ben's amounts are %q %q %q", detail[37:49], detail[49:59], detail[59:69])
	}
	if recs[9][26:28] != "04" || recs[9][28:37] != "S7654321F" {
		t.Errorf("Ben's fund record is %q", recs[9][:60])
	}
	// the read of the NRIC is audited, and the trail holds neither the number nor an amount
	su := connect(t, db.SuperURL())
	var reads, leaks int
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND body LIKE '%NRIC or FIN%'`, lt.c.org).Scan(&reads); err != nil || reads == 0 {
		t.Errorf("audited NRIC reads: %d %v", reads, err)
	}
	if err := su.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND (body LIKE '%S7654321F%' OR body ~ '(^|[^0-9])1665([^0-9]|$)')`, lt.c.org).Scan(&leaks); err != nil || leaks != 0 {
		t.Errorf("the NRIC or an amount in the audit trail: %d %v", leaks, err)
	}
}
