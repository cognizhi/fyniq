package hydra

import (
	"os"
	"strings"
	"testing"
)

// The local stack runs the Hydra the tests run (exit item 6 is proven against the pinned version).
// req: NFR-IMG-03
func TestImageMatchesTheLocalStack(t *testing.T) {
	compose, err := os.ReadFile("../../../deploy/local/compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// Fyniq's oauth and oauth-migrate, and Nemura's own instance of the authorisation server, nemura-oauth and nemura-oauth-migrate (PRD-002 question 11).
	if n := strings.Count(string(compose), "image: "+Image); n != 4 {
		t.Fatalf("deploy/local/compose.yaml should run %s for oauth, oauth-migrate, nemura-oauth and nemura-oauth-migrate; found it %d times", Image, n)
	}
}
