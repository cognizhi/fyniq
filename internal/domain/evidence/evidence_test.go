package evidence

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// flagger is a scanner that flags files containing "MALWARE", or fails.
type flagger struct{ fail bool }

func (f flagger) Scan(_ context.Context, r io.Reader) (ports.ScanResult, error) {
	if f.fail {
		return ports.ScanResult{}, errors.New("clamd did not answer")
	}
	b, _ := io.ReadAll(r)
	if bytes.Contains(b, []byte("MALWARE")) {
		return ports.ScanResult{Signature: "Test-Signature"}, nil
	}
	return ports.ScanResult{Clean: true}, nil
}

// jpegWithEXIF is a minimal JPEG: SOI, APP0 JFIF, APP1 EXIF with a location, a comment, SOS and EOI.
func jpegWithEXIF() []byte {
	seg := func(m byte, body string) []byte {
		b := []byte{0xff, m, 0, 0}
		binary.BigEndian.PutUint16(b[2:], uint16(len(body)+2)) // #nosec G115 -- a short test body
		return append(b, body...)
	}
	var b []byte
	b = append(b, 0xff, 0xd8)
	b = append(b, seg(0xe0, "JFIF\x00\x01\x02")...)
	b = append(b, seg(0xe1, "Exif\x00\x00GPS 1.3521N 103.8198E")...)
	b = append(b, seg(0xfe, "taken at home")...)
	b = append(b, seg(0xda, "\x00\x01")...)
	b = append(b, "picture-data"...)
	return append(b, 0xff, 0xd9)
}

func pngWithText() []byte {
	chunk := func(kind, data string) []byte {
		b := make([]byte, 8, 12+len(data))
		binary.BigEndian.PutUint32(b, uint32(len(data))) // #nosec G115 -- a short test chunk
		copy(b[4:], kind)
		b = append(b, data...)
		return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(append([]byte(kind), data...)))
	}
	b := []byte("\x89PNG\r\n\x1a\n")
	b = append(b, chunk("IHDR", "\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00")...)
	b = append(b, chunk("tEXt", "Location\x00Singapore 1.3521N")...)
	b = append(b, chunk("IDAT", "pixels")...)
	return append(b, chunk("IEND", "")...)
}

func webpWithEXIF() []byte {
	chunk := func(kind, data string) []byte {
		b := []byte(kind)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(data))) // #nosec G115 -- a short test chunk
		b = append(b, data...)
		if len(data)%2 == 1 {
			b = append(b, 0)
		}
		return b
	}
	body := append([]byte("WEBP"), chunk("VP8X", "\x0c\x00\x00\x00\x00\x00\x00\x00\x00\x00")...)
	body = append(body, chunk("VP8 ", "frame")...)
	body = append(body, chunk("EXIF", "GPS 1.3521N")...)
	body = append(body, chunk("XMP ", "<x:xmpmeta/>")...)
	b := []byte("RIFF")
	b = binary.LittleEndian.AppendUint32(b, uint32(len(body))) // #nosec G115 -- a short test file
	return append(b, body...)
}

// The type comes from the content, never the name: a PDF named .txt is a PDF, HTML named .pdf is refused,
// and only text picks CSV by its name.
// req: FR-DOC-08
// asvs: V5.2.2
func TestTypeFromContent(t *testing.T) {
	for _, c := range []struct {
		name, data, want string
	}{
		{"receipt.txt", "%PDF-1.7\n...", PDF},
		{"scan.pdf", "\x89PNG\r\n\x1a\n....", PNG},
		{"photo.png", "\xff\xd8\xff\xe0....", JPEG},
		{"photo.jpg", "RIFF\x10\x00\x00\x00WEBPVP8 ", WebP},
		{"ledger.csv", "date,amount\n2026-10-20,12.50\n", CSV},
		{"notes.pdf", "Paid by PayNow on 20 Oct.\n", Text},
		{"invoice.pdf", "<html><script>alert(1)</script></html>", ""},
		{"image.svg", `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`, ""},
		{"book.xlsx", "PK\x03\x04....", ""},
		{"tool.exe", "MZ\x90\x00\x03\x00\x00\x00", ""},
		{"control.txt", "a\x00b", ""},
		{"latin1.txt", "caf\xe9", ""},
	} {
		got, ok := Sniff([]byte(c.data), c.name)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("Sniff(%q named %s) = %q, %v; want %q", c.data, c.name, got, ok, c.want)
		}
	}
}

