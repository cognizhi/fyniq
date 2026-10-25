//go:build local

package local

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/cognizhi/fyniq/internal/ports"
)

// SecretFiles is the local flavour's ports.SecretStore: each version of a secret is a file <root>/<name>/<n>, readable
// only by its owner, <n>.disabled once disabled, and an empty <n>.destroyed once destroyed, so a number is never
// used twice. Only local builds have it (DEV-07); the root is git-ignored
// (.data/secrets).
type SecretFiles struct {
	root string
	mu   sync.Mutex
}

// NewSecretFiles keeps secrets under root, which it creates.
func NewSecretFiles(root string) (*SecretFiles, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &SecretFiles{root: root}, nil
}

const localRef = "local:"

// Put implements ports.SecretStore.
func (s *SecretFiles) Put(_ context.Context, name, value string) (ports.SecretRef, error) {
	if err := ports.CheckSecretName(name); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	n := 0
	for _, v := range s.versions(name) {
		n = max(n, v)
	}
	n++
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(n)), []byte(value), 0o600); err != nil {
		return "", err
	}
	return ports.SecretRef(fmt.Sprintf("%s%s/%d", localRef, name, n)), nil
}

// versions are every version number name ever had, destroyed ones included.
func (s *SecretFiles) versions(name string) []int {
	entries, err := os.ReadDir(filepath.Join(s.root, name))
	if err != nil {
		return nil
	}
	var out []int
	for _, e := range entries {
		v, _, _ := strings.Cut(e.Name(), ".")
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

// path is the file of ref, enabled or disabled.
func (s *SecretFiles) path(ref ports.SecretRef) (string, error) {
	rest, ok := strings.CutPrefix(string(ref), localRef)
	name, version, ok2 := strings.Cut(rest, "/")
	n, err := strconv.Atoi(version)
	if !ok || !ok2 || err != nil || n < 1 || ports.CheckSecretName(name) != nil {
		return "", fmt.Errorf("secret reference %q: %w", ref, ports.ErrNotFound)
	}
	return filepath.Join(s.root, name, strconv.Itoa(n)), nil
}

// Get implements ports.SecretStore.
func (s *SecretFiles) Get(_ context.Context, ref ports.SecretRef) (string, error) {
	p, err := s.path(ref)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(p) // #nosec G304 -- the path is built from a checked name and a number under the store's root
	if errors.Is(err, fs.ErrNotExist) {
		if _, derr := os.Stat(p + ".disabled"); derr == nil {
			return "", fmt.Errorf("secret %s: %w", ref, ports.ErrSecretDisabled)
		}
		return "", fmt.Errorf("secret %s: %w", ref, ports.ErrNotFound)
	}
	return string(b), err
}

// Latest implements ports.SecretStore.
func (s *SecretFiles) Latest(_ context.Context, name string) (ports.SecretRef, error) {
	if err := ports.CheckSecretName(name); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	best := 0
	for _, n := range s.versions(name) {
		if _, err := os.Stat(filepath.Join(s.root, name, strconv.Itoa(n))); err == nil {
			best = max(best, n)
		}
	}
	if best == 0 {
		return "", fmt.Errorf("secret %s has no enabled version: %w", name, ports.ErrNotFound)
	}
	return ports.SecretRef(fmt.Sprintf("%s%s/%d", localRef, name, best)), nil
}

// Disable implements ports.SecretStore.
func (s *SecretFiles) Disable(_ context.Context, ref ports.SecretRef) error {
	p, err := s.path(ref)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Rename(p, p+".disabled"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Destroy implements ports.SecretStore.
func (s *SecretFiles) Destroy(_ context.Context, ref ports.SecretRef) error {
	p, err := s.path(ref)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range []string{p, p + ".disabled"} {
		if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if _, err := os.Stat(filepath.Dir(p)); err != nil {
		return nil // a secret that never was: nothing to mark
	}
	return os.WriteFile(p+".destroyed", nil, 0o600)
}

var _ ports.SecretStore = (*SecretFiles)(nil)
