package wiring

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/hitpay"
	"github.com/cognizhi/fyniq/internal/adapters/stripe"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
)

// A provider event the parser calls handled names an object the processor fetches and hands to a rule: a kind with no rule
// would park every such event for a person (UC-PSP-01), so each kind an adapter handles has its handler here.
func TestEveryHandledProviderEventKindHasAHandler(t *testing.T) {
	handlers := (&Kit{}).PSPHandlers(clock.NewFrozen(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)))
	for name, kinds := range map[string][]ports.ProviderKind{"stripe": stripe.HandledKinds(), "hitpay": hitpay.HandledKinds()} {
		if len(kinds) == 0 {
			t.Errorf("%s handles no event kind", name)
		}
		for _, k := range kinds {
			if handlers[k] == nil {
				t.Errorf("%s handles %s events, and no rule reads a %s", name, k, k)
			}
		}
	}
}
