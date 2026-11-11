//go:build local

package local

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cognizhi/fyniq/internal/ports"
)

// PGDump is local mode's stand-in for Cloud SQL's export (DR-02, DR-04): pg_dump of the local database into Dir, one
// folder per bucket, when pg_dump is on the PATH (make dev on the host); the stack's images have none, and then the
// export is skipped and says so.
type PGDump struct {
	URL string // the database, as the migrator reaches it
	Dir string
}

var _ ports.DatabaseBackup = PGDump{}

// Export implements ports.DatabaseBackup; uri is gs://<bucket>/<name>.
func (p PGDump) Export(ctx context.Context, uri string) (string, error) {
	rest, gs := strings.CutPrefix(uri, "gs://")
	bucket, name, ok := strings.Cut(rest, "/")
	if !gs || !ok || bucket == "" || name == "" || strings.Contains(name, "..") {
		return "", fmt.Errorf("%q is not gs://<bucket>/<name>", uri)
	}
	bin, err := exec.LookPath("pg_dump")
	if err != nil {
		return "skipped: no pg_dump here, so local mode keeps no export", nil
	}
	path := filepath.Join(p.Dir, bucket, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- under the configured folder
	if err != nil {
		return "", err
	}
	defer f.Close()
	z := gzip.NewWriter(f)
	cmd := exec.CommandContext(ctx, bin, "--no-owner", "--dbname", p.URL) // #nosec G204 -- pg_dump of the local database
	cmd.Stdout, cmd.Stderr = z, io.Discard
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pg_dump: %w", err)
	}
	if err := z.Close(); err != nil {
		return "", err
	}
	return "exported to " + path, f.Sync()
}
