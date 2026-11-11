package gcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
)

// StorageAPI is Cloud Storage's JSON API.
const StorageAPI = "https://storage.googleapis.com"

// Storage is the BlobStore port on a Cloud Storage bucket, through the JSON API. Locally the same
// adapter talks to fake-gcs-server (§12.8); the bucket's retention lock, which no emulator has, is
// checked in staging and production (CTL-AUD-02, CTL-AUD-03).
type Storage struct {
	Endpoint string // StorageAPI, or the emulator's address
	Bucket   string
	// Clock decides whether an object's retain-until has passed, for the adapter's own check; nil reads the
	// wall clock. Cloud Storage judges by its own clock.
	Clock ports.Clock
	// Sign signs download links as the service's own account (IAMSigner); nil: the store gives none (SEC-24).
	Sign SignBlob
	r    rest
}

var (
	_ ports.BlobStore      = (*Storage)(nil)
	_ ports.DownloadLinker = (*Storage)(nil)
)

// NewStorage returns the adapter; tokens is nil for the emulator.
func NewStorage(endpoint, bucket string, tokens oauth2.TokenSource, client *http.Client) *Storage {
	return &Storage{Endpoint: strings.TrimSuffix(endpoint, "/"), Bucket: bucket, r: rest{tokens: tokens, client: client}}
}

func (s *Storage) object(key string) string {
	return fmt.Sprintf("%s/storage/v1/b/%s/o/%s", s.Endpoint, url.PathEscape(s.Bucket), url.PathEscape(key))
}

// Chunk is how much of a blob an upload holds at a time: a blob that fits is sent in one request; a longer one, such as
// an export written as it is read (NFR-PERF-05, rev 0.250), goes as a resumable upload, a chunk at a time, so it is never
// held whole. Cloud Storage wants every chunk but the last to be a multiple of 256 KiB.
var Chunk = 8 << 20

func (s *Storage) upload(ctx context.Context, key string, r io.Reader, extra url.Values) error {
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "..") {
		return fmt.Errorf("blob key %q is not a relative key such as org/1/file.pdf", key)
	}
	cur := make([]byte, Chunk)
	n, err := io.ReadFull(r, cur)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) { // it fits: one request
		q := url.Values{"uploadType": {"media"}, "name": {key}}
		for k, v := range extra {
			q[k] = v
		}
		u := fmt.Sprintf("%s/upload/storage/v1/b/%s/o?%s", s.Endpoint, url.PathEscape(s.Bucket), q.Encode())
		return s.r.do(ctx, http.MethodPost, u, "application/octet-stream", bytes.NewReader(cur[:n]), nil)
	}
	if err != nil {
		return err
	}
	session, err := s.resumable(ctx, key, extra)
	if err != nil {
		return err
	}
	ahead := make([]byte, Chunk)
	var offset int64
	for {
		m, rerr := io.ReadFull(r, ahead) // reading ahead tells whether cur is the last chunk
		end := errors.Is(rerr, io.EOF) || errors.Is(rerr, io.ErrUnexpectedEOF)
		if rerr != nil && !end {
			s.cancel(ctx, session)
			return rerr
		}
		if end && m == 0 {
			return s.chunk(ctx, session, cur, offset, true)
		}
		if err := s.chunk(ctx, session, cur, offset, false); err != nil {
			s.cancel(ctx, session)
			return err
		}
		offset += int64(len(cur))
		if end {
			return s.chunk(ctx, session, ahead[:m], offset, true)
		}
		cur, ahead = ahead, cur
	}
}

// resumable starts a resumable upload of key and is its session's address, on the store's own host.
func (s *Storage) resumable(ctx context.Context, key string, extra url.Values) (string, error) {
	q := url.Values{"uploadType": {"resumable"}, "name": {key}}
	for k, v := range extra {
		q[k] = v
	}
	u := fmt.Sprintf("%s/upload/storage/v1/b/%s/o?%s", s.Endpoint, url.PathEscape(s.Bucket), q.Encode())
	status, h, err := s.r.send(ctx, http.MethodPost, u, nil, nil)
	switch {
	case err != nil:
		return "", err
	case status == http.StatusPreconditionFailed:
		return "", errPrecondition
	case status != http.StatusOK && status != http.StatusCreated:
		return "", fmt.Errorf("POST: the resumable upload was not started: %d", status)
	}
	session := h.Get("Location")
	if !strings.HasPrefix(session, s.Endpoint+"/") { // the token goes only to the store it was meant for
		return "", fmt.Errorf("POST: the resumable upload's session is not on %s", s.Endpoint)
	}
	return session, nil
}

