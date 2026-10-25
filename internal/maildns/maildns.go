// Package maildns checks the deployment's sending domains (FR-INT-21; PRD-001 rev 0.104): each record the providers
// asked for, as the deployment lists it from their dashboards (a DKIM key, a return-path CNAME, an SPF include), and the
// domain's DMARC policy. The providers show their records per account (ZeptoMail's "Domains" page, retrieved 23 Oct
// 2026, needs no SPF record and names its DKIM selector per domain), so the records are configuration, not code.
package maildns

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"
)

// Resolver is the DNS: *net.Resolver is one.
type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupCNAME(ctx context.Context, host string) (string, error)
}

// Record is one record a provider asked for: a TXT record that contains Value, or a CNAME to Value.
type Record struct {
	Type  string `json:"type"` // TXT or CNAME
	Name  string `json:"name"`
	Value string `json:"value"`
}

// The statuses.
const (
	OK      = "ok"
	Missing = "missing" // no such record
	Wrong   = "wrong"   // a record, but not the one asked for
	Failed  = "failed"  // the lookup failed: try again later
	None    = "none"    // DMARC with p=none: reports only, nothing enforced
	Invalid = "invalid" // DMARC with a policy that is not none, quarantine or reject
)

// RecordStatus is one record's state.
type RecordStatus struct {
	Record
	Status string `json:"status"`
	Found  string `json:"found,omitempty"`
}

// Result is a domain's state.
type Result struct {
	Domain  string         `json:"domain"`
	Records []RecordStatus `json:"records"`
	DMARC   string         `json:"dmarc"` // its policy: reject, quarantine, none, or invalid, missing or failed
	OK      bool           `json:"ok"`    // every record ok, and a DMARC policy of quarantine or reject
}

// All checks every domain with its records, in order.
func All(ctx context.Context, r Resolver, domains []string, records map[string][]Record) []Result {
	out := make([]Result, 0, len(domains))
	for _, d := range domains {
		out = append(out, Check(ctx, r, d, records[d]))
	}
	return out
}

// Check checks domain's records and its DMARC policy, at _dmarc.<domain>, else at its parent's (a policy there
// covers its subdomains).
func Check(ctx context.Context, r Resolver, domain string, records []Record) Result {
	out := Result{Domain: domain, OK: true}
	for _, rec := range records {
		s := RecordStatus{Record: rec, Status: OK}
		switch strings.ToUpper(rec.Type) {
		case "CNAME":
			got, err := r.LookupCNAME(ctx, rec.Name)
			s.Status, s.Found = cname(got, err, rec.Value)
		default:
			got, err := r.LookupTXT(ctx, rec.Name)
			s.Status, s.Found = txt(got, err, rec.Value)
		}
		out.OK = out.OK && s.Status == OK
		out.Records = append(out.Records, s)
	}
	out.DMARC = dmarc(ctx, r, domain)
	if i := strings.Index(domain, "."); out.DMARC == Missing && i > 0 && strings.Count(domain, ".") > 1 {
		out.DMARC = dmarc(ctx, r, domain[i+1:])
	}
	out.OK = out.OK && (out.DMARC == "reject" || out.DMARC == "quarantine")
	return out
}

func notFound(err error) bool {
	var d *net.DNSError
	return errors.As(err, &d) && d.IsNotFound
}

func txt(got []string, err error, want string) (string, string) {
	switch {
	case notFound(err):
		return Missing, ""
	case err != nil:
		return Failed, ""
	}
	for _, t := range got {
		if strings.Contains(t, want) {
			return OK, ""
		}
	}
	if len(got) == 0 {
		return Missing, ""
	}
	return Wrong, clip(strings.Join(got, " | "))
}

func cname(got string, err error, want string) (string, string) {
	switch {
	case notFound(err):
		return Missing, ""
	case err != nil:
		return Failed, ""
	}
	if strings.EqualFold(strings.TrimSuffix(got, "."), strings.TrimSuffix(want, ".")) {
		return OK, ""
	}
	return Wrong, clip(got)
}

func dmarc(ctx context.Context, r Resolver, domain string) string {
	got, err := r.LookupTXT(ctx, "_dmarc."+domain)
	switch {
	case notFound(err):
		return Missing
	case err != nil:
		return Failed
	}
	for _, t := range got {
		if !strings.HasPrefix(strings.TrimSpace(t), "v=DMARC1") {
			continue
		}
		for _, part := range strings.Split(t, ";") {
			k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
			if strings.TrimSpace(k) == "p" {
				switch p := strings.ToLower(strings.TrimSpace(v)); p {
				case "reject", "quarantine", None:
					return p
				}
				return Invalid
			}
		}
		return None
	}
	return Missing
}

func clip(s string) string {
	if r := []rune(s); len(r) > 200 {
		return string(r[:200])
	}
	return s
}

// Cache keeps the last check of every domain for TTL, so reading the email settings does not ask the DNS each time.
type Cache struct {
	Resolver Resolver
	Domains  []string
	Records  map[string][]Record
	// RecordsFrom, when set, gives the records at each check in place of Records: the email connectors' (rev 0.139).
	RecordsFrom func(ctx context.Context) map[string][]Record
	TTL      time.Duration
	Now      func() time.Time

	mu   sync.Mutex
	at   time.Time
	last []Result
}

// Results are the domains' results and when they were checked: the cached ones while younger than TTL. A lookup
// takes at most five seconds.
func (c *Cache) Results(ctx context.Context) ([]Result, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now := c.Now(); c.last == nil || now.Sub(c.at) >= c.TTL {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		records := c.Records
		if c.RecordsFrom != nil {
			records = c.RecordsFrom(ctx)
		}
		c.last, c.at = All(ctx, c.Resolver, c.Domains, records), now
	}
	return c.last, c.at
}
