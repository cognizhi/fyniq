package compliance

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The nightly look at integration clients finds the ones with no active owner, a bad class, no or a passed end date, or a scope their
// class does not hold, and leaves a revoked client and a sound one alone (CTL-API-02, SEC-07).
// req: CTL-API-02
func TestIntegrationClientsMustHaveAnOwnerAClassAnEndAndTheirClassScopes(t *testing.T) {
	owner := uuid.New()
	since := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	future := since.AddDate(0, 6, 0)
	revoked := since
	sound := storage.APIClient{ClientID: "sound", Class: "integration", OwnerID: owner, ExpiresAt: future, Scopes: []string{"fyniq.read.sales", "fyniq.write.sales"}}
	clients := []storage.APIClient{
		sound,
		{ClientID: "ownerless", Class: "integration", ExpiresAt: future, Scopes: []string{"fyniq.read.sales"}},
		{ClientID: "stranger", Class: "integration", OwnerID: uuid.New(), ExpiresAt: future},
		{ClientID: "classless", Class: "robot", OwnerID: owner, ExpiresAt: future},
		{ClientID: "forever", Class: "integration", OwnerID: owner},
		{ClientID: "lapsed", Class: "integration", OwnerID: owner, ExpiresAt: since.AddDate(0, -1, 0)},
		{ClientID: "agent-writer", Class: "agent", OwnerID: owner, ExpiresAt: future, Scopes: []string{"fyniq.read.reports", "fyniq.write.sales"}},
		{ClientID: "gone", Class: "integration", RevokedAt: &revoked},
	}
	found, live := integrationFindings(clients, map[uuid.UUID]bool{owner: true}, since)
	got := map[string]bool{}
	for _, f := range found {
		got[f.ID] = true
	}
	for _, id := range []string{"ownerless", "stranger", "classless", "forever", "lapsed", "agent-writer"} {
		if !got[id] {
			t.Errorf("%s was not found", id)
		}
	}
	if got["sound"] || got["gone"] || live != 7 {
		t.Errorf("a sound or revoked client was found, or the count is %d: %+v", live, found)
	}
}
