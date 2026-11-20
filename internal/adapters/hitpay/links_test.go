package hitpay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/cognizhi/fyniq/internal/ports"
)

func linker(srv *httptest.Server) *Linker {
	return &Linker{Base: srv.URL, Key: "key-1", HTTP: srv.Client()}
}

// The one write of the payment-link capability: a form POST to /v1/payment-requests with the amount as exact decimal text, the
// currency, the invoice's number as the reference, the methods and an expiry in SGT; the answer's ID and https URL are what is kept.
// req: FR-AR-13
// req: FR-PSP-02
func TestAPaymentRequestIsOneFormPostWithExactText(t *testing.T) {
	var got url.Values
	var method, path, key, ctype string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, key, ctype = r.Method, r.URL.Path, r.Header.Get("X-BUSINESS-API-KEY"), r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		got, _ = url.ParseQuery(string(b))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"90f28b43-2cff-4f86-a29e-15697424b3e7","status":"pending","url":"https://securecheckout.sandbox.hit-pay.com/payment-request/x","expiry_date":null}`))
	}))
	defer srv.Close()
	exp := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	made, err := linker(srv).CreatePaymentRequest(context.Background(), ports.PaymentRequest{Reference: "INV-0150", Amount: "5450.00", Currency: "SGD",
		Purpose: "Invoice INV-0150", Methods: []string{"paynow_online", "card"}, ExpiresAt: &exp})
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != PaymentRequestPath || key != "key-1" || !strings.HasPrefix(ctype, "application/x-www-form-urlencoded") {
		t.Fatalf("the request was %s %s with %q and %q", method, path, key, ctype)
	}
	if got.Get("amount") != "5450.00" || got.Get("currency") != "SGD" || got.Get("reference_number") != "INV-0150" ||
		strings.Join(got["payment_methods[]"], ",") != "paynow_online,card" || got.Get("expiry_date") != "2026-12-31 08:00:00" || got.Has("webhook") {
		t.Fatalf("the form was %v", got)
	}
	if made.ID != "90f28b43-2cff-4f86-a29e-15697424b3e7" || !strings.HasPrefix(made.URL, "https://") || made.ExpiresAt == nil {
		t.Fatalf("the link: %+v", made)
	}
}

// The link client cannot do anything but make a payment request: another method or path is refused before it leaves, so a bug
// cannot charge, refund or pay out with the key; an amount that is not exact, a link with no https URL and a refusal are errors
// that keep no body.
// req: FR-INT-27
func TestTheLinkClientSendsNothingButAPaymentRequest(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("X-Case") == "refuse" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"secret detail"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"x","status":"pending","url":"http://insecure.example/x"}`))
	}))
	defer srv.Close()
	l := linker(srv)
	for _, bad := range []struct{ method, path string }{{http.MethodGet, PaymentRequestPath}, {http.MethodDelete, PaymentRequestPath + "/abc"},
		{http.MethodPost, "/v1/refund"}, {http.MethodPost, "/v1/payment-requests/abc"}, {http.MethodPost, "/v1/charges"}} {
		req, _ := http.NewRequest(bad.method, srv.URL+bad.path, nil)
		if _, err := l.client().Do(req); !errors.Is(err, ErrMethod) {
			t.Errorf("%s %s was not refused: %v", bad.method, bad.path, err)
		}
	}
	if hits != 0 {
		t.Fatalf("a refused request reached HitPay %d times", hits)
	}
	for _, amount := range []string{"100.001", "0", "-5.00", "1e2", ""} {
		if _, err := l.CreatePaymentRequest(context.Background(), ports.PaymentRequest{Reference: "INV-1", Amount: amount, Currency: "SGD"}); err == nil {
			t.Errorf("the amount %q was sent", amount)
		}
	}
	if _, err := l.CreatePaymentRequest(context.Background(), ports.PaymentRequest{Reference: "INV-1", Amount: "10.00", Currency: "SGD"}); !errors.Is(err, ports.ErrPaymentRequestRefused) {
		t.Errorf("a link with no https URL was accepted: %v", err)
	}
	refused := &Linker{Base: srv.URL, Key: "k", HTTP: &http.Client{Transport: headerAdder{srv.Client().Transport, "X-Case", "refuse"}}}
	_, err := refused.CreatePaymentRequest(context.Background(), ports.PaymentRequest{Reference: "INV-1", Amount: "10.00", Currency: "SGD"})
	if !errors.Is(err, ports.ErrPaymentRequestRefused) || strings.Contains(err.Error(), "secret detail") {
		t.Errorf("a refusal: %v", err)
	}
}

type headerAdder struct {
	next http.RoundTripper
	k, v string
}

func (h headerAdder) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set(h.k, h.v)
	return h.next.RoundTrip(r)
}

// The partner contract names the one write of the link client, and no other write.
// req: FR-INT-27
func TestThePartnerContractNamesTheOnePaymentRequestWrite(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/partners/hitpay.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for p, ops := range doc.Paths {
		for method := range ops {
			if method == "get" || method == "parameters" {
				continue
			}
			if p != PaymentRequestPath || method != "post" {
				t.Errorf("%s has a %s operation: the only write is POST %s", p, method, PaymentRequestPath)
			}
		}
	}
	if _, ok := doc.Paths[PaymentRequestPath]["post"]; !ok {
		t.Errorf("the contract has no POST %s", PaymentRequestPath)
	}
}
