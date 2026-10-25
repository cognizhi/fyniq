package gcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/ports"
)

// SecretManagerAPI is Secret Manager's REST API.
const SecretManagerAPI = "https://secretmanager.googleapis.com"

// SecretManager is ports.SecretStore on Secret Manager (FR-INT-20, SEC-12): a secret typed in Settings › Integrations
// becomes a new version of a secret OpenTofu created, and the database keeps the version's name. The service's own
// account may add, disable and destroy versions of the connectors' secrets only (infra/gcp/modules/environment), and
// the adapter itself refuses any other secret, so a reference from the database cannot read one.
type SecretManager struct {
	Endpoint string
	Project  string   // the project id the secrets are in
	Names    []string // the secret ids it may touch: the values of FYNIQ_CONNECTOR_SECRETS
	Prefixes []string // and those starting with one of these
	r        rest
}

var _ ports.SecretStore = (*SecretManager)(nil)

// NewSecretManager returns the adapter for the secrets in project that names lists, or whose ids start with one of
// prefixes.
func NewSecretManager(endpoint, project string, names, prefixes []string, tokens oauth2.TokenSource, client *http.Client) *SecretManager {
	return &SecretManager{Endpoint: strings.TrimSuffix(endpoint, "/"), Project: project, Names: names, Prefixes: prefixes,
		r: rest{tokens: tokens, client: client}}
}

// versionName is a secret version's resource name, as Secret Manager answers it (with the project's number).
var versionName = regexp.MustCompile(`^projects/[a-z0-9-]+/secrets/([a-z][a-z0-9_-]{2,254})/versions/([1-9][0-9]*)$`)

func (s *SecretManager) allowed(name string) error {
	if err := ports.CheckSecretName(name); err != nil {
		return err
	}
	if slices.Contains(s.Names, name) {
		return nil
	}
	for _, p := range s.Prefixes {
		if strings.HasPrefix(name, p) {
			return nil
		}
	}
	return fmt.Errorf("secret %s is not a connector's secret", name)
}

// version checks ref and returns it as a resource name.
func (s *SecretManager) version(ref ports.SecretRef) (string, error) {
	m := versionName.FindStringSubmatch(string(ref))
	if m == nil || s.allowed(m[1]) != nil {
		return "", fmt.Errorf("secret reference %q: %w", ref, ports.ErrNotFound)
	}
	return string(ref), nil
}

type payload struct {
	Data       string `json:"data"`
	DataCrc32c string `json:"dataCrc32c"`
}

type secretVersion struct {
	Name  string `json:"name"`
	State string `json:"state"` // ENABLED, DISABLED or DESTROYED
}

func crc(b []byte) string { return strconv.FormatUint(uint64(crc32.Checksum(b, castagnoli)), 10) }

// Put implements ports.SecretStore: addVersion, with the payload's CRC32C so Secret Manager refuses a corrupted value.
func (s *SecretManager) Put(ctx context.Context, name, value string) (ports.SecretRef, error) {
	if err := s.allowed(name); err != nil {
		return "", err
	}
	body, err := jsonBody(map[string]any{"payload": payload{Data: base64.StdEncoding.EncodeToString([]byte(value)), DataCrc32c: crc([]byte(value))}})
	if err != nil {
		return "", err
	}
	var out secretVersion
	err = s.r.do(ctx, http.MethodPost, fmt.Sprintf("%s/v1/projects/%s/secrets/%s:addVersion", s.Endpoint, url.PathEscape(s.Project), name), "application/json", body, &out)
	if errors.Is(err, errNotFound) {
		return "", fmt.Errorf("secret %s does not exist: OpenTofu creates each connector secret (infra/gcp/modules/environment)", name)
	}
	if err != nil {
		return "", fmt.Errorf("secret manager add version: %w", err)
	}
	if !versionName.MatchString(out.Name) {
		return "", fmt.Errorf("secret manager add version: the answer names %q", out.Name)
	}
	return ports.SecretRef(out.Name), nil
}

// state reads a version's state.
func (s *SecretManager) state(ctx context.Context, name string) (string, error) {
	var v secretVersion
	if err := s.r.do(ctx, http.MethodGet, fmt.Sprintf("%s/v1/%s", s.Endpoint, name), "", nil, &v); err != nil {
		return "", err
	}
	return v.State, nil
}

