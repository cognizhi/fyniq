package evidence

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// corpus is the fuzz corpus per type of FR-DOC-08: a file of each type Check accepts (PDF, PNG, JPEG, WebP, CSV, plain
// text, XLSX, DOCX, EML and MSG), with what makes each type hard (image metadata, a macro, an external entity, a
// compression bomb, an NRIC in the text), and the types it refuses (HEIC, a legacy OLE document, HTML, SVG, an
// executable). Each is made in code, so no hostile file sits in the repository; go test runs every one, and
// go test -fuzz grows the corpus from them.
func corpus(tb testing.TB) map[string]struct {
	name string
	data []byte
} {
	types := `<Types><Default Extension="xml" ContentType="application/xml"/></Types>`
	return map[string]struct {
		name string
		data []byte
	}{
		"pdf":            {"receipt.pdf", []byte("%PDF-1.7\n1 0 obj << /Type /Catalog /OpenAction << /S /JavaScript /JS (app.alert(1)) >> >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n")},
		"pdf with nric":  {"payslip.pdf", []byte("%PDF-1.4\nBT (NRIC S1234567D) Tj ET\n%%EOF\n")},
		"png":            {"scan.png", pngWithText()},
		"jpeg":           {"photo.jpg", jpegWithEXIF()},
		"webp":           {"photo.webp", webpWithEXIF()},
		"csv":            {"staff.csv", []byte("name,id,amount\nTan Ah Kow,T1234567J,12.50\n=HYPERLINK(\"http://x\"),,-1\n")},
		"text":           {"notes.txt", []byte("Paid by PayNow on 20 Oct.\r\nRef: S1234567A\n")},
		"xlsx":           {"book.xlsx", office(tb, map[string]string{"[Content_Types].xml": types, "xl/workbook.xml": "<workbook/>", "xl/sharedStrings.xml": "<sst><si><t>F1234567N</t></si></sst>"})},
		"xlsm":           {"book.xlsx", office(tb, map[string]string{"xl/workbook.xml": "<w/>", "xl/vbaProject.bin": "macro"})},
		"docx":           {"letter.docx", office(tb, map[string]string{"[Content_Types].xml": types, "word/document.xml": "<w:p><w:r><w:t>NRIC S123</w:t></w:r><w:r><w:t>4567D</w:t></w:r></w:p>"})},
		"docx with xxe":  {"letter.docx", office(tb, map[string]string{"word/document.xml": `<?xml version="1.0"?><!DOCTYPE d [<!ENTITY x SYSTEM "http://127.0.0.1:1/x">]><d>&x;</d>`})},
		"docx bomb":      {"letter.docx", office(tb, map[string]string{"word/document.xml": strings.Repeat("a", 1<<20)})},
		"eml":            {"receipt.eml", []byte("From: Ann <ann@example.com>\r\nSubject: Receipt\r\nDate: Tue, 20 Oct 2026 09:00:00 +0800\r\n\r\nNRIC: T1234567J\r\n")},
		"msg":            {"mail.msg", append([]byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1...."), []byte("_\x00_\x00s\x00u\x00b\x00s\x00t\x00g\x001\x00.\x000\x00_\x00")...)},
		"legacy ole doc": {"old.doc", []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1....WordDocument")},
		"heic":           {"IMG_2201.HEIC", []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic")},
		"html as pdf":    {"statement.pdf", []byte("<html><script>fetch('/api/v1/session')</script></html>")},
		"svg":            {"logo.svg", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`)},
		"exe as pdf":     {"invoice‮fdp.exe", []byte("MZ\x90\x00\x03\x00\x00\x00\x04\x00")},
		"empty":          {"../../etc/passwd", nil},
	}
}

// accepted are the types Check stores.
var accepted = map[string]bool{PDF: true, PNG: true, JPEG: true, WebP: true, Text: true, CSV: true, XLSX: true, DOCX: true, EML: true, MSG: true}

// Whatever bytes and name arrive, Check never panics and stores only what passed: a file of an accepted type that is
// still that type once stripped, stripped again to the same bytes, hashed as stored and as received, at most a few
// bytes larger than it came (a WebP's padding), and named safely; a refused file keeps nothing. An XLSX or DOCX it stores passed the archive checks. A
// file the scanner flags is refused.
// req: FR-DOC-08
// req: FR-DOC-01
// asvs: V5.2.2, V5.2.3, V5.3.2
func FuzzCheck(f *testing.F) {
	for _, c := range corpus(f) {
		f.Add(c.name, c.data)
		f.Add(c.name, append(append([]byte{}, c.data...), "MALWARE"...))
	}
	f.Fuzz(func(t *testing.T, name string, data []byte) {
		file, problems, err := Check(context.Background(), flagger{}, name, data)
		if err != nil {
			t.Fatalf("the scanner answered, yet: %v", err)
		}
		if file.Name != SafeName(name) {
			t.Fatalf("the name %q is kept as %q, not its safe name", name, file.Name)
		}
		if len(problems) > 0 {
			if file.Data != nil || file.SHA256 != "" || file.MediaType != "" {
				t.Fatalf("a refused file (%s) kept %d bytes as %q", problems[0].Code, len(file.Data), file.MediaType)
			}
			return
		}
		if bytes.Contains(data, []byte("MALWARE")) {
			t.Fatal("a file the scanner flags was stored")
		}
		if !accepted[file.MediaType] {
			t.Fatalf("stored as %q, not an accepted type", file.MediaType)
		}
		if mt, ok := Sniff(file.Data, file.Name); !ok || mt != file.MediaType {
			t.Fatalf("stored as %s, its stripped bytes read as %q", file.MediaType, mt)
		}
		if again, err := StripMetadata(file.MediaType, file.Data); err != nil || !bytes.Equal(again, file.Data) {
			t.Fatalf("stripping the stored %s again changed it (%v)", file.MediaType, err)
		}
		if file.SHA256 != hash(file.Data) || file.OriginalSHA256 != hash(data) || len(file.Data) > len(data)+8 {
			t.Fatalf("the hashes or the size of what is stored: %d bytes from %d", len(file.Data), len(data))
		}
		if (file.MediaType == XLSX || file.MediaType == DOCX) && CheckArchive(data) != nil {
			t.Fatal("an office file stored past its archive checks")
		}
		if file.NRIC != UnmaskedNRIC(file.MediaType, data) {
			t.Fatal("the NRIC flag is not the text's")
		}
	})
}

// An archive's checks never panic, read only its directory and the content types, and pass nothing past the limits:
// at most 10,000 parts, 200 MB uncompressed, a ratio of 100 per part, and no macro or ActiveX part.
// req: FR-DOC-08
// asvs: V5.2.3
func FuzzCheckArchive(f *testing.F) {
	for _, k := range []string{"xlsx", "xlsm", "docx", "docx with xxe", "docx bomb"} {
		f.Add(corpus(f)[k].data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if CheckArchive(data) != nil {
			return
		}
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal("an unreadable archive passed")
		}
		var total uint64
		for _, e := range z.File {
			total += e.UncompressedSize64
			lower := strings.ToLower(e.Name)
			if strings.Contains(lower, "vbaproject") || strings.Contains(lower, "activex") {
				t.Fatalf("the part %q passed", e.Name)
			}
		}
		if len(z.File) > maxEntries || total > maxUncompressed {
			t.Fatalf("%d parts, %d bytes uncompressed, passed", len(z.File), total)
		}
	})
}

// Reading a file's text for an NRIC never panics, for any type, and finds one only where a number with its right
// check letter is in the text: a type with no text read (pictures, Outlook's binary messages) is never flagged.
// req: FR-DOC-09
// req: FR-DOC-08
func FuzzUnmaskedNRIC(f *testing.F) {
	for _, c := range corpus(f) {
		mt, _ := Sniff(c.data, c.name)
		f.Add(mt, c.data)
	}
	f.Fuzz(func(t *testing.T, mt string, data []byte) {
		got := UnmaskedNRIC(mt, data)
		switch mt {
		case Text, CSV, EML, PDF, XLSX, DOCX:
		default:
			if got {
				t.Fatalf("a %q file was read for an NRIC", mt)
			}
		}
		if got && mt != XLSX && mt != DOCX {
			found := false
			for _, m := range nricShape.FindAll(data, -1) {
				found = found || ValidNRIC(string(m))
			}
			if !found {
				t.Fatal("flagged with no valid NRIC in the text")
			}
		}
	})
}

// A safe name has no path, no control, line-separating or invisible format character, at most 255 characters and no
// space at either end, is never empty or a directory's name, and is its own safe name (FR-DOC-08: names are data).
// req: FR-DOC-08
// asvs: V5.3.2
func FuzzSafeName(f *testing.F) {
	for _, c := range corpus(f) {
		f.Add(c.name)
	}
	for _, s := range []string{"inv\r\nSet-Cookie: s=1.pdf", "evil.pdf\x00.exe", "..", " . ", "a b.pdf", "caf\xe9.pdf", strings.Repeat("a", 254) + " b"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		s := SafeName(name)
		if s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\`) || !utf8.ValidString(s) || utf8.RuneCountInString(s) > 255 ||
			strings.TrimSpace(s) != s {
			t.Fatalf("SafeName(%q) = %q", name, s)
		}
		for _, r := range s {
			if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
				t.Fatalf("SafeName(%q) = %q keeps %U", name, s, r)
			}
		}
		if again := SafeName(s); again != s {
			t.Fatalf("SafeName(%q) = %q, and again %q", name, s, again)
		}
	})
}

// Until its conversion to JPEG is built, a HEIC photo is refused with a message of its own that says to export it as
// JPEG (FR-DOC-08, rev 0.38), told by its file-type box: an iPhone's heic brand, or a generic HEIF brand with a HEVC
// one among the compatible brands; an AVIF picture or an MP4 video is refused as any other type.
// req: FR-DOC-08
func TestHEICIsRefusedWithItsOwnMessage(t *testing.T) {
	ftyp := func(major string, compatible ...string) []byte {
		b := []byte("\x00\x00\x00\x00ftyp" + major + "\x00\x00\x00\x00" + strings.Join(compatible, ""))
		b[3] = byte(len(b))
		return append(b, "\x00\x00\x00\x08meta"...)
	}
	for name, c := range map[string]struct {
		data []byte
		heic bool
	}{
		"an iPhone photo":        {ftyp("heic", "mif1", "heic"), true},
		"a burst":                {ftyp("hevc", "msf1", "hevc"), true},
		"a generic HEIF of HEVC": {ftyp("mif1", "mif1", "heic"), true},
		"an AVIF picture":        {ftyp("avif", "mif1", "avif"), false},
		"a generic HEIF of AV1":  {ftyp("mif1", "mif1", "avif"), false},
		"an MP4 video":           {ftyp("isom", "isom", "mp41"), false},
		"too short":              {[]byte("\x00\x00\x00\x18ftypheic"), false},
	} {
		if got := IsHEIC(c.data); got != c.heic {
			t.Errorf("%s: IsHEIC = %v", name, got)
		}
		_, ps, err := Check(context.Background(), flagger{}, "IMG_2201.HEIC", c.data)
		if err != nil || len(ps) != 1 || ps[0].Code != CodeType || strings.Contains(ps[0].Message, "HEIC") != c.heic ||
			strings.Contains(ps[0].Fix, "JPEG and attach") != c.heic {
			t.Errorf("%s: Check = %+v %v", name, ps, err)
		}
	}
}
