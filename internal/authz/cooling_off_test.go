package authz

import "testing"

// Every operation the cooling-off after a recovery holds is a real one, and none of the recovery's own: a typo would
// leave a change open, and holding the recovery or a new passkey would leave the person no way back (SEC-35).
// req: SEC-35
// asvs: V6.4.2
func TestTheCoolingOffHoldsRealOperations(t *testing.T) {
	for _, op := range HeldInCoolingOff {
		if _, ok := Policies[op]; !ok {
			t.Errorf("the cooling-off holds %q, which no policy names", op)
		}
	}
	for _, open := range []string{"recoverAccount", "startPasskeyRegistration", "registerPasskey", "replaceMyRecoveryCodes", "getSession"} {
		for _, op := range HeldInCoolingOff {
			if op == open {
				t.Errorf("the cooling-off holds %s, which the recovered person needs", op)
			}
		}
	}
}
