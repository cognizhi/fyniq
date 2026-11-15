package litellm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The catalogue and its licence are read as downloaded, each in one GET; a server's refusal or an answer larger than
// Fyniq reads is an error, never a partial catalogue (AI-85).
// req: AI-85
func TestTheCatalogueIsReadWithItsLicence(t *testing.T) {
	big := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method != http.MethodGet:
			http.Error(w, "method", http.StatusMethodNotAllowed)
		case r.URL.Path == "/catalogue.json" && big:
			_, _ = w.Write([]byte(strings.Repeat(" ", maxBody+1)))
		case r.URL.Path == "/catalogue.json":
			_, _ = w.Write([]byte(`{"claude-test": {"input_cost_per_token": 3e-06}}`))
		case r.URL.Path == "/LICENSE":
			_, _ = w.Write([]byte("MIT License"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := Catalogue{HTTP: srv.Client(), URL: srv.URL + "/catalogue.json", LicenceURL: srv.URL + "/LICENSE"}
	doc, err := c.Fetch(context.Background())
	if err != nil || doc.Source != "litellm" || doc.Path != Path || !strings.Contains(string(doc.Body), "3e-06") || string(doc.Licence) != "MIT License" ||
		doc.URL != c.URL || doc.LicenceURL != c.LicenceURL {
		t.Fatalf("the catalogue: %v %+v", err, doc)
	}
	if _, err := (Catalogue{HTTP: srv.Client(), URL: srv.URL + "/catalogue.json", LicenceURL: srv.URL + "/missing"}).Fetch(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "404") {
		t.Errorf("a missing licence: %v", err)
	}
	big = true
	if _, err := c.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("an answer too large: %v", err)
	}
	if URL != "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json" || !strings.HasPrefix(LicenceURL, "https://"+Host+"/") {
		t.Errorf("the source: %s %s", URL, LicenceURL)
	}
}