// office builds a ZIP with the named parts, each holding its text.
func office(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, body := range parts {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(body))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// Office files, emails and Outlook messages are told apart by their structure; macros, ActiveX, compression
// bombs and legacy OLE documents are refused before anything parses them.
// req: FR-DOC-08
// asvs: V5.2.2, V5.2.3
func TestOfficeFilesAndMessages(t *testing.T) {
	types := `<Types><Default Extension="xml" ContentType="application/xml"/></Types>`
	xlsx := office(t, map[string]string{"[Content_Types].xml": types, "xl/workbook.xml": "<workbook/>"})
	docx := office(t, map[string]string{"[Content_Types].xml": types, "word/document.xml": "<document/>"})
	for name, c := range map[string]struct {
		data []byte
		want string
		code string
	}{
		"xlsx":         {xlsx, XLSX, ""},
		"docx":         {docx, DOCX, ""},
		"xlsm":         {office(t, map[string]string{"xl/workbook.xml": "<w/>", "xl/vbaProject.bin": "macro"}), XLSX, CodeMacro},
		"docm":         {office(t, map[string]string{"word/document.xml": "<d/>", "[Content_Types].xml": `<Override ContentType="application/vnd.ms-word.document.macroEnabled.main+xml"/>`}), DOCX, CodeMacro},
		"activex":      {office(t, map[string]string{"word/document.xml": "<d/>", "word/activeX/activeX1.xml": "<x/>"}), DOCX, CodeMacro},
		"bomb":         {office(t, map[string]string{"xl/workbook.xml": strings.Repeat("a", 1<<20)}), XLSX, CodeArchive},
		"other zip":    {office(t, map[string]string{"evil.sh": "rm -rf /"}), "", ""},
		"email":        {[]byte("From: Ann <ann@example.com>\r\nSubject: Receipt\r\nDate: Tue, 20 Oct 2026 09:00:00 +0800\r\n\r\nPaid.\r\n"), EML, ""},
		"not an email": {[]byte("From the desk of Ann\n\nPaid.\n"), Text, ""},
		"outlook":      {append([]byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1...."), []byte("_\x00_\x00s\x00u\x00b\x00s\x00t\x00g\x001\x00.\x000\x00_\x00")...), MSG, ""},
		"legacy doc":   {[]byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1....WordDocument"), "", ""},
	} {
		got, ok := Sniff(c.data, "file")
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s: Sniff = %q, %v; want %q", name, got, ok, c.want)
			continue
		}
		if got != XLSX && got != DOCX {
			continue
		}
		p := CheckArchive(c.data)
		if (p == nil) != (c.code == "") || (p != nil && p.Code != c.code) {
			t.Errorf("%s: CheckArchive = %+v; want %q", name, p, c.code)
		}
	}
	f, ps, err := Check(context.Background(), flagger{}, "macro.xlsx", office(t, map[string]string{"xl/workbook.xml": "<w/>", "xl/vbaProject.bin": "x"}))
	if err != nil || len(ps) != 1 || ps[0].Code != CodeMacro || f.Data != nil {
		t.Errorf("Check of a macro workbook: %v %v", ps, err)
	}
}

// File names are data: no path, no control characters, at most 255 characters.
// req: FR-DOC-08
// asvs: V5.3.2
func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"receipt.pdf":                     "receipt.pdf",
		"../../etc/passwd":                "passwd",
		`C:\Users\me\Desktop\bill.pdf`:    "bill.pdf",
		"bad\r\nname.pdf":                 "bad__name.pdf",
		"":                                "file",
		"..":                              "file",
		strings.Repeat("a", 300) + ".pdf": strings.Repeat("a", 255),
	} {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q; want %q", in, got, want)
		}
	}
}

