package gcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
)

// V4 signed URLs of Cloud Storage objects (SEC-24; p1-security-hardening design D5): a link that works for minutes, served
// by Cloud Storage as an attachment with the stored file's name and type. It is signed as the service's own account
// through the IAM Credentials API's signBlob, so no key exists anywhere (SEC-04). The canonical request and the string
// to sign follow Google's V4 signing process, pinned by its conformance vectors (signedurl_test.go).

// storageHost is Cloud Storage's host for path-style URLs.
const storageHost = "storage.googleapis.com"

// SignBlob signs payload as a service account, which email names.
type SignBlob func(ctx context.Context, payload []byte) (email string, signature []byte, err error)

// rfc3986 percent-encodes s as V4 signing does: every byte but the unreserved characters.
func rfc3986(s string, keepSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9', c == '-', c == '.', c == '_', c == '~', keepSlash && c == '/':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// v4 is the canonical query, canonical request and string to sign of a signed GET of object in bucket, signed as email
// at at, valid for ttl, with the extra query parameters (such as the response's headers).
func v4(bucket, object, email string, at time.Time, ttl time.Duration, extra map[string]string) (query, canonical, toSign string) {
	at = at.UTC()
	date, stamp := at.Format("20060102"), at.Format("20060102T150405Z")
	scope := date + "/auto/storage/goog4_request"
	params := map[string]string{
		"X-Goog-Algorithm":     "GOOG4-RSA-SHA256",
		"X-Goog-Credential":    email + "/" + scope,
		"X-Goog-Date":          stamp,
		"X-Goog-Expires":       strconv.Itoa(int(ttl / time.Second)),
		"X-Goog-SignedHeaders": "host",
	}
	for k, v := range extra {
		params[k] = v
	}
	pairs := make([]string, 0, len(params))
	for k, v := range params {
		pairs = append(pairs, rfc3986(k, false)+"="+rfc3986(v, false))
	}
	sort.Strings(pairs)
	query = strings.Join(pairs, "&")
	canonical = strings.Join([]string{"GET", "/" + bucket + "/" + rfc3986(object, true), query, "host:" + storageHost, "", "host", "UNSIGNED-PAYLOAD"}, "\n")
	sum := sha256.Sum256([]byte(canonical))
	toSign = strings.Join([]string{"GOOG4-RSA-SHA256", stamp, scope, hex.EncodeToString(sum[:])}, "\n")
	return query, canonical, toSign
}

// attachment is a Content-Disposition that saves the file as name: an ASCII fallback, and the name itself in UTF-8.
func attachment(name string) string {
	if name == "" {
		name = "file"
	}
	return mime.FormatMediaType("attachment", map[string]string{"filename": name})
}

// DownloadURL implements ports.DownloadLinker: a V4 signed URL of the object at key, at most 7 days, served as an
// attachment named d.FileName of type d.MediaType.
func (s *Storage) DownloadURL(ctx context.Context, key string, d ports.Download, at time.Time) (string, error) {
	if s.Sign == nil {
		return "", errors.New("cloud storage: no signer for download links")
	}
	if d.TTL <= 0 || d.TTL > 7*24*time.Hour {
		return "", fmt.Errorf("cloud storage: a download link lives between a second and 7 days, not %s", d.TTL)
	}
	extra := map[string]string{"response-content-disposition": attachment(d.FileName)}
	if d.MediaType != "" {
		extra["response-content-type"] = d.MediaType
	}
	email, _, err := s.Sign(ctx, nil) // the account's email, without signing anything
	if err != nil {
		return "", err
	}
	query, _, toSign := v4(s.Bucket, key, email, at, d.TTL, extra)
	_, sig, err := s.Sign(ctx, []byte(toSign))
	if err != nil {
		return "", err
	}
	return "https://" + storageHost + "/" + s.Bucket + "/" + rfc3986(key, true) + "?" + query + "&X-Goog-Signature=" + hex.EncodeToString(sig), nil
}

// iamCredentials is the IAM Credentials API, which signs as a service account.
const iamCredentials = "https://iamcredentials.googleapis.com" // #nosec G101 -- the API's address, not a credential

// metadataEmailURL is the service's own account.
const metadataEmailURL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/email"

// IAMSigner signs as the service's own account through the IAM Credentials API's signBlob, which the account must be
// allowed on itself (roles/iam.serviceAccountTokenCreator, the environment module) [live-verify]. The account's email
// comes from the metadata server, once. A nil payload only resolves the email.
func IAMSigner(tokens oauth2.TokenSource, client *http.Client) SignBlob {
	var once sync.Once
	var email string
	var emailErr error
	if client == nil {
		client = egress.Public()
	}
	r := rest{tokens: tokens, client: client}
	return func(ctx context.Context, payload []byte) (string, []byte, error) {
		once.Do(func() { email, emailErr = metadataEmail(ctx) })
		if emailErr != nil || payload == nil {
			return email, nil, emailErr
		}
		body, err := jsonBody(map[string]string{"payload": base64.StdEncoding.EncodeToString(payload)})
		if err != nil {
			return "", nil, err
		}
		var out struct {
			SignedBlob string `json:"signedBlob"`
		}
		u := iamCredentials + "/v1/projects/-/serviceAccounts/" + url.PathEscape(email) + ":signBlob"
		if err := r.do(ctx, http.MethodPost, u, "application/json", body, &out); err != nil {
			return "", nil, fmt.Errorf("sign a download link: %w", err)
		}
		sig, err := base64.StdEncoding.DecodeString(out.SignedBlob)
		return email, sig, err
	}
}

// metadataEmail is the service's own account, from the metadata server.
func metadataEmail(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataEmailURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := egress.Metadata().Do(req)
	if err != nil {
		return "", fmt.Errorf("the metadata server did not answer: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil || resp.StatusCode != http.StatusOK || !bytes.Contains(b, []byte("@")) {
		return "", fmt.Errorf("the metadata server gave no account email (%s)", resp.Status)
	}
	return strings.TrimSpace(string(b)), nil
}
