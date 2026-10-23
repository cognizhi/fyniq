package email

import (
	"context"
	"errors"
	"strings"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/maildns"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// DomainsJob is the platform job that checks the sending domains daily (outbox.PlatformSchedules).
const DomainsJob = "email.domains"

// CheckDomains checks the deployment's sending domains (FR-INT-21; PRD-001 rev 0.104), each against the records its
// providers asked for and its DMARC policy, and records a failed check in the installation's trail for each domain
// that is not ok, for the checks to show (Appendix C.2 #37). Before first-run setup there is no trail to record in.
// A domain that is not ok does not fail the job: the job ran, and the record says what is wrong.
func CheckDomains(ctx context.Context, store storage.Store, inst storage.Installation, r maildns.Resolver, domains []string,
	records map[string][]maildns.Record) ([]maildns.Result, error) {
	results := maildns.All(ctx, r, domains, records)
	org, err := inst.InstallationOrganisation(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return results, nil
	}
	if err != nil {
		return results, err
	}
	for _, res := range results {
		if res.OK {
			continue
		}
		var wrong []string
		for _, rec := range res.Records {
			if rec.Status != maildns.OK {
				wrong = append(wrong, rec.Type+" "+rec.Name+": "+rec.Status)
			}
		}
		if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Audit().Record(ctx, storage.AuditRecord{Action: balance.ActionCheckFailed, ObjectType: "organisation", ObjectID: org,
				Changes: map[string][2]any{"check": {nil, DomainsJob}, "domain": {nil, res.Domain}, "dmarc": {nil, res.DMARC},
					"records": {nil, strings.Join(wrong, "; ")}}})
		}); err != nil {
			return results, err
		}
	}
	return results, nil
}
