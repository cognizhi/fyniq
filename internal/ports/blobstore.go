package ports

import (
	"context"
	"errors"
	"io"
)

// BlobStore keeps files (documents, evidence, exports, audit anchors). Production: Cloud
// Storage. Local: a directory under .data/, or fake-gcs-server through the production adapter.
type BlobStore interface {
	Put(ctx context.Context, key string, r io.Reader) error
	// Create writes a blob that must not exist yet and returns ErrExists if it does: write-once,
	// for audit anchors (in the cloud the bucket's retention lock also stops deletion).
	Create(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// List returns the keys under a prefix, sorted.
	List(ctx context.Context, prefix string) ([]string, error)
	Delete(ctx context.Context, key string) error
}

// ErrExists is returned by Create when the blob is already there.
var ErrExists = errors.New("the blob already exists")
