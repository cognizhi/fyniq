package payroll_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

// The sample file on the last page of CPF Board's "CPF EZPay (FTP) File Specifications (effective from 16 January 2025)", as printed, comments and all (its notes
// say comments must not appear in a submission file: they sit in the name's columns, and the tests compare the record up to the name and the name apart from them).
const boardSample = `F 234567891APTE01 0420180129183315FTP.DTL
F0234567891APTE01 04201801010000005180000000000
F0234567891APTE01 04201801020000000019500000001
F0234567891APTE01 04201801030000000007000000001
F0234567891APTE01 04201801040000000001000000001
F0234567891APTE01 04201801050000000009000000001
F0234567891APTE01 04201801100000000000000000000
F0234567891APTE01 04201801110000000000000000000
F1234567891APTE01 0420180101S1122334A00000011100000003000000000000000LMICKEY TAN AH TAN   Mickey  CPF con = $1110
F1234567891APTE01 0420180104S1122334A00000000010000000000000000000000 MICKEY TAN AH TAN      CDAC = $1.00
F1234567891APTE01 0420180101S2122334B00000011100000003000000000000000NJACKIE JACK   Jackie CPF con = $1110
F1234567891APTE01 0420180105S2122334B00000000090000000000000000000000 JACKIE JACK    ECF = $9.00
F1234567891APTE01 0420180101S3122334C00000014800000004000000000000000ERAVIDAVI SINGH S/O RAVIDAVI SINGH   Ravidavi Singh CPF con = $1480
F1234567891APTE01 0420180103S3122334C00000000070000000000000000000000 RAVIDAVI SINGH S/O RAVIDAVI SINGH    SINDA = $7.00
F1234567891APTE01 0420180101S4122334D00000014800000004000000000000000EMUHAMMED ALI BIN MUHAMMED ALI   Muhammed Ali  CPF con = $1480
F1234567891APTE01 0420180102S4122334D00000000195000000000000000000000 MUHAMMED ALI BIN MUHAMMED ALI    MBMF = $19.50
F9234567891APTE01 040000017000000000521650`

func boardEmployer() payroll.CPFEmployer {
	return payroll.CPFEmployer{ID: "234567891A", PaymentType: "PTE", Serial: "01", AdviceCode: "04", Month: time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC),
		Made: time.Date(2018, 1, 29, 18, 33, 15, 0, time.UTC)}
}

func boardPeople() []payroll.CPFPerson {
	return []payroll.CPFPerson{
		{Account: "S1122334A", Name: "Mickey Tan Ah Tan", Status: "L", OW: payroll.D("3000"), AW: payroll.D("0"), CPF: payroll.D("1110"), Fund: "CDAC", SHG: payroll.D("1")},
		{Account: "S2122334B", Name: "Jackie Jack", Status: "N", OW: payroll.D("3000"), AW: payroll.D("0"), CPF: payroll.D("1110"), Fund: "ECF", SHG: payroll.D("9")},
		{Account: "S3122334C", Name: "Ravidavi Singh S/O Ravidavi Singh", Status: "E", OW: payroll.D("4000"), AW: payroll.D("0"), CPF: payroll.D("1480"), Fund: "SINDA", SHG: payroll.D("7")},
		{Account: "S4122334D", Name: "Muhammed Ali Bin Muhammed Ali", Status: "E", OW: payroll.D("4000"), AW: payroll.D("0"), CPF: payroll.D("1480"), Fund: "MBMF", SHG: payroll.D("19.50")},
	}
}

