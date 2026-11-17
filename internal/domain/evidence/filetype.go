package evidence

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxBytes is the largest file accepted: 25 MB (FR-DOC-08's default; a setting later).
const MaxBytes = 25 << 20

// The media types accepted, decided from the content (FR-DOC-08). HEIC waits for its conversion to JPEG.
const (
	PDF  = "application/pdf"
	PNG  = "image/png"
	JPEG = "image/jpeg"
	WebP = "image/webp"
	Text = "text/plain"
	CSV  = "text/csv"
	XLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	DOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	EML  = "message/rfc822"
	MSG  = "application/vnd.ms-outlook"
)

// Accepted lists what each accepted type is, for the messages.
var Accepted = "PDF, PNG, JPEG, WebP, CSV, plain text, XLSX, DOCX, EML or MSG"

// Sniff decides a file's type from its content, never from its name or the client's Content-Type: only text
// picks CSV over plain text by its name, since both are text. ok is false for anything not accepted. An XLSX
// or DOCX still has to pass CheckArchive.
func Sniff(data []byte, name string) (mediaType string, ok bool) {
	switch {
	case bytes.HasPrefix(data, []byte("%PDF-")):
		return PDF, true
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return PNG, true
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return JPEG, true
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return WebP, true
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		return officeType(data)
	case bytes.HasPrefix(data, []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1")):
		// An OLE compound file: an Outlook message has its MAPI property streams; a legacy .doc or .xls, which
		// may carry macros, is refused.
		if bytes.Contains(data, []byte("_\x00_\x00s\x00u\x00b\x00s\x00t\x00g\x001\x00.\x000\x00_\x00")) {
			return MSG, true
		}
		return "", false
	}
	if !isText(data) {
		return "", false
	}
	if isEmail(data) {
		return EML, true
	}
	if strings.EqualFold(path.Ext(name), ".csv") {
		return CSV, true
	}
	return Text, true
}

// heifBrands are the brands of an ISO base media file that hold HEVC pictures: an iPhone's HEIC photo.
var heifBrands = []string{"heic", "heix", "hevc", "hevx", "heim", "heis", "hevm", "hevs"}

// IsHEIC reports whether data is a HEIC picture by its file-type box: its major brand, or a generic HEIF brand (mif1,
// msf1) with a HEVC brand among the compatible ones. It is refused with its own message until its conversion to
// JPEG is built (FR-DOC-08, rev 0.38).
func IsHEIC(data []byte) bool {
	if len(data) < 16 || string(data[4:8]) != "ftyp" {
		return false
	}
	size := int(binary.BigEndian.Uint32(data[:4]))
	if size < 16 || size > len(data) || size > 4096 {
		size = min(len(data), 4096)
	}
	major := string(data[8:12])
	if slices.Contains(heifBrands, major) {
		return true
	}
	if major != "mif1" && major != "msf1" {
		return false
	}
	for i := 16; i+4 <= size; i += 4 { // the compatible brands, after the minor version
		if slices.Contains(heifBrands, string(data[i:i+4])) {
			return true
		}
	}
	return false
}

// officeType tells an XLSX from a DOCX by its parts; any other ZIP is refused.
func officeType(data []byte) (string, bool) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", false
	}
	for _, f := range z.File {
		switch f.Name {
		case "xl/workbook.xml":
			return XLSX, true
		case "word/document.xml":
			return DOCX, true
		}
	}
	return "", false
}

// isEmail reports whether text starts with an RFC 5322 header block that has a From and a Date, Subject,
// Message-ID or Received header.
func isEmail(data []byte) bool {
	head, _, _ := bytes.Cut(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), []byte("\n\n"))
	var from, other bool
	for _, line := range strings.Split(string(head), "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' { // a folded line continues the header above
			continue
		}
		name, _, ok := strings.Cut(line, ":")
		if !ok || name == "" || strings.ContainsAny(name, " \t") {
			return false
		}
		switch strings.ToLower(name) {
		case "from":
			from = true
		case "date", "subject", "message-id", "received":
			other = true
		}
	}
	return from && other
}

// Archive limits for XLSX and DOCX (FR-DOC-08), checked before anything reads the parts.
const (
	maxEntries      = 10000
	maxUncompressed = 200 << 20
	maxRatio        = 100
)

// CheckArchive checks an XLSX or DOCX before anything parses it (FR-DOC-08): its entry count, total
// uncompressed size and compression ratio, and no macros or ActiveX controls. It reads only the central
// directory and, bounded, the content-types part.
func CheckArchive(data []byte) *Problem {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return &Problem{CodeArchive, "file", "The file's archive structure cannot be read, so it was not stored.", "Open it and save it again, then attach that."}
	}
	if len(z.File) > maxEntries {
		return &Problem{CodeArchive, "file", fmt.Sprintf("The file holds %d parts; at most %d are accepted.", len(z.File), maxEntries), "Save a smaller copy, or export it as PDF."}
	}
	var total uint64
	for _, f := range z.File {
		total += f.UncompressedSize64
		if total > maxUncompressed || (f.CompressedSize64 > 0 && f.UncompressedSize64/f.CompressedSize64 > maxRatio) ||
			(f.CompressedSize64 == 0 && f.UncompressedSize64 > 0) {
			return &Problem{CodeArchive, "file", "The file expands far beyond its size when opened, as a compression bomb does, so it was not stored.",
				"Export it as PDF and attach that."}
		}
		lower := strings.ToLower(f.Name)
		if strings.Contains(lower, "vbaproject") || strings.Contains(lower, "vbadata") || strings.Contains(lower, "activex") {
			return &Problem{CodeMacro, "file", "The file contains macros or ActiveX controls, which are refused.",
				"Save it without macros (as .xlsx or .docx, not .xlsm or .docm), or export it as PDF, and attach that."}
		}
		if f.Name == "[Content_Types].xml" {
			rc, err := f.Open()
			if err != nil {
				return &Problem{CodeArchive, "file", "The file's parts cannot be read, so it was not stored.", "Open it and save it again, then attach that."}
			}
			types, _ := io.ReadAll(io.LimitReader(rc, 1<<20))
			_ = rc.Close()
			if bytes.Contains(bytes.ToLower(types), []byte("macroenabled")) || bytes.Contains(bytes.ToLower(types), []byte("vbaproject")) {
				return &Problem{CodeMacro, "file", "The file is macro-enabled, which is refused.",
					"Save it without macros (as .xlsx or .docx, not .xlsm or .docm), or export it as PDF, and attach that."}
			}
		}
	}
	return nil
}

// isText reports whether data is UTF-8 text that no browser would take for markup or a script: printable
// characters, tabs and line breaks only, and not what http.DetectContentType calls HTML or XML.
func isText(data []byte) bool {
	if len(data) == 0 || !utf8.Valid(data) {
		return false
	}
	if ct := http.DetectContentType(data); !strings.HasPrefix(ct, "text/plain") {
		return false
	}
	for _, r := range string(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))) {
		if r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			continue
		}
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// SafeName keeps a file's name as data (FR-DOC-08): its last path element, with control characters, line
// separators and invisible format characters replaced (a right-to-left override makes "invoice\u202efdp.exe" read as
// a PDF), at most 255 characters, no space at either end; "file" when nothing is left. It is never used as a path or
// a key, and a safe name is its own safe name.
func SafeName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.ToValidUTF8(name, "_")
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) || r == unicode.ReplacementChar {
			return '_'
		}
		return r
	}, name)
	if r := []rune(name); len(r) > 255 {
		name = string(r[:255])
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return name
}
