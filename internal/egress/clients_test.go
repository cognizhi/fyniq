package egress_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/egress"
)

// A connection reaches only the hosts its configuration names, exactly or under a ".suffix", and still only addresses
// the guard allows; a connection that names none reaches nothing.
// req: SEC-36
// req: QA-SEC-04
// asvs: V1.3.6, V15.3.2
func TestAConnectionReachesOnlyItsOwnHosts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer srv.Close()
	port := srv.URL[strings.LastIndex(srv.URL, ":"):]
	r := &resolver{answers: map[string][][]netip.Addr{
		"api.example.com": {addrs("127.0.0.1")}, "eu.api.example.com": {addrs("127.0.0.1")}, "example.com": {addrs("127.0.0.1")},
		"other.example.org": {addrs("127.0.0.1")},
	}}
	local := &egress.Guard{Local: true, Resolver: r}
	get := func(c *http.Client, host string) error {
		res, err := c.Get("http://" + host + port + "/")
		if err == nil {
			b, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if string(b) != "ok" {
				return errors.New("no answer")
			}
		}
		return err
	}
	exact, suffix := local.For("API.example.com."), local.For(".example.com")
	if err := get(exact, "api.example.com"); err != nil {
		t.Fatalf("its own host: %v", err)
	}
	if err := get(suffix, "eu.api.example.com"); err != nil {
		t.Fatalf("a name under its suffix: %v", err)
	}
	for c, host := range map[*http.Client]string{exact: "other.example.org", suffix: "example.com", local.For(): "api.example.com"} {
		if err := get(c, host); !errors.Is(err, egress.ErrRefused) || !strings.Contains(err.Error(), "not one of this connection's hosts") {
			t.Errorf("%s was reached: %v", host, err)
		}
	}
	// The allow-list never widens the guard: a release guard still refuses this machine.
	if err := get((&egress.Guard{Resolver: r}).For("api.example.com"), "api.example.com"); !errors.Is(err, egress.ErrRefused) {
		t.Fatalf("a listed host on a loopback address, in a release build: %v", err)
	}
	if egress.HostOf("https://Mail.Example.com:8443/v1/") != "mail.example.com" || egress.HostOf("::bad") != "" {
		t.Fatal("HostOf must give a configured URL's host")
	}
	b := local.Bounded(64<<20, 0)
	if b.MaxBody != 64<<20 || local.MaxBody != 0 || !b.Local {
		t.Fatal("Bounded must copy the guard, changing only its limits")
	}
}

// The metadata server and the process itself each have one narrow client, which refuses every other destination.
// req: SEC-36
// asvs: V15.3.2
func TestTheNarrowClientsReachOneDestination(t *testing.T) {
	for _, u := range []string{"http://example.com/", "http://metadata.google.internal:8080/", "https://169.254.169.254/", "http://10.0.0.1/"} {
		if _, err := egress.Metadata().Get(u); !errors.Is(err, egress.ErrRefused) {
			t.Errorf("the metadata client reached %s: %v", u, err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/away" {
			http.Redirect(w, r, "http://example.com/", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("ready"))
	}))
	defer srv.Close()
	res, err := egress.Loopback().Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if _, err := egress.Loopback().Get(srv.URL + "/away"); !errors.Is(err, egress.ErrRedirect) {
		t.Fatalf("the loopback client followed a redirect: %v", err)
	}
	if _, err := egress.Loopback().Get("http://example.com/"); !errors.Is(err, egress.ErrRefused) {
		t.Fatalf("the loopback client left the machine: %v", err)
	}
	if _, err := egress.Public().Get(srv.URL); !errors.Is(err, egress.ErrRefused) {
		t.Fatalf("the public client reached this machine: %v", err)
	}
}