// The Board's own sample, rebuilt from its people: every record is 150 bytes and agrees with the sample column for column (up to the name, and the name with its
// comments left out), the summaries and the trailer's totals included (5216.50 over 17 records).
// req: FR-PAY-09
// req: NM-PAY-13
func TestTheBoardsSampleIsRebuiltColumnForColumn(t *testing.T) {
	file, err := payroll.CPFFile(boardEmployer(), boardPeople())
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(file), "\r\n"), "\r\n")
	want := strings.Split(boardSample, "\n")
	if len(got) != len(want) {
		t.Fatalf("%d records, the sample has %d:\n%s", len(got), len(want), file)
	}
	for i := range want {
		if len(got[i]) != payroll.RecordLength {
			t.Errorf("record %d is %d bytes, not %d", i+1, len(got[i]), payroll.RecordLength)
		}
		// the sample's detail records hold the name from column 71 and its comment after it
		cut := 70
		if len(want[i]) < cut {
			cut = len(want[i])
		}
		if got[i][:cut] != want[i][:cut] {
			t.Errorf("record %d differs in its first %d columns:\n got  %q\n want %q", i+1, cut, got[i][:cut], want[i][:cut])
		}
		if want[i][1] == '1' { // a detail: the name, apart from the comment
			name := strings.TrimRight(got[i][70:136], " ")
			if !strings.HasPrefix(strings.TrimRight(want[i][70:], " "), name) {
				t.Errorf("record %d: the name %q is not what the sample starts with: %q", i+1, name, want[i][70:])
			}
		}
	}
	// the sample's own arithmetic: the trailer's count and amount, and the summaries it sums
	if !strings.HasPrefix(got[16], "F9234567891APTE01 04"+"0000017"+"000000000521650") {
		t.Errorf("the trailer is %q", got[16])
	}
	if payroll.CPFFileName(boardEmployer()) != "234567891APTE01JAN201804.DTL" {
		t.Errorf("the file name is %s", payroll.CPFFileName(boardEmployer()))
	}
}

// What the specification forbids is refused, never repaired: negative or fractional-cent amounts, a character it lists, an account that is not S or T with a check
// letter, a CPF figure that is not whole dollars (RND-CPF made it so) and an employer detail out of form.
// req: FR-PAY-09
func TestTheFileRefusesWhatTheSpecificationForbids(t *testing.T) {
	one := func(f func(*payroll.CPFPerson)) error {
		p := boardPeople()[0]
		f(&p)
		_, err := payroll.CPFFile(boardEmployer(), []payroll.CPFPerson{p})
		return err
	}
	for name, f := range map[string]func(*payroll.CPFPerson){
		"a negative wage":       func(p *payroll.CPFPerson) { p.OW = payroll.D("-1") },
		"a third decimal":       func(p *payroll.CPFPerson) { p.AW = payroll.D("10.005") },
		"a forbidden character": func(p *payroll.CPFPerson) { p.Name = "Tan; Ah Kow" },
		"a bad account number":  func(p *payroll.CPFPerson) { p.Account = "F1122334A" },
		"a CPF figure in cents": func(p *payroll.CPFPerson) { p.CPF = payroll.D("1110.50") },
		"a status of its own":   func(p *payroll.CPFPerson) { p.Status = "X" },
		"a name too long":       func(p *payroll.CPFPerson) { p.Name = strings.Repeat("A", 67) },
		"a non-ASCII name":      func(p *payroll.CPFPerson) { p.Name = "Tan Ah Kow \u00e9" },
		"an unknown fund":       func(p *payroll.CPFPerson) { p.Fund = "RICE" },
	} {
		if err := one(f); err == nil {
			t.Errorf("a person with %s was written", name)
		}
	}
	e := boardEmployer()
	e.PaymentType = "pt"
	if _, err := payroll.CPFFile(e, boardPeople()); err == nil {
		t.Error("an employer with a payment type of two letters was written")
	}
	if _, err := payroll.CPFFile(boardEmployer(), nil); err == nil {
		t.Error("a file with nobody in it was written")
	}
}

// A person with wages only (additional wages in the month, a leaver in the month) keeps both wage columns, and the levy is a summary of its own.
// req: FR-PAY-09
func TestAdditionalWagesAndTheLevyHaveTheirColumns(t *testing.T) {
	e := boardEmployer()
	e.SDL = payroll.D("11.25")
	file, err := payroll.CPFFile(e, []payroll.CPFPerson{{Account: "T0112233Z", Name: "Lim Mei Ling", Status: "O", OW: payroll.D("3500.50"), AW: payroll.D("1200.25"), CPF: payroll.D("1739")}})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(file), "\r\n"), "\r\n")
	if len(lines) != 10 { // header, seven summaries, one detail, trailer
		t.Fatalf("%d records", len(lines))
	}
	if !strings.HasPrefix(lines[7], "F0234567891APTE01 0420180111"+"000000001125"+"0000000") {
		t.Errorf("the levy's summary is %q", lines[7][:50])
	}
	d := lines[8]
	if d[49:59] != "0000350050" || d[59:69] != "0000120025" || d[69:70] != "O" || d[37:49] != "000000173900" {
		t.Errorf("the detail is %q", d[:100])
	}
	if !strings.HasPrefix(lines[9], "F9234567891APTE01 04"+"0000010"+"000000000175025") {
		t.Errorf("the trailer is %q", lines[9][:50])
	}
}
