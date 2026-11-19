package authn

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A principal that is a person, or acts for one (the AI, an MCP call, a token), names that person for the database, so their
// department scope binds everything they read through the API (FR-ADM-01); a caller that is no person names no one.
// req: FR-ADM-01
// asvs: V8.2.1
func TestAPrincipalNamesItsPersonForTheDatabase(t *testing.T) {
	user := uuid.New()
	for name, p := range map[string]Principal{
		"a person":         {UserID: user, Class: ClassUser},
		"the AI for them":  {UserID: user, Class: ClassAgent, Client: "assistant"},
		"a token for them": {UserID: user, Class: ClassIntegration, Client: "token"},
	} {
		if got := storage.PersonFrom(WithPrincipal(context.Background(), p)); got != user {
			t.Errorf("%s: names %v, want %v", name, got, user)
		}
	}
	if got := storage.PersonFrom(WithPrincipal(context.Background(), Principal{Subject: "client", Class: ClassIntegration})); got != uuid.Nil {
		t.Errorf("a caller that is no person names %v", got)
	}
}
