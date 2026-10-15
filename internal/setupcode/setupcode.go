// Package setupcode makes and reads one-time codes: the first-run setup code (FR-ADM-06), and the
// Super admins' invitations, recovery codes and recovery confirmations (FR-ADM-09): 128 random bits as
// 26 base-32 characters, shown in groups for typing. Only a code's SHA-256 is stored; the code is shown
// once and never logged.
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

// RecoveryCodes is how many printed recovery codes a Super admin keeps (open question 36).
const RecoveryCodes = 10

// NewSet returns n new codes, grouped for display, and their hashes.
func NewSet(n int) (display []string, hashes [][]byte) {
	for range n {
		d, h := New()
		display, hashes = append(display, d), append(hashes, h)
	}
	return display, hashes
}
