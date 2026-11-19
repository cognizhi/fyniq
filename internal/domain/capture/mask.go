package capture

import (
	"regexp"
	"strings"

	"github.com/cognizhi/fyniq/internal/domain/evidence"
)

// The masker (PRD-001 AI-02, AI-20, FR-DOC-09; the owner's decision of 20 Nov 2026): a document's text is masked BEFORE the model reads it. No full bank account
// number, IBAN or NRIC/FIN goes to a model; the person keys a supplier's bank details themselves, and a payee's bank details are never the AI's to set (§9.6). The
// masking is by shape and by the words around a number, and errs on the side of masking: a long run of digits that is not a UEN, an amount or a date is masked too,
// so a long invoice number may be hidden, which the person types in.

const (
	// NRICMask and BankMask replace what was masked.
	NRICMask = "[NRIC removed]"
	BankMask = "[bank number removed]"
)

var (
	nricToken = regexp.MustCompile(`(?i)\b[STFGM]\d{7}[A-Z]\b`)
	// An IBAN: two letters, two check digits, then 11 to 30 letters and digits, with the spaces printed in groups of four.
	ibanToken = regexp.MustCompile(`\b[A-Z]{2}\d{2}(?: ?[A-Z0-9]{4}){2,7}(?: ?[A-Z0-9]{1,4})?\b`)
	// A run of digits, with hyphens or spaces between groups.
	digitRun = regexp.MustCompile(`\d(?:[ -]?\d)+`)
	// The words that say a number on the same line is a bank detail.
	bankWord = regexp.MustCompile(`(?i)\b(account|acct|a/c|iban|bank|swift|bic|paynow|giro|beneficiary|sort code|routing)\b`)
)

func digitsIn(s string) int {
	n := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n++
		}
	}
	return n
}

// MaskText hides the NRIC and FIN numbers, IBANs and bank account numbers in text, and says how many things it hid. A number is a bank account number when it follows a
// bank word on its line (four digits or more), or is nine digits or more and is not a UEN (a letter right after it), an amount (a decimal point) or a date.
func MaskText(text string) (string, int) {
	masked := 0
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = nricToken.ReplaceAllStringFunc(line, func(m string) string {
			if evidence.ValidNRIC(m) {
				masked++
				return NRICMask
			}
			return m
		})
		line = ibanToken.ReplaceAllStringFunc(line, func(m string) string {
			if digitsIn(m) >= 4 && len(strings.ReplaceAll(m, " ", "")) >= 15 {
				masked++
				return BankMask
			}
			return m
		})
		onBankLine := bankWord.MatchString(line)
		var out strings.Builder
		last := 0
		for _, loc := range digitRun.FindAllStringIndex(line, -1) {
			run := line[loc[0]:loc[1]]
			n := digitsIn(run)
			after := ""
			if loc[1] < len(line) {
				after = line[loc[1] : loc[1]+1]
			}
			before := ""
			if loc[0] > 0 {
				before = line[loc[0]-1 : loc[0]]
			}
			uen := len(after) == 1 && after[0] >= 'A' && after[0] <= 'Z' && n >= 8
			decimal := after == "." && loc[1]+1 < len(line) && line[loc[1]+1] >= '0' && line[loc[1]+1] <= '9' || before == "."
			hide := (onBankLine && n >= 4) || (n >= 9 && !uen && !decimal)
			if hide && !strings.Contains(run, "[") {
				masked++
				out.WriteString(line[last:loc[0]] + BankMask)
				last = loc[1]
			}
		}
		out.WriteString(line[last:])
		lines[i] = out.String()
	}
	return strings.Join(lines, "\n"), masked
}
