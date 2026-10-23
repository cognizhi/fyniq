package gcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// metadataIdentityURL mints an ID token of the service's own account for an audience.
const metadataIdentityURL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/identity"

// IdentityTokens are ID tokens of the service's own account for audience, from the metadata server, reused until a
// minute before they expire: how the api and the worker call the private render service (PRD-001 §12.3, rev 0.96),
// which accepts its callers' accounts only (CallerTokens).
func IdentityTokens(audience string, client *http.Client) func(ctx context.Context) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	src := oauth2.ReuseTokenSourceWithExpiry(nil, identity{audience: audience, client: client, endpoint: metadataIdentityURL}, time.Minute)
	return func(context.Context) (string, error) {
		t, err := src.Token()
		if err != nil {
			return "", err
		}
		return t.AccessToken, nil
	}
}

type identity struct {
	audience, endpoint string
	client             *http.Client
}

func (i identity) Token() (*oauth2.Token, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.endpoint+"?"+url.Values{"audience": {i.audience}, "format": {"full"}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := i.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the metadata server did not answer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the metadata server refused an ID token: %s", resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(string(raw))
	exp, err := expiry(token)
	if err != nil {
		return nil, err
	}
	return &oauth2.Token{AccessToken: token, TokenType: "Bearer", Expiry: exp}, nil
}

// expiry reads an ID token's exp claim; the token is not verified here: its audience verifies it.
func expiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, errors.New("the metadata server's ID token is not a JWT")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, errors.New("the metadata server's ID token cannot be read")
	}
	var c struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(b, &c); err != nil || c.Exp == 0 {
		return time.Time{}, errors.New("the metadata server's ID token has no expiry")
	}
	return time.Unix(c.Exp, 0), nil
}
