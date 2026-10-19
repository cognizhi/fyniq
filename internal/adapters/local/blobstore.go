//go:build local

package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
)

// retentionDir holds each retained blob's retain-until and legal hold, under the root and outside the keys.
const retentionDir = ".retention"

// Dir keeps blobs as files under a root directory (normally .data/blobs). Retention (FR-DOC-02) has no
// local lock, so Dir enforces it in code: a retained blob cannot be deleted or replaced through it.
type Dir struct {
	root string
	// Clock decides whether a retain-until has passed; nil reads the wall clock.
	Clock ports.Clock
	mu    sync.Mutex // orders retention changes against deletes and puts
}

// retention is what Dir records for a retained blob.
type retention struct {
	RetainUntil time.Time `json:"retain_until"`
	LegalHold   bool      `json:"legal_hold,omitempty"`
}

// NewDir returns a store rooted at root, creating it if needed.
func NewDir(root string) (*Dir, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Dir{root: root}, nil
}

func (d *Dir) path(key string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(key))
	if key == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) ||
		clean == retentionDir || strings.HasPrefix(clean, retentionDir+string(filepath.Separator)) {
		return "", fmt.Errorf("blob key %q is outside the store: use a relative key such as org/1/file.pdf", key)
	}
	return filepath.Join(d.root, clean), nil
}

func (d *Dir) now() time.Time {
	if d.Clock != nil {
		return d.Clock.Now()
	}
	return clock.System{}.Now()
}

// retentionPath is where key's retention is recorded.
func (d *Dir) retentionPath(key string) (string, error) {
	p, err := d.path(key)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(d.root, p)
	if err != nil {
		return "", err
	}
	return filepath.Join(d.root, retentionDir, rel+".json"), nil
}

func (d *Dir) readRetention(key string) (retention, error) {
	var r retention
	p, err := d.retentionPath(key)
	if err != nil {
		return r, err
	}
	b, err := os.ReadFile(p) // #nosec G304 -- p is under the store's root: path() refuses keys outside it
	if errors.Is(err, fs.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("blob %s: its retention record cannot be read, so it is kept: %w", key, ports.ErrRetained)
	}
	return r, nil
}

func (d *Dir) writeRetention(key string, r retention) error {
	p, err := d.retentionPath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".put-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// retained returns ErrRetained when key is under retention or legal hold now.
func (d *Dir) retained(key string) error {
	r, err := d.readRetention(key)
	if err != nil {
		return err
	}
	if r.LegalHold || r.RetainUntil.After(d.now()) {
		return fmt.Errorf("blob %s: %w", key, ports.ErrRetained)
	}
	return nil
}

func (d *Dir) exists(key string) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("blob %s: %w", key, ports.ErrNotFound)
	} else if err != nil {
		return err
	}
	return nil
}

// SetRetainUntil implements ports.BlobStore: the retain-until only moves later (FR-DOC-02).
func (d *Dir) SetRetainUntil(_ context.Context, key string, until time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.exists(key); err != nil {
		return err
	}
	r, err := d.readRetention(key)
	if err != nil {
		return err
	}
	if until.Before(r.RetainUntil) {
		return fmt.Errorf("blob %s is kept until %s, not %s: %w", key, r.RetainUntil.UTC().Format(time.RFC3339),
			until.UTC().Format(time.RFC3339), ports.ErrRetentionShortened)
	}
	r.RetainUntil = until.UTC()
	return d.writeRetention(key, r)
}

// SetLegalHold implements ports.BlobStore.
func (d *Dir) SetLegalHold(_ context.Context, key string, hold bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.exists(key); err != nil {
		return err
	}
	r, err := d.readRetention(key)
	if err != nil {
		return err
	}
	r.LegalHold = hold
	return d.writeRetention(key, r)
}

// Put writes r under key, replacing any existing blob unless it is retained.
func (d *Dir) Put(_ context.Context, key string, r io.Reader) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.retained(key); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".put-*")
	if err != nil {
		return err
	}
	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// Get opens the blob at key.
func (d *Dir) Get(_ context.Context, key string) (io.ReadCloser, error) {
	p, err := d.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p) // #nosec G304 -- p is under the store's root: path() refuses keys outside it
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("blob %s: %w", key, ports.ErrNotFound)
	}
	return f, err
}

// Delete removes the blob at key unless it is retained; deleting a missing blob is not an error.
func (d *Dir) Delete(_ context.Context, key string) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.retained(key); err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if rp, err := d.retentionPath(key); err == nil {
		_ = os.Remove(rp) // an expired retention record goes with its blob
	}
	return nil
}

var _ ports.BlobStore = (*Dir)(nil)

// Create writes r under key only if nothing is there yet (O_EXCL); it returns ports.ErrExists
// otherwise. The bucket's retention lock has no local equivalent: CTL-AUD-03 checks it in staging.
func (d *Dir) Create(_ context.Context, key string, r io.Reader) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- p is under the store's root
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("blob %s: %w", key, ports.ErrExists)
	}
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(p)
		return err
	}
	return f.Close()
}

// List returns the keys under prefix, sorted.
func (d *Dir) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	err := filepath.WalkDir(d.root, func(p string, e fs.DirEntry, err error) error {
		if err == nil && e.IsDir() && p == filepath.Join(d.root, retentionDir) {
			return fs.SkipDir
		}
		if err != nil || e.IsDir() || strings.HasPrefix(e.Name(), ".put-") {
			return err
		}
		rel, err := filepath.Rel(d.root, p)
		if err != nil {
			return err
		}
		if key := filepath.ToSlash(rel); strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
		return nil
	})
	sort.Strings(keys)
	return keys, err
}
