// Package comment holds the rules of a comment on a record (PRD-001 FR-UX-13): its length, its characters and how many people it
// may mention. Who may read, write or be mentioned is the API's decision from the record's own row of Appendix D.
package comment

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The refusals, each with a plain message and a stable code (ENG-23).
const (
	CodeBody     = "CMT-01" // a comment is empty, too long or holds a control character
	CodeMentions = "CMT-02" // a comment names too many people or the same one twice
	CodeRecord   = "CMT-03" // a record that takes no comments
	CodeNoUser   = "CMT-04" // only a person who is a member comments
)

// MaxBody is the most characters a comment has; MaxMentions the most people it mentions.
const (
	MaxBody     = 2000
	MaxMentions = 10
)

// Problem is one refusal: the field it is about, a code, what is wrong and how to fix it.
type Problem struct{ Field, Code, Message, Fix string }

// Clean is body as it is kept: surrounding space trimmed, line breaks as \n, and a problem when it is empty, longer than MaxBody
// characters or holds a control character other than a line break or a tab.
func Clean(body string) (string, *Problem) {
	body = strings.TrimSpace(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(body))
	n := utf8.RuneCountInString(body)
	switch {
	case n == 0:
		return "", &Problem{"body", CodeBody, "is empty", "Write the comment"}
	case n > MaxBody:
		return "", &Problem{"body", CodeBody, fmt.Sprintf("has %d characters, and a comment has at most %d", n, MaxBody), "Shorten it, or split it into two comments"}
	}
	for _, r := range body {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return "", &Problem{"body", CodeBody, "holds a control character", "Send plain text"}
		}
	}
	return body, nil
}

// CheckMentions is nil when ids name at most MaxMentions distinct people.
func CheckMentions(ids []string) *Problem {
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return &Problem{"mention_ids", CodeMentions, "names the same person twice", "Mention each person once"}
		}
		seen[id] = true
	}
	if len(ids) > MaxMentions {
		return &Problem{"mention_ids", CodeMentions, fmt.Sprintf("names %d people, and a comment mentions at most %d", len(ids), MaxMentions), "Mention fewer people"}
	}
	return nil
}
