package perf

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// Statement is an OFX bank statement of n synthetic lines for the account identifier, in currency, over the days before
// end: the 10,000 lines NFR-PERF-04 imports. Each line has its own FITID, so none is a duplicate of another.
func Statement(identifier, currency string, n int, end time.Time, seed uint64) []byte {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) // #nosec G404 -- synthetic amounts, not a secret
	start := end.AddDate(0, 0, -89)
	var b strings.Builder
	b.WriteString("OFXHEADER:100\nDATA:OFXSGML\nVERSION:102\nSECURITY:NONE\nENCODING:USASCII\nCHARSET:1252\nCOMPRESSION:NONE\n" +
		"OLDFILEUID:NONE\nNEWFILEUID:NONE\n\n<OFX>\n")
	fmt.Fprintf(&b, "<SIGNONMSGSRSV1><SONRS><STATUS><CODE>0<SEVERITY>INFO</STATUS><DTSERVER>%s<LANGUAGE>ENG</SONRS></SIGNONMSGSRSV1>\n",
		end.Format("20060102150405"))
	b.WriteString("<BANKMSGSRSV1><STMTTRNRS><TRNUID>1<STATUS><CODE>0<SEVERITY>INFO</STATUS>\n<STMTRS>\n")
	fmt.Fprintf(&b, "<CURDEF>%s\n<BANKACCTFROM><BANKID>7171<ACCTID>%s<ACCTTYPE>CHECKING</BANKACCTFROM>\n<BANKTRANLIST>\n", currency, identifier)
	fmt.Fprintf(&b, "<DTSTART>%s\n<DTEND>%s\n", start.Format("20060102"), end.Format("20060102"))
	var balance int64
	for i := range n {
		cents := int64(r.IntN(99_900) + 100)
		kind := "DEBIT"
		if i%3 == 0 {
			kind = "CREDIT"
		} else {
			cents = -cents
		}
		balance += cents
		day := start.AddDate(0, 0, i*90/max(n, 1))
		fmt.Fprintf(&b, "<STMTTRN><TRNTYPE>%s<DTPOSTED>%s<TRNAMT>%s<FITID>PERF-%d-%d<NAME>PERF PAYEE %d<MEMO>perf line %d</STMTTRN>\n",
			kind, day.Format("20060102"), decimal(cents), seed, i, i%250, i)
	}
	fmt.Fprintf(&b, "</BANKTRANLIST>\n<LEDGERBAL><BALAMT>%s<DTASOF>%s</LEDGERBAL>\n</STMTRS>\n</STMTTRNRS></BANKMSGSRSV1>\n</OFX>\n",
		decimal(balance), end.Format("20060102"))
	return []byte(b.String())
}

// decimal is minor units as a plain decimal with two places and an ASCII minus.
func decimal(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}
