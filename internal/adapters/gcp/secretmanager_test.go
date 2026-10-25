package gcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

// fakeSecretManager answers like Secret Manager's REST API for the secrets OpenTofu created: addVersion, access,
// get, list (with the state filter), disable and destroy. Names in its answers carry the project's number, as the
// real service's do; access refuses a disabled or destroyed version with 400 FAILED_PRECONDITION.
type fakeSecretManager struct {
	mu       sync.Mutex
	secrets  map[string][]fakeVersion // by secret id; only the ones OpenTofu created exist
	requests []string
	auth     string
	created  map[string]string // the secrets made through the API, and the body each was made with
}

type fakeVersion struct {
	data  []byte
	state string
}

func (f *fakeSecretManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	f.auth = r.Header.Get("Authorization")
	path := strings.TrimPrefix(r.URL.Path, "/v1/")
	action := ""
	if i := strings.LastIndex(path, ":"); i > 0 {
		path, action = path[:i], path[i+1:]
	}
	parts := strings.Split(path, "/") // projects/{p}/secrets[/{s}[/versions/{n}]]
	if len(parts) == 3 && parts[0] == "projects" && parts[2] == "secrets" && r.Method == http.MethodPost {
		id := r.URL.Query().Get("secretId")
		if _, ok := f.secrets[id]; ok {
			http.Error(w, `{"error":{"status":"ALREADY_EXISTS"}}`, http.StatusConflict)
			return
		}
		var body strings.Builder
		_, _ = io.Copy(&body, r.Body)
		f.secrets[id] = nil
		if f.created == nil {
			f.created = map[string]string{}
		}
		f.created[id] = body.String()
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "projects/123456/secrets/" + id})
		return
	}
	if len(parts) < 4 || parts[0] != "projects" || parts[2] != "secrets" {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	versions, ok := f.secrets[parts[3]]
	if !ok {
		http.Error(w, `{"error":{"status":"NOT_FOUND"}}`, http.StatusNotFound)
		return
	}
	name := func(n int) string { return fmt.Sprintf("projects/123456/secrets/%s/versions/%d", parts[3], n) }
	switch {
	case len(parts) == 4 && action == "addVersion" && r.Method == http.MethodPost:
		var in struct {
			Payload payload `json:"payload"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		b, err := base64.StdEncoding.DecodeString(in.Payload.Data)
		if err != nil || in.Payload.DataCrc32c != crc(b) {
			http.Error(w, `{"error":{"status":"INVALID_ARGUMENT"}}`, http.StatusBadRequest)
			return
		}
		f.secrets[parts[3]] = append(versions, fakeVersion{data: b, state: "ENABLED"})
		_ = json.NewEncoder(w).Encode(secretVersion{Name: name(len(versions) + 1), State: "ENABLED"})
	case len(parts) == 5 && parts[4] == "versions" && r.Method == http.MethodGet:
		var out struct {
			Versions []secretVersion `json:"versions"`
		}
		for i := len(versions) - 1; i >= 0; i-- {
			if r.URL.Query().Get("filter") == "state:ENABLED" && versions[i].state != "ENABLED" {
				continue
			}
			out.Versions = append(out.Versions, secretVersion{Name: name(i + 1), State: versions[i].state})
		}
		_ = json.NewEncoder(w).Encode(out)
	case len(parts) == 6 && parts[4] == "versions":
		n, err := strconv.Atoi(parts[5])
		if err != nil || n < 1 || n > len(versions) {
			http.Error(w, `{"error":{"status":"NOT_FOUND"}}`, http.StatusNotFound)
			return
		}
		v := &versions[n-1]
		switch action {
		case "access":
			if v.state != "ENABLED" {
				http.Error(w, `{"error":{"status":"FAILED_PRECONDITION"}}`, http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"name": name(n), "payload": payload{Data: base64.StdEncoding.EncodeToString(v.data), DataCrc32c: crc(v.data)}})
		case "":
			_ = json.NewEncoder(w).Encode(secretVersion{Name: name(n), State: v.state})
		case "disable":
			if v.state == "DESTROYED" {
				http.Error(w, `{"error":{"status":"FAILED_PRECONDITION"}}`, http.StatusBadRequest)
				return
			}
			v.state = "DISABLED"
			_ = json.NewEncoder(w).Encode(secretVersion{Name: name(n), State: v.state})
		case "destroy":
			if v.state == "DESTROYED" {
				http.Error(w, `{"error":{"status":"FAILED_PRECONDITION"}}`, http.StatusBadRequest)
				return
			}
			v.state, v.data = "DESTROYED", nil
			_ = json.NewEncoder(w).Encode(secretVersion{Name: name(n), State: v.state})
		default:
			http.Error(w, "bad action", http.StatusBadRequest)
		}
	default:
		http.Error(w, "bad request", http.StatusBadRequest)
	}
}

func secretManager(t *testing.T, f *fakeSecretManager) *SecretManager {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return NewSecretManager(srv.URL, "fyniq-staging", []string{"zeptomail-key"}, []string{"connector-"}, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token-1"}), srv.Client())
}

// The Secret Manager adapter passes the SecretStore suite against a fake of the REST API, under the service's own
// token, and reads only the connectors' secrets.
// req: FR-INT-20
// req: SEC-12
// req: NFR-PORT-04
// asvs: V13.3.1, V13.3.2
func TestSecretManagerConformance(t *testing.T) {
	f := &fakeSecretManager{secrets: map[string][]fakeVersion{"connector-example-key": nil, "connector-example-mode": nil,
		"zeptomail-key": {{data: []byte("from before the framework"), state: "ENABLED"}}, "zeptomail-key-old": nil,
		"database-password": {{data: []byte("db"), state: "ENABLED"}}}}
	s := secretManager(t, f)
	porttest.SecretStore(t, s)
	if f.auth != "Bearer token-1" {
		t.Fatalf("authorization %q", f.auth)
	}
	ctx := context.Background()
	if _, err := s.Get(ctx, "projects/123456/secrets/database-password/versions/1"); err == nil {
		t.Fatal("a reference outside the connectors' secrets was read")
	}
	if _, err := s.Put(ctx, "database-password", "x"); err == nil {
		t.Fatal("a version was added outside the connectors' secrets")
	}
	if ref, err := s.Latest(ctx, "zeptomail-key"); err != nil || ref != "projects/123456/secrets/zeptomail-key/versions/1" {
		t.Fatalf("a secret named for a connector (connector_secret_sources): %q, %v", ref, err)
	}
	if _, err := s.Put(ctx, "zeptomail-key-old", "x"); err == nil {
		t.Fatal("a name is allowed exactly, not as a prefix")
	}
	if _, err := s.Put(ctx, "connector-never-created", "x"); err == nil || !strings.Contains(err.Error(), "OpenTofu") {
		t.Fatalf("a secret OpenTofu did not create: %v", err)
	}
	for _, r := range f.requests {
		if strings.Contains(r, "database-password") {
			t.Fatalf("the adapter called Secret Manager about another secret: %s", r)
		}
	}
}

// A value whose checksum does not match is refused, whichever way it travels.
// req: FR-INT-20
func TestSecretManagerChecksTheCRC(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"payload": payload{Data: base64.StdEncoding.EncodeToString([]byte("value")), DataCrc32c: "1"}})
	}))
	defer srv.Close()
	s := NewSecretManager(srv.URL, "p", nil, []string{"connector-"}, nil, srv.Client())
	if _, err := s.Get(context.Background(), ports.SecretRef("projects/1/secrets/connector-x-key/versions/1")); err == nil || !strings.Contains(err.Error(), "corrupted") {
		t.Fatalf("%v", err)
	}
}

// The organisations' connector secrets (design D12, the owner's decision of 25 Oct 2026): in their own project, each
// made the first time a version is put, in Singapore under the secrets key, and only one named org-; a secret made
// meanwhile by another request is used as it is. SplitSecrets sends each name and reference to its own store.
// req: FR-INT-20
// req: SEC-12
// req: CTL-DP-01
// asvs: V13.3.1, V13.3.2
func TestOrganisationsSecretsAreMadeInTheirOwnProject(t *testing.T) {
	inst := &fakeSecretManager{secrets: map[string][]fakeVersion{"connector-example-key": nil}}
	orgs := &fakeSecretManager{secrets: map[string][]fakeVersion{"org-raced-connector-wise-token": nil}}
	key := "projects/fyniq-staging/locations/asia-southeast1/keyRings/platform-staging/cryptoKeys/secrets"
	o := secretManager(t, orgs)
	o.Project, o.Names, o.Prefixes, o.Create = "fyniq-staging-secrets", nil, []string{OrgSecretPrefix}, &SecretCreation{Location: "asia-southeast1", KMSKey: key}
	s := SplitSecrets{Installation: secretManager(t, inst), Organisations: o}
	ctx := context.Background()

	ref, err := s.Put(ctx, "org-0192-connector-wise-token", "wise-read-only")
	if err != nil || ref != "projects/123456/secrets/org-0192-connector-wise-token/versions/1" {
		t.Fatalf("an organisation's first secret: %q, %v", ref, err)
	}
	body := orgs.created["org-0192-connector-wise-token"]
	if !strings.Contains(body, `"location":"asia-southeast1"`) || !strings.Contains(body, `"kmsKeyName":"`+key+`"`) {
		t.Fatalf("made with %s", body)
	}
	if v, err := s.Get(ctx, ref); err != nil || v != "wise-read-only" {
		t.Fatalf("reading it back: %q, %v", v, err)
	}
	if again, err := s.Put(ctx, "org-0192-connector-wise-token", "rotated"); err != nil || again != "projects/123456/secrets/org-0192-connector-wise-token/versions/2" ||
		len(orgs.created) != 1 {
		t.Fatalf("a second version: %q, %v, made %d", again, err, len(orgs.created))
	}
	if _, err := s.Put(ctx, "org-raced-connector-wise-token", "x"); err != nil {
		t.Fatalf("a secret made meanwhile: %v", err)
	}
	if _, err := s.Put(ctx, "connector-example-key", "installation"); err != nil || len(inst.secrets["connector-example-key"]) != 1 {
		t.Fatalf("an installation secret goes where OpenTofu made it: %v", err)
	}
	if err := s.Destroy(ctx, ref); err != nil || orgs.secrets["org-0192-connector-wise-token"][0].state != "DESTROYED" {
		t.Fatalf("destroying an organisation's version: %v", err)
	}
	if _, err := s.Put(ctx, "database-password", "x"); err == nil || len(inst.created) != 0 {
		t.Fatal("a name outside the connectors' secrets was made or written")
	}
	for _, r := range inst.requests {
		if strings.Contains(r, "/secrets/org-") {
			t.Fatalf("an organisation's secret reached the installation's project: %s", r)
		}
	}
}
