//go:build local

package banking_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/pgtest"
	"github.com/cognizhi/fyniq/internal/bankfile"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

var now = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

type world struct {
	store    *postgres.Store
	org      uuid.UUID
	sgd, usd storage.MoneyAccount
}

func start(t *testing.T) world {
	t.Helper()
	ctx := context.Background()
	db, err := pgtest.Start(ctx)
	if err != nil {
		t.Fatalf("the import tests need Docker: %v", err)
	}
	t.Cleanup(db.Stop)
	st, err := postgres.Open(ctx, db.URL("fyniq_api"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	org, err := st.CreateOrganisation(ctx, storage.NewOrganisation{Name: "Import Pte Ltd", BaseCurrency: "SGD"})
	if err != nil {
		t.Fatal(err)
	}
	w := world{store: st, org: org.ID}
	if err := st.Do(ctx, org.ID, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, x := range []struct {
			code, name, cur, ident string
			dst                    *storage.MoneyAccount
		}{{"1010", "DBS current", "SGD", "072-123456-7", &w.sgd}, {"1021", "DBS USD", "USD", "072-999999-9", &w.usd}} {
			a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: x.code, Name: x.name, Type: storage.Asset,
				AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: x.cur}})
			if err != nil {
				return err
			}
			if *x.dst, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: a.ID, Kind: storage.MoneyBank, Name: x.name,
				Identifier: x.ident, Currency: x.cur, At: now}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w world) imp(t *testing.T, r banking.ImportRequest) ([]banking.ImportedStatement, error) {
	t.Helper()
	if r.Source == "" {
		r.Source = storage.MoneySourceFile
	}
	if r.At.IsZero() {
		r.At = now
	}
	var out []banking.ImportedStatement
	err := w.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		out, err = banking.Import(ctx, u, r)
		return err
	})
	return out, err
}

func code(err error) string {
	var e *banking.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// mt940 writes one MT940 statement for account, opening and closing in SGD-style comma decimals, with lines
// "YYMMDD C|D amount ref" separated by semicolons.
func mt940(account, cur, open, close, lines string) string {
	var b strings.Builder
	fmt.Fprintf(&b, ":20:S\n:25:%s\n:60F:%s\n", account, open)
	for _, l := range strings.Split(lines, ";") {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		ref := "NONREF"
		bank := ""
		if len(f) > 3 {
			bank = "//" + f[3]
		}
		fmt.Fprintf(&b, ":61:%s%s%sNTRF%s%s\n:86:%s\n", f[0], f[1], f[2], ref, bank, "LINE "+f[0])
	}
	fmt.Fprintf(&b, ":62F:%s\n", close)
	_ = cur
	return b.String()
}

// One file brings statements for several accounts at once, each matched to its money account by the identifier the
// file names and its currency; a file for another account, or for none here, is refused, as is a CSV without the
// account it is for and a file the reader cannot read.
// req: FR-BNK-02
// req: FR-BNK-15
func TestStatementsForSeveralAccountsGoToTheirOwn(t *testing.T) {
	w := start(t)
	file := mt940("0721234567", "SGD", "C251001SGD1000,00", "C251031SGD1109,00", "251003 C 109,00 BR1") +
		mt940("DBSSSGSG/072-999999-9", "USD", "C251001USD0,00", "C251031USD1000,00", "251004 C 1000,00 BR2")
	got, err := w.imp(t, banking.ImportRequest{Format: bankfile.FormatMT940, Data: []byte(file)})
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v, %v", got, err)
	}
	if got[0].Account.ID != w.sgd.ID || got[1].Account.ID != w.usd.ID || got[0].Added != 1 || got[1].Added != 1 || got[0].Statement == nil {
		t.Fatalf("each to its own: %+v / %+v", got[0], got[1])
	}
	usd := w.usd.ID
	if _, err := w.imp(t, banking.ImportRequest{Format: bankfile.FormatMT940, Data: []byte(mt940("0721234567", "SGD", "C251101SGD1109,00", "C251130SGD1109,00", "")),
		Account: &usd}); code(err) != banking.CodeAccount {
		t.Fatalf("a file for another account than the one chosen: %v", err)
	}
	if _, err := w.imp(t, banking.ImportRequest{Format: bankfile.FormatMT940, Data: []byte(mt940("999", "SGD", "C251101SGD0,00", "C251130SGD0,00", ""))}); code(err) != banking.CodeAccount {
		t.Fatalf("a file for no money account here: %v", err)
	}
	if _, err := w.imp(t, banking.ImportRequest{Format: bankfile.FormatCSV, Data: []byte("Date,Amount\n01/11/2025,1.00\n")}); code(err) != banking.CodeRequest {
		t.Fatalf("a CSV without its account: %v", err)
	}
	if _, err := w.imp(t, banking.ImportRequest{Format: bankfile.FormatMT940, Data: []byte("not MT940")}); code(err) != banking.CodeFile {
		t.Fatalf("a file the reader refuses: %v", err)
	}
}

