// Package banking is bank import, matching and reconciliation (PRD-001 §8.4; p1-bank-import-reconcile): the import of
// statements into money accounts with their duplicate and continuity checks (FR-BNK-02, FR-BNK-04, FR-BNK-16), bank
// rules with their back-test (FR-BNK-05), and, as later tasks land, matching, transfers, auto-reconcile and
// reconciliation. Its commands run in the caller's unit of work, so a preview rolls back what an import would add.
package banking

import (
	"fmt"
	"strings"
)

// The stable codes of the banking commands (ENG-23).
const (
	CodeAccount  = "BANK-01" // the statement is for no money account here, or for another than the one chosen
	CodeCurrency = "BANK-02" // the statement's currency is not its account's
	CodeClosed   = "BANK-03" // the money account is closed
	CodeFile     = "BANK-04" // the file cannot be read: the reader's refusal, with its line and field
	CodeRequest  = "BANK-05" // the request is incomplete, such as a CSV without its account
	CodeRule     = "BANK-06" // a bank rule's name, conditions or action do not fit, or no longer read
)

// Error is a refusal with a stable code, what is wrong and how to fix it.
type Error struct {
	Code    string
	Message string
	Fix     string
	Err     error // the reader's refusal, for CodeFile
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

func (e *Error) Unwrap() error { return e.Err }

func refuse(code, fix, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Fix: fix}
}

// identifier is an account identifier compared as banks write it: without spaces, hyphens, dots or slashes, in upper
// case, so "072-123456-7" and "0721234567" are one account.
func identifier(s string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '.', '/':
			return -1
		}
		return r
	}, s))
}

// last4 shows an identifier's last four characters, as messages and the audit trail keep it (SEC-15).
func last4(s string) string {
	id := identifier(s)
	if len(id) <= 4 {
		return id
	}
	return "…" + id[len(id)-4:]
}
