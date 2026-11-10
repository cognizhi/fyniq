package egress_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/egress"
)

// A private address is reached only as a registered endpoint: on its port, over TLS whose key matches the pin, by a
// client built with it. Unregistered, on another port, in the clear or with another key it is refused; the metadata
// service stays refused whatever range is registered.
// req: SEC-37
// req: QA-SEC-04
// asvs: V1.3.6, V12.3.4
func TestARegisteredPrivateEndpointIsReachedOnlyWithItsPinnedKey(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("model")) }))
	defer srv.Close()
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("plain")) }))
	defer plain.Close()
	port := func(u string) int { p, _ := strconv.Atoi(u[strings.LastIndex(u, ":")+1:]); return p }
	pin, err := egress.ParsePin(egress.PinOf(srv.Certificate().RawSubjectPublicKeyInfo))
	if err != nil {
		t.Fatal(err)
	}
	here := netip.MustParsePrefix("127.0.0.1/32")
	release := &egress.Guard{} // refuses loopback and private addresses, as in a release build
	get := func(c *http.Client, u string) (string, error) {
		res, err := c.Get(u)
		if err != nil {
			return "", err
		}
		defer res.Body.Close()
		b, err := io.ReadAll(res.Body)
		return string(b), err
	}
	if b, err := get(release.With(egress.Endpoint{Prefix: here, Port: port(srv.URL), Pin: pin}).Client(nil), srv.URL); err != nil || b != "model" {
		t.Fatalf("the registered endpoint: %q, %v", b, err)
	}
	other := pin
	other[0] ^= 1
	refused := map[string]*http.Client{
		"unregistered":   release.Client(nil),
		"another key":    release.With(egress.Endpoint{Prefix: here, Port: port(srv.URL), Pin: other}).Client(nil),
		"another port":   release.With(egress.Endpoint{Prefix: here, Port: port(srv.URL) + 1, Pin: pin}).Client(nil),
		"not its host":   release.With(egress.Endpoint{Prefix: here, Port: port(srv.URL), Pin: pin}).Client(func(string, int) bool { return false }),
		"in the clear":   release.With(egress.Endpoint{Prefix: here, Port: port(plain.URL), Pin: pin}).Client(nil),
		"metadata range": release.With(egress.Endpoint{Prefix: netip.MustParsePrefix("169.254.0.0/16"), Port: 443, Pin: pin}).Client(nil),
	}
	for name, c := range refused {
		u := srv.URL
		switch name {
		case "in the clear":
			u = plain.URL
		case "metadata range":
			u = "https://169.254.169.254/"
		}
		if b, err := get(c, u); !errors.Is(err, egress.ErrRefused) {
			t.Errorf("%s: %q, %v", name, b, err)
		}
	}
	// The platform's own networks stay refused, even registered.
	own := &egress.Guard{Own: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}
	if b, err := get(own.With(egress.Endpoint{Prefix: here, Port: port(srv.URL), Pin: pin}).Client(nil), srv.URL); !errors.Is(err, egress.ErrRefused) {
		t.Fatalf("one of the platform's own networks: %q, %v", b, err)
	}
	if !own.IsOwn(netip.MustParsePrefix("127.1.0.0/24")) || own.IsOwn(netip.MustParsePrefix("10.20.0.0/28")) {
		t.Fatal("IsOwn must tell a range that overlaps the platform's networks")
	}
	// A raw connection never reaches it: only the pinned TLS of With's clients does.
	if _, err := release.With(egress.Endpoint{Prefix: here, Port: port(srv.URL), Pin: pin}).DialContext(t.Context(), "tcp", srv.Listener.Addr().String()); !errors.Is(err, egress.ErrRefused) {
		t.Fatalf("a raw connection to the registered endpoint: %v", err)
	}
	if _, err := egress.ParsePin("not-a-pin"); err == nil {
		t.Fatal("a pin that is not the base64 of a SHA-256 was read")
	}
}
