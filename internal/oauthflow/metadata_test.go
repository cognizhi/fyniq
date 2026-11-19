package oauthflow_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cognizhi/fyniq/internal/oauthflow"
)

// The authorisation server's metadata is published at the issuer's well-known path (RFC 8414), names only what Fyniq's OAuth server
// does (the code with S256 PKCE, public clients, no secret), and names the registration endpoint only while an Owner has registration
// on, so a client that finds none uses the client ID it was given (AI-36, AI-90).
// req: AI-36
// req: AI-90
// asvs: V10.1.1, V10.4.1
func TestTheAuthorisationServerMetadataNamesTheRegistrationEndpointOnlyWhileItIsOpen(t *testing.T) {
	open := false
	m := oauthflow.Metadata{Issuer: "http://localhost:8080/oauth/", RegisterURL: "http://localhost:8080/api/v1/oauth/register",
		RegistrationOpen: func(context.Context) bool { return open }, Scopes: []string{"openid", "fyniq.read.reports"}}
	mux := http.NewServeMux()
	m.Register(mux)
	get := func(path string) (int, map[string]any) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		var doc map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &doc)
		return w.Code, doc
	}
	status, doc := get("/.well-known/oauth-authorization-server/oauth")
	if status != 200 || doc["issuer"] != "http://localhost:8080/oauth/" || doc["token_endpoint"] != "http://localhost:8080/oauth/oauth2/token" ||
		doc["authorization_endpoint"] != "http://localhost:8080/oauth/oauth2/auth" {
		t.Fatalf("the metadata: %d %v", status, doc)
	}
	if doc["registration_endpoint"] != nil {
		t.Errorf("registration is closed and the metadata names its endpoint: %v", doc["registration_endpoint"])
	}
	if pk, _ := doc["code_challenge_methods_supported"].([]any); len(pk) != 1 || pk[0] != "S256" {
		t.Errorf("PKCE: %v", pk)
	}
	if au, _ := doc["token_endpoint_auth_methods_supported"].([]any); len(au) != 1 || au[0] != "none" {
		t.Errorf("client authentication: %v", au)
	}
	if gt, _ := doc["grant_types_supported"].([]any); len(gt) != 2 || doc["response_types_supported"].([]any)[0] != "code" {
		t.Errorf("grants: %v", gt)
	}
	open = true
	if _, doc := get("/.well-known/oauth-authorization-server/oauth"); doc["registration_endpoint"] != "http://localhost:8080/api/v1/oauth/register" {
		t.Errorf("registration open: %v", doc["registration_endpoint"])
	}
	if status, _ := get("/.well-known/oauth-authorization-server"); status != 404 {
		t.Errorf("the root path answered for an issuer with a path: %d", status)
	}
}
