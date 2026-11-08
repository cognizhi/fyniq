package journal

import (
	"context"
	"fmt"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// RuleCarryForward is the purge after retention's carry-forward rule (AUD-10), built by internal/domain/retention.
const RuleCarryForward = "purge-carry-forward@1"

// CarryForward posts the purge after retention's carry-forward entries (AUD-10; p1-audit-explorer design D7b) in the
// purge's unit of work: the posting service is the one path to the ledger (§11.4), and the database admits these
// entries only in the transaction that recorded their boundary (FY305). It posts nothing of another rule.
func CarryForward(ctx context.Context, u storage.UnitOfWork, entries []storage.NewEntry) error {
	for _, e := range entries {
		if e.RuleVersion != RuleCarryForward {
			return fmt.Errorf("journal: %s is not a carry-forward of the purge after retention", e.PostingIdentity)
		}
		if _, err := u.Ledger().Append(ctx, e); err != nil {
			return err
		}
	}
	return nil
}
