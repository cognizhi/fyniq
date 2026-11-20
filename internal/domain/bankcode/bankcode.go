// Package bankcode derives a Singapore bank's BIC from its bank code (PRD-001 FR-PPL-12, FR-PAY-11; p1-payroll-link design D26, owner's decision of 21 Nov 2026).
//
// Nemura keeps a person's bank by its code and name, with the account number sealed; Fyniq writes the payment file and needs the BIC, so it looks the BIC up here. The table is effective-dated
// data with its source, like a statutory pack: a code that is not in it gives no BIC, and a person fills the BIC in when preparing the batch (the batch's lines wait until they do).
//
// [verify] The codes and BICs below are the banks' published ones as the build recalled them; confirm each against the Association of Banks in Singapore's GIRO bank and branch code list and the
// bank's own SWIFT/BIC entry before the first payroll goes live, and add every bank the founders use. An unlisted bank is never guessed.
package bankcode

import (
	"regexp"
	"strings"
	"time"
)

// Source says where the table comes from.
const Source = "Association of Banks in Singapore, GIRO bank and branch codes (https://www.abs.org.sg); each bank's published SWIFT BIC [verify]"

// Bank is one row: its name, its code (four digits, or a code with its three-digit branch) and its BIC.
type Bank struct {
	Code string
	Name string
	BIC  string
	From time.Time // effective from
	To   time.Time // zero: no end
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

var from2026 = day(2026, time.January, 1)

// Table is the effective-dated list.
var Table = []Bank{
	{Code: "7171", Name: "DBS Bank / POSB", BIC: "DBSSSGSG", From: from2026},
	{Code: "7339", Name: "Oversea-Chinese Banking Corporation", BIC: "OCBCSGSG", From: from2026},
	{Code: "7375", Name: "United Overseas Bank", BIC: "UOVBSGSG", From: from2026},
	{Code: "9496", Name: "Standard Chartered Bank (Singapore)", BIC: "SCBLSG22", From: from2026},
	{Code: "7214", Name: "Citibank Singapore", BIC: "CITISGSG", From: from2026},
	{Code: "7232", Name: "The Hongkong and Shanghai Banking Corporation, Singapore", BIC: "HSBCSGS2", From: from2026},
	{Code: "7302", Name: "Malayan Banking (Maybank Singapore)", BIC: "MBBESGS2", From: from2026},
	{Code: "7986", Name: "CIMB Bank (Singapore)", BIC: "CIBBSGSG", From: from2026},
}

var (
	codeRE = regexp.MustCompile(`^[0-9]{4}([0-9]{3})?$`)
	bicRE  = regexp.MustCompile(`^[A-Z0-9]{8}([A-Z0-9]{3})?$`)
)

// ValidCode says whether s is shaped like a bank code: four digits, or four and the three of a branch.
func ValidCode(s string) bool { return codeRE.MatchString(s) }

// ValidBIC says whether s is shaped like a BIC: eight or eleven letters and digits.
func ValidBIC(s string) bool { return bicRE.MatchString(s) }

// BIC is the BIC of the bank with this code on the day, and whether the table has it. A seven-digit code is its bank's four with a branch.
func BIC(code string, on time.Time) (string, bool) {
	code = strings.TrimSpace(code)
	if !ValidCode(code) {
		return "", false
	}
	for _, b := range Table {
		if b.Code == code[:4] && !on.Before(b.From) && (b.To.IsZero() || on.Before(b.To)) {
			return b.BIC, true
		}
	}
	return "", false
}
