package ports

import (
	"context"
	"io"
)

// BlobStore keeps files (documents, evidence, exports, audit anchors). Production: Cloud
// Storage. Local: a directory under .data/, or fake-gcs-server through the production adapter.
type BlobStore interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}
