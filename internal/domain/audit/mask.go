package audit

import (
	"regexp"
	"strings"
)

// SEC-15: logs and audit events never contain secrets, tokens, full NRIC or bank numbers. Values
// under a sensitive field name are masked by name; NRIC/FIN numbers and tokens are also found by
// their shape in any text. AUD-02 (P1) adds encrypted or keyed-hash values so a change can still be
// proved without exposing it.

// secretKeys are field names whose values are removed entirely.
var secretKeys = []string{"password", "secret", "token", "api_key", "apikey", "private_key", "authorization", "cookie", "credential"}

// numberKeys are field names whose values keep only their last four characters.
var numberKeys = []string{"nric", "fin", "bank_account", "account_number", "iban", "card_number", "paynow"}

var (
	nricPattern   = regexp.MustCompile(`\b[STFGMstfgm]\d{7}[A-Za-z]\b`)
	jwtPattern    = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*`)
	bearerPattern = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
	keyPattern    = regexp.MustCompile(`\b(?:sk|pk|rk|ghp|gho|xox[abp])[-_][A-Za-z0-9_-]{12,}\b`)
)

// Redacted replaces a secret's value.
const Redacted = "[redacted]"

// keyIs reports whether a field name contains one of the listed names as whole words:
// client_secret and bank_account_number match, but definition does not match fin.
func keyIs(key string, list []string) bool {
	k := "_" + strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, key) + "_"
	for _, s := range list {
		if strings.Contains(k, "_"+s+"_") {
			return true
		}
	}
	return false
}

// lastFour keeps the last four characters of an identifier: S1234567D → *****567D.
func lastFour(s string) string {
	r := []rune(s)
	if len(r) <= 4 {
		return strings.Repeat("*", len(r))
	}
	return strings.Repeat("*", len(r)-4) + string(r[len(r)-4:])
}

// RedactText masks NRIC/FIN numbers and removes tokens found anywhere in free text.
func RedactText(s string) string {
	s = nricPattern.ReplaceAllStringFunc(s, lastFour)
	s = jwtPattern.ReplaceAllString(s, Redacted)
	s = bearerPattern.ReplaceAllString(s, "Bearer "+Redacted)
	return keyPattern.ReplaceAllString(s, Redacted)
}

// MaskValue masks v as the value of a field called key, looking inside objects and lists.
func MaskValue(key string, v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		switch {
		case keyIs(key, secretKeys):
			return Redacted
		case keyIs(key, numberKeys):
			return lastFour(x)
		}
		return RedactText(x)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = MaskValue(k, val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = MaskValue(key, val)
		}
		return out
	default:
		if keyIs(key, secretKeys) {
			return Redacted
		}
		return v
	}
}

// MaskChanges masks every before and after value of a change set.
func MaskChanges(changes map[string]Change) map[string]Change {
	out := make(map[string]Change, len(changes))
	for k, c := range changes {
		out[k] = Change{MaskValue(k, c[0]), MaskValue(k, c[1])}
	}
	return out
}