// Each statement's opening balance must be the previous one's closing balance: a break is recorded as a gap, never
// filled, and the account's data is complete only to the day before it; a statement that overlaps one held is left to
// the duplicate check.
// req: FR-BNK-16
func TestABreakInTheBalanceChainIsAGap(t *testing.T) {
	w := start(t)
	oct := mt940("0721234567", "SGD", "C251001SGD1000,00", "C251031SGD1109,00", "251003 C 109,00 BR1")
	nov := mt940("0721234567", "SGD", "C251101SGD900,00", "C251130SGD950,00", "251105 C 50,00 BR2") // 1109.00 expected
	for _, f := range []string{oct, nov} {
		if _, err := w.imp(t, banking.ImportRequest{Format: bankfile.FormatMT940, Data: []byte(f)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		gaps, err := u.Banking().Gaps(ctx, w.sgd.ID, true)
		if err != nil {
			return err
		}
		if len(gaps) != 1 || gaps[0].ExpectedOpening == nil || *gaps[0].ExpectedOpening != "1109.0000" || *gaps[0].FoundOpening != "900.0000" {
			t.Errorf("gaps: %+v", gaps)
		}
		f, err := banking.FreshnessOf(ctx, u, w.sgd.ID)
		if err != nil {
			return err
		}
		if f.OpenGaps != 1 || f.CompleteTo == nil || f.CompleteTo.Format("2006-01-02") != "2025-10-31" || f.LastImport == nil {
			t.Errorf("freshness: %+v", f)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// G4-042: two legitimate S$500.00 receipts on one day with one reference in a statement with running balances are both
// kept; importing the same file again adds nothing, nor does a source covering the same days; a look-alike without the
// bank's reference or a running balance is kept and marked possible duplicate, never dropped silently.
// req: FR-BNK-04
// req: FR-BNK-16
func TestDuplicatesAreDroppedOnlyOnProof(t *testing.T) {
	w := start(t)
	file := mt940("0721234567", "SGD", "C251001SGD0,00", "C251031SGD1000,00", "251003 C 500,00 INV7;251003 C 500,00 INV7")
	first, err := w.imp(t, banking.ImportRequest{Format: bankfile.FormatMT940, Data: []byte(file)})
	if err != nil || first[0].Added != 2 || first[0].Flagged != 0 {
		t.Fatalf("both receipts kept: %+v, %v", first, err)
	}
	again, err := w.imp(t, banking.ImportRequest{Format: bankfile.FormatMT940, Data: []byte(file)})
	if err != nil || again[0].Dropped != 2 || again[0].Statement != nil {
		t.Fatalf("the same file again: %+v, %v", again, err)
	}
	ofx := `<OFX><BANKMSGSRSV1><STMTTRNRS><STMTRS><CURDEF>SGD<BANKACCTFROM><ACCTID>0721234567</BANKACCTFROM><BANKTRANLIST>
<STMTTRN><DTPOSTED>20251003<TRNAMT>500.00<FITID>INV7</STMTTRN><STMTTRN><DTPOSTED>20251003<TRNAMT>500.00<FITID>INV7</STMTTRN>
</BANKTRANLIST><LEDGERBAL><BALAMT>1000.00</LEDGERBAL></STMTRS></STMTTRNRS></BANKMSGSRSV1></OFX>`
	overlap, err := w.imp(t, banking.ImportRequest{Format: bankfile.FormatOFX, Data: []byte(ofx)})
	if err != nil || overlap[0].Dropped != 2 || overlap[0].Statement != nil {
		t.Fatalf("an overlapping source: %+v, %v", overlap, err)
	}
	sgd := w.sgd.ID
	csv := "Date,Amount\n03/10/2025,500.00\n"
	look, err := w.imp(t, banking.ImportRequest{Format: bankfile.FormatCSV, Data: []byte(csv), Account: &sgd,
		Mapping: &bankfile.Mapping{DateFormats: []string{"02/01/2006"}, Date: "Date", Amount: "Amount"}})
	if err != nil || look[0].Flagged != 1 || look[0].Lines[0].DuplicateOf == nil {
		t.Fatalf("a look-alike without proof: %+v, %v", look, err)
	}
	if err := w.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) error {
		lines, err := u.Banking().Lines(ctx, storage.BankLineFilter{MoneyAccountID: &sgd, Statuses: []string{storage.LinePossibleDuplicate}})
		if err == nil && len(lines) != 1 {
			t.Errorf("possible duplicates: %+v", lines)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// A line that may be a transfer's other side is named (FR-BNK-04: a transfer already recorded from the other account),
// so it is not taken for new money.
// req: FR-BNK-04
// req: FR-BNK-09
func TestATransfersOtherSideIsRecognised(t *testing.T) {
	w := start(t)
	sgdFile := mt940("0721234567", "SGD", "C251001SGD2000,00", "C251031SGD650,00", "251004 D 1350,00 TR1")
	got, err := w.imp(t, banking.ImportRequest{Format: bankfile.FormatMT940, Data: []byte(sgdFile)})
	if err != nil {
		t.Fatal(err)
	}
	var tr storage.BankTransfer
	if err := w.store.Do(context.Background(), w.org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		tr, err = u.BankMatching().RecordTransfer(ctx, storage.NewBankTransfer{FromAccountID: w.sgd.ID, ToAccountID: w.usd.ID,
			FromLineID: got[0].Lines[0].ID, OnDate: time.Date(2025, 10, 4, 0, 0, 0, 0, time.UTC), FromCurrency: "SGD", FromAmount: "1350.00",
			ToCurrency: "USD", ToAmount: "1000.00", At: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	usdFile := mt940("0729999999", "USD", "C251001USD0,00", "C251031USD1000,00", "251006 C 1000,00 WISE1")
	in, err := w.imp(t, banking.ImportRequest{Format: bankfile.FormatMT940, Data: []byte(usdFile)})
	if err != nil || in[0].Lines[0].Transfer == nil || *in[0].Lines[0].Transfer != tr.ID {
		t.Fatalf("the transfer's other side: %+v, %v", in, err)
	}
}
