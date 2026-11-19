package ports

import (
	"context"
	"errors"
)

// ConvertKind is what a Converter makes of a file (FR-DOC-08).
type ConvertKind string

const (
	// ConvertHEICToJPEG turns a HEIC photo, as an iPhone takes by default, into a JPEG picture.
	ConvertHEICToJPEG ConvertKind = "heic_jpeg"
	// ConvertPreviewPNG renders the first page of a PDF, or of an office file, to a PNG picture without running anything
	// in it: no script, form or external link (the safe preview).
	ConvertPreviewPNG ConvertKind = "preview_png"
)

// Converted is a converter's output: the picture and its type. It is untrusted until checked: the caller scans it and
// decides its type from its content, as it does any file.
type Converted struct {
	MediaType string // image/jpeg or image/png
	Data      []byte
}

// ErrNotConvertible is a file the converter reads but cannot convert, such as a damaged photo or an encrypted PDF.
var ErrNotConvertible = errors.New("the file cannot be converted")

// Converter turns files into pictures in a sandbox of its own, so that no parser of a hostile format runs in the API or
// the worker (SEC-24; PRD-001 FR-DOC-08). A converter that cannot decide returns an error, never a picture. In the
// cloud it is a private service like the scanner; locally a stand-in serves tests, and without either configured a HEIC
// file is refused with a plain message.
type Converter interface {
	// Convert reads data, of at most 25 MB, and returns the picture of kind. ErrNotConvertible means the file itself is
	// the problem; any other error means the converter was not available.
	Convert(ctx context.Context, kind ConvertKind, data []byte) (Converted, error)
}
