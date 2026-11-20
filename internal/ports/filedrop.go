package ports

import (
	"context"
	"time"
)

// FileDrop is a folder a bank leaves its statement files in (host-to-host, FR-BNK-17; p2-bank-feeds-payments task 2.1): it lists the files and reads one, and
// that is all. It writes, renames and deletes nothing, so a statement already delivered stays where the bank put it. The adapters are an SFTP folder
// (internal/adapters/sftp) and, in local mode and tests, a directory (internal/adapters/local).
type FileDrop interface {
	// List is the regular files of the folder, by name; a sub-folder, a link and a hidden file are not listed.
	List(ctx context.Context) ([]DropFile, error)
	// Read is the whole of one listed file, at most max bytes: a larger one is an error, never a part of it.
	Read(ctx context.Context, name string, max int64) ([]byte, error)
}

// DropFile is a file in a drop folder.
type DropFile struct {
	Name    string // a plain file name: no path separator
	Size    int64
	ModTime time.Time
}
