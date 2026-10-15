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
	r        rest
}

var _ ports.BlobStore = (*Storage)(nil)

// NewStorage returns the adapter; tokens is nil for the emulator.
func NewStorage(endpoint, bucket string, tokens oauth2.TokenSource, client *http.Client) *Storage {
	return &Storage{Endpoint: strings.TrimSuffix(endpoint, "/"), Bucket: bucket, r: rest{tokens: tokens, client: client}}
}

func (s *Storage) object(key string) string {
	return fmt.Sprintf("%s/storage/v1/b/%s/o/%s", s.Endpoint, url.PathEscape(s.Bucket), url.PathEscape(key))
}

func (s *Storage) upload(ctx context.Context, key string, r io.Reader, extra url.Values) error {
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "..") {
		return fmt.Errorf("blob key %q is not a relative key such as org/1/file.pdf", key)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	q := url.Values{"uploadType": {"media"}, "name": {key}}
	for k, v := range extra {
		q[k] = v
	}
	u := fmt.Sprintf("%s/upload/storage/v1/b/%s/o?%s", s.Endpoint, url.PathEscape(s.Bucket), q.Encode())
	return s.r.do(ctx, http.MethodPost, u, "application/octet-stream", bytes.NewReader(b), nil)
}

// Put implements ports.BlobStore.
func (s *Storage) Put(ctx context.Context, key string, r io.Reader) error {
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
	err := s.r.do(ctx, http.MethodDelete, s.object(key), "", nil, nil)
	if errors.Is(err, errNotFound) {
		return nil
	}
	return err
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