// Get implements ports.SecretStore: access, with the answer's CRC32C checked. Secret Manager refuses to give a
// disabled or destroyed version, and the version's state says which.
func (s *SecretManager) Get(ctx context.Context, ref ports.SecretRef) (string, error) {
	name, err := s.version(ref)
	if err != nil {
		return "", err
	}
	var out struct {
		Payload payload `json:"payload"`
	}
	err = s.r.do(ctx, http.MethodGet, fmt.Sprintf("%s/v1/%s:access", s.Endpoint, name), "", nil, &out)
	if err != nil {
		state, serr := s.state(ctx, name)
		switch {
		case errors.Is(serr, errNotFound) || state == "DESTROYED":
			return "", fmt.Errorf("secret %s: %w", ref, ports.ErrNotFound)
		case state == "DISABLED":
			return "", fmt.Errorf("secret %s: %w", ref, ports.ErrSecretDisabled)
		}
		return "", fmt.Errorf("secret manager access: %w", err)
	}
	b, err := base64.StdEncoding.DecodeString(out.Payload.Data)
	if err != nil {
		return "", errors.New("secret manager access: the payload is not base64")
	}
	if out.Payload.DataCrc32c != "" && out.Payload.DataCrc32c != crc(b) {
		return "", errors.New("secret manager access: the payload was corrupted in transit")
	}
	return string(b), nil
}

// Latest implements ports.SecretStore: the enabled version with the highest number.
func (s *SecretManager) Latest(ctx context.Context, name string) (ports.SecretRef, error) {
	if err := s.allowed(name); err != nil {
		return "", err
	}
	best, bestN := "", 0
	token := ""
	for {
		q := url.Values{"filter": {"state:ENABLED"}, "pageSize": {"100"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var out struct {
			Versions      []secretVersion `json:"versions"`
			NextPageToken string          `json:"nextPageToken"`
		}
		err := s.r.do(ctx, http.MethodGet, fmt.Sprintf("%s/v1/projects/%s/secrets/%s/versions?%s", s.Endpoint, url.PathEscape(s.Project), name, q.Encode()), "", nil, &out)
		if errors.Is(err, errNotFound) {
			return "", fmt.Errorf("secret %s: %w", name, ports.ErrNotFound)
		}
		if err != nil {
			return "", fmt.Errorf("secret manager list versions: %w", err)
		}
		for _, v := range out.Versions {
			m := versionName.FindStringSubmatch(v.Name)
			if m == nil || v.State != "ENABLED" {
				continue
			}
			if n, _ := strconv.Atoi(m[2]); n > bestN {
				best, bestN = v.Name, n
			}
		}
		if token = out.NextPageToken; token == "" {
			break
		}
	}
	if best == "" {
		return "", fmt.Errorf("secret %s has no enabled version: %w", name, ports.ErrNotFound)
	}
	return ports.SecretRef(best), nil
}

// act disables or destroys a version, and is idempotent: a version already in the state the action leads to (or
// destroyed, for a disable) is left as it is.
func (s *SecretManager) act(ctx context.Context, ref ports.SecretRef, action string, done ...string) error {
	name, err := s.version(ref)
	if err != nil {
		return err
	}
	body, err := jsonBody(map[string]any{})
	if err != nil {
		return err
	}
	if err := s.r.do(ctx, http.MethodPost, fmt.Sprintf("%s/v1/%s:%s", s.Endpoint, name, action), "application/json", body, nil); err != nil {
		state, serr := s.state(ctx, name)
		if serr == nil {
			for _, d := range done {
				if state == d {
					return nil
				}
			}
		}
		return fmt.Errorf("secret manager %s: %w", action, err)
	}
	return nil
}

// Disable implements ports.SecretStore.
func (s *SecretManager) Disable(ctx context.Context, ref ports.SecretRef) error {
	return s.act(ctx, ref, "disable", "DISABLED", "DESTROYED")
}

// Destroy implements ports.SecretStore.
func (s *SecretManager) Destroy(ctx context.Context, ref ports.SecretRef) error {
	return s.act(ctx, ref, "destroy", "DESTROYED")
}
