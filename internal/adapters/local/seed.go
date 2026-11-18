package local

import (
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// loadOrCreateSeed reads the key seed at path, or makes one of size random bytes there (0600) on first use. Services
// that share a data directory may start together (the local stack's shared volume): the new seed is written whole to a
// file of its own and linked into place, which fails when another got there first, whose seed is read instead, so every
// service holds the same key and none reads a seed half written.
func loadOrCreateSeed(path string, size int) ([]byte, error) {
	seed, err := os.ReadFile(path) // #nosec G304 -- the path is the deployment's own data directory
	if !errors.Is(err, fs.ErrNotExist) {
		return seed, err
	}
	seed = make([]byte, size)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(dir, ".seed-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(seed); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Link(tmp.Name(), path); errors.Is(err, fs.ErrExist) {
		return os.ReadFile(path) // #nosec G304 -- another service made it first
	} else if err != nil {
		return nil, err
	}
	return seed, nil
}
