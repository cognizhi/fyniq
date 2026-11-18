// Package neverlist is the never-list of PRD-001 §9.6, as words: what the AI and MCP clients may never do, whatever a
// catalogue file or a tool definition says. It is its own small package so that the proposal catalogue (which refuses a task
// type on it as it loads, and again as each proposal is made) and the MCP server's tool surface (which refuses a tool on it
// as the manifest is built) share one list, and so that the MCP server's binary carries no domain command to read it.
package neverlist

import (
	"slices"
	"strings"
	"unicode"
)

// never is the never-list (§9.6, "Never in the catalogue"): words, or runs of words, a task type's ID or command may
// not hold, with what they name. The proposal service refuses them as the catalogue loads and again as each proposal is made, so adding one
// to the catalogue proposes nothing.
var never = []struct {
	words []string
	what  string
}{
	{[]string{"approve", "approval", "approvals", "reject", "sign", "signature"}, "approving or rejecting anything"},
	{[]string{"release", "pay", "payment", "initiate"}, "releasing or initiating payments"},
	{[]string{"payee", "payees", "bank details", "bank account"}, "creating or changing payee bank details"},
	{[]string{"user", "users", "role", "roles", "member", "members", "team mode", "policy", "policies", "catalogue", "setting", "settings"},
		"changing users, roles, team mode, approval policies, the AI catalogue or AI settings"},
	{[]string{"statutory", "pack", "packs", "parameter", "parameters"}, "changing statutory parameters"},
	{[]string{"reopen", "unlock"}, "reopening periods"},
	{[]string{"void", "delete", "remove", "purge"}, "voiding or deleting posted records"},
	{[]string{"lodge", "file", "filing", "submit"}, "filing or lodging with an authority"},
	{[]string{"export"}, "exporting the organisation's data"},
	{[]string{"audit"}, "the audit trail"},
	// (rev 0.3) equity and a director's balance; (rev 0.6) officers, registers and an obligation marked met (AI-66).
	{[]string{"share", "shares", "shareholder", "shareholders", "holder", "holders", "instrument", "instruments", "investor", "investors",
		"equity", "allot", "allotment"}, "issuing or transferring shares, or changing holders, instruments or investor rights"},
	{[]string{"director", "directors", "repay", "repayment", "convert", "conversion", "waive", "waiver"},
		"repaying, converting or waiving a director's balance"},
	{[]string{"officer", "officers", "register", "registers", "met", "fulfil", "fulfilled"},
		"changing officers or registers, or marking an obligation as met"},
}

// Refuse is what on the never-list a task type, a command or a tool would do, or "" when it does none of it: by the
// words of its ID and of its command.
func Refuse(id, command string) string {
	for _, words := range [][]string{splitWords(id), splitWords(command)} {
		for _, n := range never {
			for _, w := range n.words {
				if holds(words, strings.Fields(w)) {
					return n.what
				}
			}
		}
	}
	return ""
}

// holds is whether words hold run, one word after another.
func holds(words, run []string) bool {
	for i := 0; i+len(run) <= len(words); i++ {
		if slices.Equal(words[i:i+len(run)], run) {
			return true
		}
	}
	return false
}

// splitWords are an ID's or a command's words, lower-case: sec.create_task → sec create task; CreateComplianceTask →
// create compliance task.
func splitWords(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '.' || r == '_' || r == '-' || r == ' ':
			flush()
		case unicode.IsUpper(r):
			flush()
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}
