package ports

import (
	"context"
	"time"
)

// DatabaseBackup exports the database (DR-02, DR-04, NFR-DR-01; p1-operations-dr design D1): the weekly copy that survives
// the loss of the production project, and the monthly one kept a year.
type DatabaseBackup interface {
	// Export writes the whole database, as gzip-compressed SQL, to uri (gs://<bucket>/<name>, or a bucket and name the
	// local adapter maps to a folder), and returns once it is written, with what it did in words.
	Export(ctx context.Context, uri string) (string, error)
}

// DatabaseClone makes a throwaway copy of the database as it was at a time, for the monthly restore drill (DR-06,
// CTL-RES-02; p1-operations-dr design D2), and removes it. Production: a Cloud SQL clone at a point in time, in the
// production project on the platform's private network, which no service is configured to reach. Local: a copy of the
// database made from it as a template, which cannot go back in time.
type DatabaseClone interface {
	// Clone copies the database as it was at at, and says where the copy is. Only a copy it made can be removed.
	Clone(ctx context.Context, at time.Time) (CloneTarget, error)
	// Remove deletes the copy named name; a missing copy is not an error, and a name that is not a drill's copy is refused.
	Remove(ctx context.Context, name string) error
}

// CloneTarget is a copy of the database: its name, and where it is: the host its database listens on (Cloud SQL's private
// address), or the database's name locally. The services' own database URLs, pointed at it, reach it as their own roles.
type CloneTarget struct {
	Name     string
	Host     string
	Database string
}
