package ports

import "context"

// DatabaseBackup exports the database (DR-02, DR-04, NFR-DR-01; p1-operations-dr design D1): the weekly copy that survives
// the loss of the production project, and the monthly one kept a year.
type DatabaseBackup interface {
	// Export writes the whole database, as gzip-compressed SQL, to uri (gs://<bucket>/<name>, or a bucket and name the
	// local adapter maps to a folder), and returns once it is written, with what it did in words.
	Export(ctx context.Context, uri string) (string, error)
}
