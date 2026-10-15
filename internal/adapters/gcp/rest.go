package gcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/clock"
)

// The Google Cloud adapters call the services' REST APIs over HTTP rather than the client libraries:
// the images stay lean (NFR-IMG-04), and each adapter is tested against a fake or the local emulator.
// They authenticate as the service's own account (SEC-03) with a token from the metadata server.

// MetadataTokens are the service account's access tokens from the metadata server of Cloud Run (and
// any Google Cloud runtime), reused until a minute before they expire.
func MetadataTokens() oauth2.TokenSource {
	return oauth2.ReuseTokenSourceWithExpiry(nil, metadata{}, time.Minute)
}

const metadataAccessURL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token"

type metadata struct{}

func (metadata) Token() (*oauth2.Token, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataAccessURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the metadata server did not answer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the metadata server refused a token: %s", resp.Status)
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&t); err != nil {
		return nil, fmt.Errorf("the metadata server's token cannot be read")
	}
	return &oauth2.Token{AccessToken: t.AccessToken, TokenType: t.TokenType, Expiry: clock.System{}.Now().Add(time.Duration(t.ExpiresIn) * time.Second)}, nil
}

// errNotFound is a 404 from a Google API.
var errNotFound = errors.New("not found")

// errPrecondition is a 412: a precondition such as ifGenerationMatch failed.
var errPrecondition = errors.New("precondition failed")

// errConflict is a 409: the resource exists already, such as a task with the same name.
var errConflict = errors.New("already exists")

// rest sends one request to a Google API, with the access token when there is a token source (the
// local emulators need none), and decodes a JSON answer into out. Errors name the method and the
// status, never a token or a body.
type rest struct {
	tokens oauth2.TokenSource
	client *http.Client
}

func (r *rest) do(ctx context.Context, method, url, contentType string, body io.Reader, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if r.tokens != nil {
		t, err := r.tokens.Token()
		if err != nil {
			return fmt.Errorf("no access token: %w", err)
		}
		t.SetAuthHeader(req)
	}
	client := r.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: the service did not answer", method)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errNotFound
	case resp.StatusCode == http.StatusPreconditionFailed:
		return errPrecondition
	case resp.StatusCode == http.StatusConflict:
		return errConflict
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return fmt.Errorf("%s: %s", method, resp.Status)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil
	}
	if w, ok := out.(io.Writer); ok {
		_, err := io.Copy(w, resp.Body)
		return err
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

func jsonBody(v any) (io.Reader, error) {
	b, err := json.Marshal(v)
	return bytes.NewReader(b), err
}
