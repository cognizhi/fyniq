package maildns_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/maildns"
)

// fakeDNS answers from its tables; a name in neither is not found, and one in fail fails.
type fakeDNS struct {
	txt   map[string][]string
	cname map[string]string
	fail  map[string]bool
	asked int
}

func (f *fakeDNS) LookupTXT(_ context.Context, name string) ([]string, error) {
	f.asked++
	if f.fail[name] {
		return nil, &net.DNSError{Err: "timeout", Name: name, IsTimeout: true}
	}
	if v, ok := f.txt[name]; ok {
		return v, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (f *fakeDNS) LookupCNAME(_ context.Context, name string) (string, error) {
	f.asked++
	if v, ok := f.cname[name]; ok {
		return v, nil
	}
	return "", &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

var records = []maildns.Record{
	{Type: "TXT", Name: "1234._domainkey.mail.example.com", Value: "v=DKIM1; k=rsa; p=MIGf"},
	{Type: "CNAME", Name: "bounce-zem.mail.example.com", Value: "cluster89.zeptomail.com"},
}

// A domain is ok when every record its providers asked for is there as asked, and its DMARC policy (its own, else
// its parent's) quarantines or rejects; each record says what is wrong otherwise.
// req: FR-INT-21
func TestASendingDomainIsCheckedRecordByRecord(t *testing.T) {
	good := &fakeDNS{txt: map[string][]string{"1234._domainkey.mail.example.com": {"v=DKIM1; k=rsa; p=MIGfMA0"},
		"_dmarc.example.com": {"v=DMARC1; p=quarantine; rua=mailto:dmarc@example.com"}},
		cname: map[string]string{"bounce-zem.mail.example.com": "cluster89.zeptomail.com."}}
	r := maildns.Check(context.Background(), good, "mail.example.com", records)
	if !r.OK || r.DMARC != "quarantine" || r.Records[0].Status != maildns.OK || r.Records[1].Status != maildns.OK {
		t.Fatalf("a good domain: %+v", r)
	}
	bad := &fakeDNS{txt: map[string][]string{"1234._domainkey.mail.example.com": {"v=DKIM1; p=OTHERKEY"}, "_dmarc.mail.example.com": {"v=DMARC1; p=none"}},
		fail: map[string]bool{}, cname: map[string]string{"bounce-zem.mail.example.com": "elsewhere.example.net"}}
	r = maildns.Check(context.Background(), bad, "mail.example.com", records)
	if r.OK || r.DMARC != "none" || r.Records[0].Status != maildns.Wrong || r.Records[0].Found != "v=DKIM1; p=OTHERKEY" ||
		r.Records[1].Status != maildns.Wrong || r.Records[1].Found != "elsewhere.example.net" {
		t.Fatalf("a wrong key, CNAME and a p=none policy: %+v", r)
	}
	none := &fakeDNS{fail: map[string]bool{"1234._domainkey.mail.example.com": true}}
	r = maildns.Check(context.Background(), none, "mail.example.com", records)
	if r.OK || r.DMARC != maildns.Missing || r.Records[0].Status != maildns.Failed || r.Records[1].Status != maildns.Missing {
		t.Fatalf("a failed lookup and missing records: %+v", r)
	}
}

// The cache asks the DNS again only once its results are older than its TTL.
// req: FR-INT-21
func TestTheCheckIsCachedForItsTTL(t *testing.T) {
	dns := &fakeDNS{}
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	c := &maildns.Cache{Resolver: dns, Domains: []string{"mail.example.com"}, Records: map[string][]maildns.Record{"mail.example.com": records},
		TTL: time.Hour, Now: func() time.Time { return now }}
	_, at := c.Results(context.Background())
	asked := dns.asked
	now = now.Add(59 * time.Minute)
	if _, again := c.Results(context.Background()); dns.asked != asked || !again.Equal(at) {
		t.Fatalf("asked again within the hour (%d lookups)", dns.asked-asked)
	}
	now = now.Add(time.Minute)
	if _, later := c.Results(context.Background()); dns.asked == asked || !later.Equal(now) {
		t.Fatal("did not ask again after the hour")
	}
}