// chunk sends the bytes of a resumable upload from offset; the last one names the blob's size and finishes it.
func (s *Storage) chunk(ctx context.Context, session string, b []byte, offset int64, last bool) error {
	total := "*"
	if last {
		total = strconv.FormatInt(offset+int64(len(b)), 10)
	}
	rng := fmt.Sprintf("bytes %d-%d/%s", offset, offset+int64(len(b))-1, total)
	status, _, err := s.r.send(ctx, http.MethodPut, session, http.Header{"Content-Range": {rng}}, bytes.NewReader(b))
	switch {
	case err != nil:
		return err
	case status == http.StatusPreconditionFailed:
		return errPrecondition
	case last && (status == http.StatusOK || status == http.StatusCreated):
		return nil
	case !last && status == http.StatusPermanentRedirect: // 308: Resume Incomplete, the chunk kept
		return nil
	}
	return fmt.Errorf("PUT: a chunk of the resumable upload was not kept: %d", status)
}

// cancel ends a resumable upload that will not finish, so the store keeps none of it.
func (s *Storage) cancel(ctx context.Context, session string) {
	_, _, _ = s.r.send(context.WithoutCancel(ctx), http.MethodDelete, session, nil, nil)
}

// Put implements ports.BlobStore: a retained object is never replaced.
func (s *Storage) Put(ctx context.Context, key string, r io.Reader) error {
	if err := s.retained(ctx, key); err != nil {
		return err
	}
	return s.upload(ctx, key, r, nil)
}

// Create implements ports.BlobStore: write once, with the precondition that no live object has the
// name (ifGenerationMatch=0), so two writers of the same anchor cannot both succeed.
func (s *Storage) Create(ctx context.Context, key string, r io.Reader) error {
	err := s.upload(ctx, key, r, url.Values{"ifGenerationMatch": {"0"}})
	if errors.Is(err, errPrecondition) {
		return ports.ErrExists
	}
	return err
}

// Get implements ports.BlobStore.
func (s *Storage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	var b bytes.Buffer
	err := s.r.do(ctx, http.MethodGet, s.object(key)+"?alt=media", "", nil, &b)
	if errors.Is(err, errNotFound) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return io.NopCloser(&b), nil
}

