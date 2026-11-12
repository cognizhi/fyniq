package audit

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
)

// SEC-15: logs and audit events never contain secrets, tokens, full NRIC or bank numbers. Values
// under a sensitive field name are masked by name; NRIC/FIN numbers and tokens are also found by
// their shape in any text. AUD-02 (PRD-001 rev 0.230, proof only): before that, Prove replaces each
// identifier and pay value with the organisation's keyed fingerprint, so a change, and a change back,
// is proved without the value.

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
		if f, ok := x["fingerprint"].(string); ok && strings.HasPrefix(f, FingerprintPrefix) {
			return x // a proof (AUD-02): its base64url could look like a token to the patterns below
		}
		if _, _, ok := SealedOf(x); ok {
			return x // a sealed personal value (§11.8): its base64 could look like an NRIC to the patterns below
		}
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

// payKeys are field names whose values are a person's pay. They match whole names only: a forecast's
// salary_increase is a rate, not anyone's pay.
var payKeys = []string{"salary", "monthly_salary", "annual_salary", "gross_salary", "wage", "wages", "gross_pay", "basic_pay", "net_pay"}

// Sensitive reports whether a field's value is proved by its fingerprint, never kept (AUD-02): an identifier
// (the names masked to their last four characters) or a person's pay.
func Sensitive(key string) bool {
	if keyIs(key, numberKeys) {
		return true
	}
	k := strings.ToLower(key)
	for _, p := range payKeys {
		if k == p {
			return true
		}
	}
	return false
}

// Fingerprint is the organisation's keyed hash of a value (ports.Cipher.Fingerprint within the organisation's
// audit scope): equal for equal values, and useless without the key.
type Fingerprint func(ctx context.Context, value []byte) ([]byte, error)

// FingerprintPrefix starts every proof's fingerprint: its format, HMAC-SHA256 in unpadded base64url.
const FingerprintPrefix = "fp1:"

// Prove replaces every sensitive value in a change set, at any depth, with its proof: {"fingerprint": "fp1:…"} and,
// for an identifier, its last four characters as "hint". A value already proved, an absent one and an empty
// string stay as they are. It runs before masking, which would otherwise fingerprint the mask.
func Prove(ctx context.Context, changes map[string]Change, fp Fingerprint) (map[string]Change, error) {
	out := make(map[string]Change, len(changes))
	for k, c := range changes {
		before, err := prove(ctx, k, c[0], fp)
		if err != nil {
			return nil, err
		}
		after, err := prove(ctx, k, c[1], fp)
		if err != nil {
			return nil, err
		}
		out[k] = Change{before, after}
	}
	return out, nil
}

func prove(ctx context.Context, key string, v any, fp Fingerprint) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		if _, done := x["fingerprint"]; done {
			return x, nil
		}
		out := make(map[string]any, len(x))
		for k, val := range x {
			p, err := prove(ctx, k, val, fp)
			if err != nil {
				return nil, err
			}
			out[k] = p
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			p, err := prove(ctx, key, val, fp)
			if err != nil {
				return nil, err
			}
			out[i] = p
		}
		return out, nil
	case bool:
		return x, nil
	}
	if !Sensitive(key) {
		return v, nil
	}
	s, ok := v.(string)
	if !ok {
		s = fmt.Sprint(v) // a pay figure as a number: proved as its text
	}
	if s == "" {
		return s, nil
	}
	mac, err := fp(ctx, []byte(s))
	if err != nil {
		return nil, fmt.Errorf("audit: fingerprint %s: %w", key, err)
	}
	p := map[string]any{"fingerprint": FingerprintPrefix + base64.RawURLEncoding.EncodeToString(mac)}
	if keyIs(key, numberKeys) {
		p["hint"] = lastFour(s)
	}
	return p, nil
}
