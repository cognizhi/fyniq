package email_test

import (
	"context"
	"net"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/maildns"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// dns answers DMARC for example.com only; every other name is not found.
type dns struct{}

func (dns) LookupTXT(_ context.Context, name string) ([]string, error) {
	if name == "_dmarc.example.com" {
		return []string{"v=DMARC1; p=reject"}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (dns) LookupCNAME(_ context.Context, name string) (string, error) {
	return "", &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

// The fakes record what the check writes: the store and unit of work embed the ports and answer only what it calls.
type (
	trail struct {
		storage.Audit
		records *[]storage.AuditRecord
	}
	unit struct {
		storage.UnitOfWork
		records *[]storage.AuditRecord
	}
	store struct {
		storage.Store
		org     uuid.UUID
		records []storage.AuditRecord
	}
	installation struct {
		storage.Installation
		org uuid.UUID
	}
)

func (t trail) Record(_ context.Context, r storage.AuditRecord) error {
	*t.records = append(*t.records, r)
	return nil
}
func (u unit) Audit() storage.Audit { return trail{records: u.records} }
func (s *store) Do(ctx context.Context, org uuid.UUID, fn func(context.Context, storage.UnitOfWork) error) error {
	if org != s.org {
		panic("the check wrote outside the installation's trail")
	}
	return fn(ctx, unit{records: &s.records})
}
func (i installation) InstallationOrganisation(context.Context) (uuid.UUID, error) {
	if i.org == uuid.Nil {
		return uuid.Nil, ports.ErrNotFound
	}
	return i.org, nil
}

// The nightly check records a failed check in the installation's trail for each sending domain that is not ok,
// naming what is wrong, and nothing for one that is; before setup it only checks.
// req: FR-INT-21
func TestTheNightlyDomainCheckRecordsEachDomainThatIsNotOK(t *testing.T) {
	org := uuid.New()
	s := &store{org: org}
	records := map[string][]maildns.Record{"mail.example.com": {{Type: "TXT", Name: "k._domainkey.mail.example.com", Value: "v=DKIM1"}}}
	results, err := email.CheckDomains(context.Background(), s, installation{org: org}, dns{}, []string{"mail.example.com", "example.com"}, records)
	if err != nil || len(results) != 2 || results[0].OK || !results[1].OK {
		t.Fatalf("%+v, %v; want the subdomain's missing DKIM to fail and example.com to pass", results, err)
	}
	if len(s.records) != 1 || s.records[0].Action != balance.ActionCheckFailed || s.records[0].Changes["domain"][1] != "mail.example.com" ||
		s.records[0].Changes["dmarc"][1] != "reject" || s.records[0].Changes["records"][1] != "TXT k._domainkey.mail.example.com: missing" {
		t.Fatalf("the trail: %+v", s.records)
	}
	before := &store{org: uuid.New()}
	if _, err := email.CheckDomains(context.Background(), before, installation{}, dns{}, []string{"mail.example.com"}, records); err != nil || len(before.records) != 0 {
		t.Fatalf("before setup: %v, %d records", err, len(before.records))
	}
}
