// Package setupcode makes and reads the one-time first-run setup code (FR-ADM-06): 128 random bits as
// 26 base-32 characters, shown in groups for typing. Only its SHA-256 is stored; the code is printed
// once, at deployment, and never logged.
package setupcode

import (
	"crypto/rand"
	"crypto/sha256"
	"strings"
)

// New returns a new code, grouped for display, and its hash.
func New() (display string, hash []byte) {
	code := rand.Text() // 26 characters of A–Z and 2–7: 130 bits from crypto/rand
	var b strings.Builder
	for i := 0; i < len(code); i += 5 {
		if i > 0 {
			b.WriteByte('-')
		}
		b.WriteString(code[i:min(i+5, len(code))])
	}
	return b.String(), Hash(code)
}

// Hash returns the SHA-256 of a code as typed: case, spaces and dashes do not matter.
func Hash(typed string) []byte {
	clean := strings.NewReplacer("-", "", " ", "", "\t", "").Replace(strings.ToUpper(strings.TrimSpace(typed)))
	sum := sha256.Sum256([]byte(clean))
	return sum[:]
}
