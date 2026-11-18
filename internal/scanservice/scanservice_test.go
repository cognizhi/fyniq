package scanservice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
)

// tokens accepts "ok" as the scanner class's token only.
type tokens struct{}

func (tokens) Token(string) (string, error) { return "ok", nil }
func (tokens) Verify(token, class string) error {
	if token != "ok" || class != Class {
		return ports.ErrUnauthorised
	}
	return nil
}

// clamd stands in for the sidecar: EICAR is found, "broken" cannot be decided, anything else is clean; it reads the
// whole file as clamd does.
type clamd struct{ read int }

func (c *clamd) Scan(_ context.Context, r io.Reader) (ports.ScanResult, error) {
	b, err := io.ReadAll(r)
	c.read = len(b)
	switch {
	case err != nil:
		return ports.ScanResult{}, err
	case bytes.Contains(b, []byte("EICAR")):
		return ports.ScanResult{Signature: "Eicar-Test-Signature"}, nil
	case string(b) == "broken":
		return ports.ScanResult{}, errors.New("clamd: lstat() failed")
	}
	return ports.ScanResult{Clean: true}, nil
}

// The scanner service scans for its callers only, through its client: a clean file, a found signature, a file clamd
// could not decide (never clean), a file too large, and a caller without a token (FR-DOC-08, SEC-24; rev 0.247).
// req: SEC-24
// req: FR-DOC-08
// asvs: V5.2.1
func TestTheScannerServiceScansForItsCallers(t *testing.T) {
	c := &clamd{}
	srv := httptest.NewServer(Handler{Scanner: c, Tokens: tokens{}})
	defer srv.Close()
	client := &Client{URL: srv.URL, HTTP: srv.Client(), Token: func(context.Context) (string, error) { return "ok", nil }}
	ctx := context.Background()
	if r, err := client.Scan(ctx, strings.NewReader("a receipt")); err != nil || !r.Clean || c.read != len("a receipt") {
		t.Errorf("a clean file: %+v %v", r, err)
	}
	if r, err := client.Scan(ctx, strings.NewReader("X5O!P%@AP EICAR test")); err != nil || r.Clean || r.Signature != "Eicar-Test-Signature" {
		t.Errorf("EICAR: %+v %v", r, err)
	}
	var p *Problem
	if r, err := client.Scan(ctx, strings.NewReader("broken")); !errors.As(err, &p) || p.Code != CodeFailed || r.Clean ||
		strings.Contains(err.Error(), "lstat") {
		t.Errorf("an undecided file: %+v %v", r, err)
	}
	if _, err := client.Scan(ctx, io.LimitReader(zeros{}, MaxBody+1)); !errors.As(err, &p) || p.Code != CodeTooLarge {
		t.Errorf("a file too large: %v", err)
	}
	stranger := &Client{URL: srv.URL, HTTP: srv.Client(), Token: func(context.Context) (string, error) { return "forged", nil }}
	if r, err := stranger.Scan(ctx, strings.NewReader("a receipt")); !errors.As(err, &p) || p.Code != CodeUnauthorised || p.Status != http.StatusUnauthorized || r.Clean {
		t.Errorf("a stranger: %+v %v", r, err)
	}
}

type zeros struct{}

func (zeros) Read(b []byte) (int, error) {
	clear(b)
	return len(b), nil
}