// List implements ports.BlobStore.
func (s *Storage) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for page := ""; ; {
		q := url.Values{"prefix": {prefix}, "fields": {"items(name),nextPageToken"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		var out struct {
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := s.r.do(ctx, http.MethodGet, fmt.Sprintf("%s/storage/v1/b/%s/o?%s", s.Endpoint, url.PathEscape(s.Bucket), q.Encode()), "", nil, &out); err != nil {
			return nil, err
		}
		for _, it := range out.Items {
			keys = append(keys, it.Name)
		}
		if page = out.NextPageToken; page == "" {
			break
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// Delete implements ports.BlobStore. In a bucket under a retention policy, Cloud Storage refuses to
// delete an object younger than the retention period, whoever asks.
func (s *Storage) Delete(ctx context.Context, key string) error {
	if err := s.retained(ctx, key); err != nil {
		return err
	}
	err := s.r.do(ctx, http.MethodDelete, s.object(key), "", nil, nil)
	if errors.Is(err, errNotFound) {
		return nil
	}
	return err
}

// The object's retention (FR-DOC-02): Cloud Storage's Object Retention Lock and temporary hold enforce it
// in the bucket; the adapter checks them as well before it deletes or replaces an object, so the emulator,
// which records a retention but enforces nothing, behaves as the cloud does. The hold is also kept in the
// object's metadata, which the emulator keeps.
const holdMetadata = "fyniq-legal-hold"

type objectRetention struct {
	Retention *struct {
		Mode            string    `json:"mode"`
		RetainUntilTime time.Time `json:"retainUntilTime"`
	} `json:"retention"`
	TemporaryHold bool              `json:"temporaryHold"`
	Metadata      map[string]string `json:"metadata"`
}

func (o objectRetention) until() time.Time {
	if o.Retention == nil {
		return time.Time{}
	}
	return o.Retention.RetainUntilTime
}

func (o objectRetention) held() bool { return o.TemporaryHold || o.Metadata[holdMetadata] == "true" }

func (s *Storage) now() time.Time {
	if s.Clock != nil {
		return s.Clock.Now()
	}
	return clock.System{}.Now()
}

// retention reads an object's retention; ErrNotFound when there is no object.
func (s *Storage) retention(ctx context.Context, key string) (objectRetention, error) {
	var o objectRetention
	err := s.r.do(ctx, http.MethodGet, s.object(key)+"?fields=retention,temporaryHold,metadata", "", nil, &o)
	if errors.Is(err, errNotFound) {
		return o, fmt.Errorf("blob %s: %w", key, ports.ErrNotFound)
	}
	return o, err
}

// retained returns ErrRetained when the object is under retention or hold; nil when it is free or missing.
func (s *Storage) retained(ctx context.Context, key string) error {
	o, err := s.retention(ctx, key)
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if o.held() || o.until().After(s.now()) {
		return fmt.Errorf("blob %s: %w", key, ports.ErrRetained)
	}
	return nil
}

func (s *Storage) patch(ctx context.Context, key string, body any) error {
	b, err := jsonBody(body)
	if err != nil {
		return err
	}
	err = s.r.do(ctx, http.MethodPatch, s.object(key), "application/json", b, nil)
	if errors.Is(err, errNotFound) {
		return fmt.Errorf("blob %s: %w", key, ports.ErrNotFound)
	}
	return err
}

// SetRetainUntil implements ports.BlobStore with a locked object retention: Cloud Storage itself refuses to
// shorten one, and the adapter refuses first.
func (s *Storage) SetRetainUntil(ctx context.Context, key string, until time.Time) error {
	o, err := s.retention(ctx, key)
	if err != nil {
		return err
	}
	if until.Before(o.until()) {
		return fmt.Errorf("blob %s is kept until %s, not %s: %w", key, o.until().UTC().Format(time.RFC3339),
			until.UTC().Format(time.RFC3339), ports.ErrRetentionShortened)
	}
	if until.Equal(o.until()) {
		return nil // already so; a locked retention is patched only to move it later
	}
	return s.patch(ctx, key, map[string]any{"retention": map[string]any{"mode": "Locked",
		"retainUntilTime": until.UTC().Format(time.RFC3339Nano)}})
}

// SetLegalHold implements ports.BlobStore with a temporary hold, which does not restart the retention
// clock (FR-DOC-02).
func (s *Storage) SetLegalHold(ctx context.Context, key string, hold bool) error {
	if _, err := s.retention(ctx, key); err != nil {
		return err
	}
	return s.patch(ctx, key, map[string]any{"temporaryHold": hold, "metadata": map[string]any{holdMetadata: strconv.FormatBool(hold)}})
}

// RetentionOf implements ports.BlobStore: the object's retention as Cloud Storage keeps it (CTL-AUD-03).
func (s *Storage) RetentionOf(ctx context.Context, key string) (ports.BlobRetention, error) {
	o, err := s.retention(ctx, key)
	if err != nil {
		return ports.BlobRetention{}, err
	}
	return ports.BlobRetention{Locked: o.Retention != nil && o.Retention.Mode == "Locked", RetainUntil: o.until(), Held: o.held()}, nil
}

// Retention is a bucket's retention policy.
type Retention struct {
	Period time.Duration
	Locked bool // Bucket Lock: the policy can no longer be shortened or removed
}

// Retention reads the bucket's retention policy (CTL-AUD-02: the anchors' bucket must be locked).
func (s *Storage) Retention(ctx context.Context) (Retention, error) {
	var out struct {
		RetentionPolicy *struct {
			RetentionPeriod string `json:"retentionPeriod"`
			IsLocked        bool   `json:"isLocked"`
		} `json:"retentionPolicy"`
	}
	u := fmt.Sprintf("%s/storage/v1/b/%s?fields=retentionPolicy", s.Endpoint, url.PathEscape(s.Bucket))
	if err := s.r.do(ctx, http.MethodGet, u, "", nil, &out); err != nil {
		return Retention{}, err
	}
	if out.RetentionPolicy == nil {
		return Retention{}, nil
	}
	secs, err := strconv.ParseInt(out.RetentionPolicy.RetentionPeriod, 10, 64)
	if err != nil {
		return Retention{}, fmt.Errorf("the retention period %q is not a number of seconds", out.RetentionPolicy.RetentionPeriod)
	}
	return Retention{Period: time.Duration(secs) * time.Second, Locked: out.RetentionPolicy.IsLocked}, nil
}
