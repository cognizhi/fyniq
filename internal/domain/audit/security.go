package audit

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
)

// Security events (AUD-06, PRD-001 rev 0.230; p1-audit-explorer design D2) keep their IP address and user agent beside
// the chain, fingerprinted in their body as request.detail, so the details can be erased after the security-log
// retention while the event stays and verifies. They are sign-ins and sign-outs, changes to how a person signs in,
// recoveries, a Super admin's installation events, every reveal of a full identifier and every export (AUD-05).

// securityPrefixes and securitySuffixes name the security events' actions.
var (
	securityPrefixes = []string{"session.", "identity.", "passkey.", "sign_in_method.", "recovery_codes.", "account_recovery.",
		"account.recovered", "super_admin.", "report_export."}
	securitySuffixes = []string{".exported", "_revealed", "_downloaded"}
	// securityExact are reads of a file whose action says neither (AUD-05): opening an evidence file.
	securityExact = []string{"evidence.opened"}
)

// IsSecurity reports whether an action is a security event's.
func IsSecurity(action string) bool {
	for _, p := range securityPrefixes {
		if strings.HasPrefix(action, p) {
			return true
		}
	}
	for _, s := range securitySuffixes {
		if strings.HasSuffix(action, s) {
			return true
		}
	}
	return slices.Contains(securityExact, action)
}

// LooksSecurity is what a security event's action looks like. An action it matches must be a security event
// (IsSecurity), which the conventions test holds for every action the code writes.
var LooksSecurity = regexp.MustCompile(`(^|[._])(sign_in|signin|sign_out|signout|session|passkey|identity|recover\w*|login|logout|export\w*|reveal\w*|download\w*|super_admin|device|factor|totp|step_up)([._]|$)`)

// DetailValue is what a security event's request.detail fingerprints: its IP address and user agent, as a JSON pair,
// so no two pairs read alike.
func DetailValue(ip, userAgent string) []byte {
	b, _ := json.Marshal([2]string{ip, userAgent}) // two strings always marshal
	return b
}

// Clip shortens s to at most n bytes at a character boundary: a user agent is kept to 1,000 bytes and an IP address to
// 64 (000285).
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8Start(s[n]) {
		n--
	}
	return s[:n]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