// Image metadata such as the location is stripped before a picture is stored; the picture itself stays.
// req: FR-DOC-08
// asvs: V5.2.2
func TestImageMetadataIsStripped(t *testing.T) {
	for name, c := range map[string]struct {
		mt   string
		data []byte
		keep string
	}{
		"JPEG": {JPEG, jpegWithEXIF(), "picture-data"},
		"PNG":  {PNG, pngWithText(), "pixels"},
		"WebP": {WebP, webpWithEXIF(), "frame"},
	} {
		out, err := StripMetadata(c.mt, c.data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if bytes.Contains(out, []byte("1.3521N")) || bytes.Contains(out, []byte("taken at home")) || bytes.Contains(out, []byte("xmpmeta")) {
			t.Errorf("%s: the location or comment is still there: %q", name, out)
		}
		if !bytes.Contains(out, []byte(c.keep)) {
			t.Errorf("%s: the picture was lost: %q", name, out)
		}
		if mt, ok := Sniff(out, "x"); !ok || mt != c.mt {
			t.Errorf("%s: the stripped file reads as %q", name, mt)
		}
	}
	if w, _ := StripMetadata(WebP, webpWithEXIF()); w[20]&0x0c != 0 || int(binary.LittleEndian.Uint32(w[4:])) != len(w)-8 {
		t.Errorf("WebP: the EXIF and XMP flags or the RIFF size were not updated: %x", w[:24])
	}
	for _, bad := range [][]byte{[]byte("\xff\xd8\xff\xe1\xff\xff"), []byte("\x89PNG\r\n\x1a\n\x00\x00\x10\x00IDAT"), []byte("RIFF\x00\x00\x00\x00WEBPVP8 \xff\xff\xff\xff")} {
		if _, err := StripMetadata(mustSniff(t, bad), bad); err == nil {
			t.Errorf("a truncated picture %q was accepted", bad)
		}
	}
}

func mustSniff(t *testing.T, b []byte) string {
	t.Helper()
	mt, ok := Sniff(b, "x")
	if !ok {
		t.Fatalf("Sniff(%q) refused", b)
	}
	return mt
}

// Every check of an upload, in order: size, type, the scan of the file as received (a scanner failure
// refuses the file), metadata stripped, and both hashes.
// req: FR-DOC-01
// req: FR-DOC-08
// asvs: V5.2.1, V5.4.3
func TestCheck(t *testing.T) {
	ctx := context.Background()
	codeOf := func(_ File, ps []Problem, err error) string {
		if err != nil {
			return "error"
		}
		if len(ps) == 0 {
			return ""
		}
		return ps[0].Code
	}
	if c := codeOf(Check(ctx, flagger{}, "a.pdf", nil)); c != CodeSize {
		t.Errorf("empty: %s", c)
	}
	if c := codeOf(Check(ctx, flagger{}, "a.pdf", append([]byte("%PDF-"), make([]byte, MaxBytes)...))); c != CodeSize {
		t.Errorf("over 25 MB: %s", c)
	}
	if c := codeOf(Check(ctx, flagger{}, "a.pdf", []byte("MZ\x90\x00"))); c != CodeType {
		t.Errorf("an executable: %s", c)
	}
	if c := codeOf(Check(ctx, flagger{}, "a.pdf", []byte("%PDF-1.7 MALWARE"))); c != CodeMalware {
		t.Errorf("flagged: %s", c)
	}
	if c := codeOf(Check(ctx, flagger{fail: true}, "a.pdf", []byte("%PDF-1.7"))); c != "error" {
		t.Errorf("a failed scan: %s; want the file refused with an error", c)
	}
	if c := codeOf(Check(ctx, nil, "a.pdf", []byte("%PDF-1.7"))); c != "error" {
		t.Errorf("no scanner: %s; want the file refused with an error", c)
	}
	if c := codeOf(Check(ctx, flagger{}, "a.jpg", []byte("\xff\xd8\xff\xe1\xff\xff"))); c != CodeImage {
		t.Errorf("an unreadable JPEG: %s", c)
	}
	f, ps, err := Check(ctx, flagger{}, "../photo.jpg", jpegWithEXIF())
	if err != nil || len(ps) > 0 {
		t.Fatalf("a clean JPEG: %v %v", ps, err)
	}
	if f.Name != "photo.jpg" || f.MediaType != JPEG || f.SHA256 == f.OriginalSHA256 || f.SHA256 != hash(f.Data) || f.OriginalSHA256 != hash(jpegWithEXIF()) {
		t.Errorf("a clean JPEG: %+v", f)
	}
}

// What the uploader says is checked; payroll and identity files wait for payroll permissions.
// req: FR-DOC-05
// req: FR-DOC-09
func TestCheckDetails(t *testing.T) {
	ok := Details{EvidenceType: "receipt", Source: storage.SourceUpload, DataClass: storage.ClassGeneral}
	if ps := CheckDetails(ok); len(ps) != 0 {
		t.Fatalf("valid details: %v", ps)
	}
	for name, d := range map[string]Details{
		"blank type": {EvidenceType: " ", Source: storage.SourceUpload, DataClass: storage.ClassGeneral},
		"long note":  {EvidenceType: "receipt", Note: strings.Repeat("n", 1001), Source: storage.SourceUpload, DataClass: storage.ClassGeneral},
		"payroll":    {EvidenceType: "payslip", Source: storage.SourceUpload, DataClass: storage.ClassPayroll},
		"identity":   {EvidenceType: "passport", Source: storage.SourceUpload, DataClass: storage.ClassIdentity},
		"class":      {EvidenceType: "receipt", Source: storage.SourceUpload, DataClass: "secret"},
		"email":      {EvidenceType: "receipt", Source: storage.SourceEmail, DataClass: storage.ClassGeneral},
	} {
		if ps := CheckDetails(d); len(ps) != 1 {
			t.Errorf("%s: %v; want one problem", name, ps)
		}
	}
}

// A committed file is kept until the end of its financial year plus the records' retention.
// req: FR-DOC-02
func TestRetainUntil(t *testing.T) {
	sgt := time.FixedZone("SGT", 8*3600)
	for _, c := range []struct {
		start int
		at    time.Time
		want  time.Time
	}{
		{1, time.Date(2026, 10, 20, 9, 0, 0, 0, sgt), time.Date(2034, 1, 1, 0, 0, 0, 0, sgt)},
		{4, time.Date(2026, 10, 20, 9, 0, 0, 0, sgt), time.Date(2034, 4, 1, 0, 0, 0, 0, sgt)},
		{4, time.Date(2026, 3, 31, 23, 0, 0, 0, sgt), time.Date(2033, 4, 1, 0, 0, 0, 0, sgt)},
		{0, time.Date(2026, 12, 31, 16, 30, 0, 0, time.UTC), time.Date(2035, 1, 1, 0, 0, 0, 0, sgt)}, // 1 Jan 2027 in Singapore
	} {
		if got := RetainUntil(c.start, 7, c.at, sgt); !got.Equal(c.want) {
			t.Errorf("RetainUntil(%d, 7, %s) = %s; want %s", c.start, c.at, got, c.want)
		}
	}
	if RecordsYears() != 7 {
		t.Errorf("RecordsYears = %d; want the recommended 7", RecordsYears())
	}
}

// Hostile bytes never panic the type check or the metadata stripper (the seed corpus runs in every test).
// req: FR-DOC-08
func FuzzStripMetadata(f *testing.F) {
	for _, s := range [][]byte{jpegWithEXIF(), pngWithText(), webpWithEXIF(), []byte("%PDF-1.7"), []byte("\xff\xd8\xff\xe1\x00\x02")} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if mt, ok := Sniff(b, "x"); ok {
			if out, err := StripMetadata(mt, b); err == nil && len(out) > len(b)+8 {
				t.Fatalf("stripping grew the file from %d to %d bytes", len(b), len(out))
			}
		}
	})
}
