package filedrop

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/bankfile"
	"github.com/cognizhi/fyniq/internal/ports"
)

// memory is a drop folder in memory.
type memory map[string][]byte

func (m memory) List(context.Context) ([]ports.DropFile, error) {
	var out []ports.DropFile
	for n, b := range m {
		out = append(out, ports.DropFile{Name: n, Size: int64(len(b))})
	}
	return out, nil
}

func (m memory) Read(_ context.Context, name string, max int64) ([]byte, error) {
	b, ok := m[name]
	if !ok || int64(len(b)) > max {
		return nil, errors.New("not there or too large")
	}
	return b, nil
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../bankfile/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The statement files in a drop folder are an account's transactions (FR-BNK-15, FR-BNK-17): the account the statement names, its lines with the bank's reference as their ID and the
// running balance left to the importer, taken in by file name after the cursor, one file's lines a page, with More while later files wait; with no cursor, lines before the day
// the source began are left out; a PGP-wrapped file and a larger one stop the sync with a plain reason; a folder it only reads.
// req: FR-BNK-15
// req: FR-BNK-17
func TestStatementFilesInADropFolderAreAnAccountsTransactions(t *testing.T) {
	ctx := context.Background()
	first := fixture(t, "swift.mt940")
	second := []byte(strings.ReplaceAll(strings.ReplaceAll(string(first), "BR2510", "BS2510"), "BR2512", "BS2512")) // another day's file of the same account
	drop := memory{"STMT_20251231.sta": first, "STMT_20260102.sta": second, "A-other-bank.sta": []byte("{1:F01XXXXSGSGAXXX0000000000}{2:I940XXXXSGSGXXXXN}{4:\n:20:A\n:25:OTHER/999\n:28C:1/1\n:60F:C251001SGD0,00\n:62F:C251001SGD0,00\n-}")}
	s := Statements{Drop: drop}
	accounts, err := s.Accounts(ctx)
	if err != nil || len(accounts) != 2 {
		t.Fatalf("accounts: %+v, %v", accounts, err)
	}
	var mine string
	for _, a := range accounts {
		if a.Currency == "SGD" && strings.HasSuffix(a.ID, "0721234567") {
			mine = a.ID
		}
	}
	if mine == "" {
		t.Fatalf("the account the statement names is not listed: %+v", accounts)
	}
	page, err := s.Transactions(ctx, mine, "", time.Time{})
	if err != nil || len(page.Transactions) != 3 || page.Cursor != "STMT_20251231.sta" || !page.More {
		t.Fatalf("the first page: %+v, %v", page, err)
	}
	if got := page.Transactions[0]; got.ID != "BR25100301" || got.Amount != "109.00" || got.Currency != "SGD" || !got.BookingDate.Equal(time.Date(2025, 10, 3, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("the first line: %+v", got)
	}
	next, err := s.Transactions(ctx, mine, page.Cursor, time.Time{})
	if err != nil || len(next.Transactions) != 3 || next.Cursor != "STMT_20260102.sta" || next.More || next.Transactions[0].ID != "BS25100301" {
		t.Fatalf("the next page: %+v, %v", next, err)
	}
	if done, err := s.Transactions(ctx, mine, next.Cursor, time.Time{}); err != nil || len(done.Transactions) != 0 || done.More || done.Cursor != next.Cursor {
		t.Errorf("after the last file: %+v, %v", done, err)
	}
	if early, err := s.Transactions(ctx, mine, "", time.Date(2025, 10, 4, 0, 0, 0, 0, time.UTC)); err != nil || len(early.Transactions) != 2 {
		t.Errorf("lines before the source began are left out: %+v, %v", early, err)
	}
	if none, err := s.Transactions(ctx, "no-such-account", "", time.Time{}); err != nil || len(none.Transactions) != 0 || none.More {
		t.Errorf("an account no file names: %+v, %v", none, err)
	}
	// What cannot be read stops the sync with its reason.
	if _, err := (Statements{Drop: memory{"x.sta.pgp": []byte("-----BEGIN PGP MESSAGE-----")}}).Accounts(ctx); !errors.Is(err, ErrEncrypted) {
		t.Errorf("a PGP file: %v", err)
	}
	if _, err := (Statements{Drop: memory{"bad.sta": []byte("not a statement")}}).Accounts(ctx); err == nil || !strings.Contains(err.Error(), "bad.sta") {
		t.Errorf("a file that is no statement: %v", err)
	}
	if _, err := (Statements{Drop: memory{"big.sta": make([]byte, MaxFileBytes+1)}}).Accounts(ctx); err == nil {
		t.Error("a file over the limit was read")
	}
	// Another format.
	camt := Statements{Drop: memory{"a.xml": fixture(t, "statement.camt053.xml")}, Format: bankfile.FormatCAMT053}
	if a, err := camt.Accounts(ctx); err != nil || len(a) != 1 {
		t.Errorf("camt.053: %+v, %v", a, err)
	}
}
