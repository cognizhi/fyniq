package brand

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// MaxPrint is the largest logo for documents and emails the render service draws and a document carries.
const MaxPrint = 512 << 10

// Key is where a brand file is kept in the blob store: by organisation and content, so it is written once.
func Key(org uuid.UUID, sum []byte) string {
	return "brand/" + org.String() + "/" + hex.EncodeToString(sum)
}

// InForce is the brand version in force at t among versions (newest first, as storage.Brand lists them): the latest
// saved at or before t. A document shows the brand in force when it was issued, so drawn again it shows the same logo
// (FR-ORG-09, FR-DOC-04). ok is false before the first version.
func InForce(versions []storage.BrandVersion, t time.Time) (storage.BrandVersion, bool) {
	for _, v := range versions {
		if !v.CreatedAt.After(t) {
			return v, true
		}
	}
	return storage.BrandVersion{}, false
}

// PrintLogo reads v's logo for documents and emails from blobs, checked against the SHA-256 it was saved with: nil
// when v has none.
func PrintLogo(ctx context.Context, blobs ports.BlobStore, org uuid.UUID, v storage.BrandVersion) ([]byte, error) {
	if v.Print == nil || blobs == nil {
		return nil, nil
	}
	r, err := blobs.Get(ctx, Key(org, v.Print.SHA256))
	if err != nil {
		return nil, fmt.Errorf("brand: the logo for documents of version %d: %w", v.Version, err)
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, MaxPrint+1))
	if err != nil {
		return nil, err
	}
	if sum := sha256.Sum256(data); !bytes.Equal(sum[:], v.Print.SHA256) {
		return nil, fmt.Errorf("brand: the logo for documents of version %d does not match its record", v.Version)
	}
	return data, nil
}

// Themed is c with the brand's accent, when c is the platform's starter: the organisation's own templates keep the
// accent their authors chose (FR-ORG-09: the brand sets the default theme of document templates, §8.25).
func Themed(c doctemplate.Content, source string, v storage.BrandVersion) doctemplate.Content {
	if source == doctemplate.SourcePlatform && v.Accent != "" {
		c.Theme.Accent = v.Accent
	}
	return c
}

// At is the brand version in force at t, read in u: the zero version before the first.
func At(ctx context.Context, u storage.UnitOfWork, t time.Time) (storage.BrandVersion, error) {
	all, err := u.Brand().Versions(ctx)
	v, _ := InForce(all, t)
	return v, err
}

// Current is the latest brand version, read in u: the zero version before the first. Previews show it.
func Current(ctx context.Context, u storage.UnitOfWork) (storage.BrandVersion, error) {
	all, err := u.Brand().Versions(ctx)
	if err != nil || len(all) == 0 {
		return storage.BrandVersion{}, err
	}
	return all[0], nil
}
