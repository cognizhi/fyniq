package evidence

import (
	"archive/zip"
	"bytes"
	"io"
	"regexp"
	"strings"
)

// The NRIC pattern scan (PRD-001 FR-DOC-09; rev 0.183): a file whose text shows a Singapore NRIC or FIN in full is
// flagged, so whoever opens it knows it holds an unmasked identifier; a masked one, such as S****567D, is not. The
// text is read from plain text, CSV and email files as they are, from the parts of an XLSX or DOCX file that hold
// text, and from a PDF's bytes as they are, which finds only the text a PDF does not compress: a best effort, until
// the redaction tool (P2) reads every page.
//
// An S, T, F or G number is flagged only when its check letter is right, so an invoice number of the same shape
// rarely is. An M number (FINs from 2022) is flagged on its shape alone: its check letters are not confirmed from an
// official source [verify].

var nricShape = regexp.MustCompile(`(?i)\b([STFGM])(\d{7})([A-Z])\b`)

// The check letters of S and T numbers, and of F and G numbers, by the remainder of the weighted sum.
const (
	stLetters = "JZIHGFEDCBA"
	fgLetters = "XWUTRQPNMLK"
)

var nricWeights = [7]int{2, 7, 6, 5, 4, 3, 2}

// ValidNRIC reports whether s, such as S1234567D, is an NRIC or FIN whose check letter is right; an M number on its
// shape alone.
func ValidNRIC(s string) bool {
	m := nricShape.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(s)))
	if m == nil || len(m[0]) != len(strings.TrimSpace(s)) {
		return false
	}
	prefix, digits, letter := m[1], m[2], m[3]
	if prefix == "M" {
		return true // [verify] the M series' check letters
	}
	sum := 0
	for i, d := range digits {
		sum += int(d-'0') * nricWeights[i]
	}
	if prefix == "T" || prefix == "G" {
		sum += 4
	}
	letters := stLetters
	if prefix == "F" || prefix == "G" {
		letters = fgLetters
	}
	return letters[sum%11] == letter[0]
}

// UnmaskedNRIC reports whether a file of media type mt shows an NRIC or FIN in full.
func UnmaskedNRIC(mt string, data []byte) bool {
	var text []byte
	switch mt {
	case Text, CSV, EML, PDF:
		text = data
	case XLSX, DOCX:
		text = officeText(data)
	default:
		return false // pictures, and Outlook's binary messages, are not read
	}
	for _, m := range nricShape.FindAll(text, -1) {
		if ValidNRIC(string(m)) {
			return true
		}
	}
	return false
}

// officeText is the text of an XLSX or DOCX file's parts that hold it, their XML tags taken out; CheckArchive has
// bounded the archive already.
func officeText(data []byte) []byte {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil
	}
	var out bytes.Buffer
	for _, f := range z.File {
		if f.Name != "word/document.xml" && f.Name != "xl/sharedStrings.xml" && !strings.HasPrefix(f.Name, "xl/worksheets/") {
			continue
		}
		r, err := f.Open()
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(r, MaxBytes))
		_ = r.Close()
		if err != nil {
			continue
		}
		// A paragraph's or a cell's end parts words; other tags, such as Word's runs, may split one, so they go.
		b = boundaryTags.ReplaceAll(b, []byte(" "))
		out.Write(xmlTags.ReplaceAll(b, nil))
		out.WriteByte(' ')
	}
	return out.Bytes()
}

var (
	boundaryTags = regexp.MustCompile(`</(w:p|w:tc|si|c|row)>`)
	xmlTags      = regexp.MustCompile(`<[^>]*>`)
)
