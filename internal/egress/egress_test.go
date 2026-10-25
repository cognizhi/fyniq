package egress_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/cognizhi/fyniq/internal/egress"
)

type resolver struct {
	mu      sync.Mutex
	answers map[string][][]netip.Addr // each lookup takes the next answer, the last one repeating
	calls   map[string]int
}

func (r *resolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.calls == nil {
		r.calls = map[string]int{}
	}
	as, ok := r.answers[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	i := min(r.calls[host], len(as)-1)
	r.calls[host]++
	return as[i], nil
}

func addrs(s ...string) []netip.Addr {
	out := make([]netip.Addr, 0, len(s))
	for _, x := range s {
		out = append(out, netip.MustParseAddr(x))
	}
	return out
}

// Every address that is not a public one is refused: this machine, private and shared ranges, link-local with the
// metadata service, unique-local, unspecified and multicast, also as IPv4-mapped IPv6. The local flavour lets its
// substitutes through on loopback and the Docker network, never the metadata service.
// req: SEC-36
// req: QA-SEC-04
// asvs: V1.3.6
func TestEgressRefusals(t *testing.T) {
	release, local := &egress.Guard{}, &egress.Guard{Local: true}
	for _, a := range []string{"127.0.0.1", "::1", "10.0.0.1", "172.16.5.4", "192.168.1.1", "169.254.169.254", "169.254.1.1", "fe80::1",
		"fc00::1", "fd12:3456::1", "0.0.0.0", "::", "100.64.1.1", "198.18.0.1", "224.0.0.1", "ff02::1", "::ffff:127.0.0.1", "::ffff:10.1.2.3"} {
		if err := release.Allowed(netip.MustParseAddr(a)); !errors.Is(err, egress.ErrRefused) {
			t.Errorf("%s was allowed", a)
		}
	}
	for _, a := range []string{"8.8.8.8", "203.0.113.10", "2001:4860:4860::8888"} {
		if err := release.Allowed(netip.MustParseAddr(a)); err != nil {
			t.Errorf("%s: %v", a, err)
		}
	}
	for a, want := range map[string]bool{"127.0.0.1": true, "172.18.0.5": true, "169.254.169.254": false, "fe80::1": false} {
		if err := local.Allowed(netip.MustParseAddr(a)); (err == nil) != want {
			t.Errorf("local flavour, %s: %v", a, err)
		}
	}

	ctx := context.Background()
	r := &resolver{answers: map[string][][]netip.Addr{
		"public.example":           {addrs("203.0.113.10")},
		"mixed.example":            {addrs("203.0.113.10", "10.0.0.1")},
		"metadata.google.internal": {addrs("203.0.113.10")},
		"api.internal":             {addrs("203.0.113.10")},
		"worker.fyniq.test":        {addrs("203.0.113.10")},
	}}
	g := &egress.Guard{Resolver: r, Internal: []string{".fyniq.test"}}
	if _, err := g.Resolve(ctx, "public.example"); err != nil {
		t.Fatalf("a public name: %v", err)
	}
	for _, host := range []string{"mixed.example", "metadata.google.internal", "api.internal", "worker.fyniq.test", "localhost", "unknown.example", "169.254.169.254", "[::1]"} {
		if _, err := g.Resolve(ctx, host); !errors.Is(err, egress.ErrRefused) {
			t.Errorf("%s was allowed: %v", host, err)
		}
	}
	if _, err := g.DialContext(ctx, "tcp", "public.example:0"); !errors.Is(err, egress.ErrRefused) {
		t.Errorf("port 0: %v", err)
	}
}

// The guard dials the address it checked: a name that resolves differently on a second lookup (DNS rebinding) is
// looked up once, and the connection goes where the check allowed.
// req: SEC-36
// req: QA-SEC-04
// asvs: V1.3.6
func TestEgressDialsTheAddressItChecked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	r := &resolver{answers: map[string][][]netip.Addr{"rebind.example": {addrs("127.0.0.1"), addrs("169.254.169.254")}}}
	g := &egress.Guard{Local: true, Resolver: r}
	res, err := g.Client(nil).Get("http://rebind.example:" + port + "/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if string(b) != "ok" || r.calls["rebind.example"] != 1 {
		t.Fatalf("%q after %d lookups", b, r.calls["rebind.example"])
	}
	if _, err := g.Client(nil).Get("http://rebind.example:" + port + "/"); !errors.Is(err, egress.ErrRefused) {
		t.Fatalf("the second answer, the metadata address: %v", err)
	}
}

// No redirect is followed, even to an allowed address; a host the connector did not declare is refused before any
// connection; an answer larger than the limit is refused, whether it says so or not.
// req: SEC-36
// req: QA-SEC-04
// asvs: V15.3.2, V13.2.4
func TestEgressFollowsNoRedirectAndBoundsTheAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "http://169.254.169.254/computeMetadata/v1/", http.StatusFound)
		case "/large":
			_, _ = w.Write([]byte(strings.Repeat("x", 2048)))
		case "/chunked":
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte(strings.Repeat("y", 2048)))
		default:
			_, _ = w.Write([]byte(strings.Repeat("z", 1024)))
		}
	}))
	defer srv.Close()
	g := &egress.Guard{Local: true, MaxBody: 1024}
	if _, err := g.Client(nil).Get(srv.URL + "/redirect"); !errors.Is(err, egress.ErrRedirect) {
		t.Fatalf("a redirect: %v", err)
	}
	if _, err := g.Client(nil).Get(srv.URL + "/large"); !errors.Is(err, egress.ErrTooLarge) {
		t.Fatalf("a body over the limit it announced: %v", err)
	}
	res, err := g.Client(nil).Get(srv.URL + "/chunked")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(res.Body); !errors.Is(err, egress.ErrTooLarge) {
		t.Fatalf("a body over the limit it did not announce: %v", err)
	}
	_ = res.Body.Close()
	res, err = g.Client(nil).Get(srv.URL + "/exact")
	if err != nil {
		t.Fatal(err)
	}
	if b, err := io.ReadAll(res.Body); err != nil || len(b) != 1024 {
		t.Fatalf("a body at the limit: %d, %v", len(b), err)
	}
	_ = res.Body.Close()
	only := g.Client(func(host string, _ int) bool { return host == "api.example.com" })
	if _, err := only.Get(srv.URL + "/exact"); !errors.Is(err, egress.ErrRefused) {
		t.Fatalf("a host the connector did not declare: %v", err)
	}
}
