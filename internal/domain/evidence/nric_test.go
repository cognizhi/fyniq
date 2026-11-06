package evidence

import "testing"

// The NRIC pattern scan (FR-DOC-09, rev 0.183): an NRIC or FIN shown in full is found in text, CSV and email files and
// in the text of XLSX and DOCX files, even across Word's runs; one whose check letter is wrong, or a masked one, is not.
// req: FR-DOC-09
func TestUnmaskedNRICsAreFound(t *testing.T) {
	for s, want := range map[string]bool{
		"S1234567D": true,  // 1·2+2·7+3·6+4·5+5·4+6·3+7·2 = 106; 106 mod 11 = 7: D
		"s1234567d": true,  // as people type it
		"T1234567J": true,  // 106 + 4 = 110; 0: J
		"F1234567N": true,  // 106 mod 11 = 7 in XWUTRQPNMLK: N
		"G1234567X": true,  // 110 mod 11 = 0: X
		"S1234567A": false, // the wrong check letter: an invoice number of the same shape
		"S123456D":  false, // too short
		"X1234567D": false,
	} {
		if got := ValidNRIC(s); got != want {
			t.Errorf("ValidNRIC(%q) = %v, want %v", s, got, want)
		}
	}
	for name, c := range map[string]struct {
		mt   string
		data []byte
		want bool
	}{
		"a CSV":                   {CSV, []byte("name,id\nTan Ah Kow,S1234567D\n"), true},
		"a masked one":            {CSV, []byte("name,id\nTan Ah Kow,S****567D\n"), false},
		"an invoice number":       {Text, []byte("Invoice S1234567A, total 109.00"), false},
		"an email":                {EML, []byte("Subject: payslip\r\n\r\nNRIC: T1234567J\r\n"), true},
		"a picture":               {PNG, []byte("S1234567D"), false},
		"a DOCX split in runs":    {DOCX, office(t, map[string]string{"word/document.xml": `<w:p><w:r><w:t>NRIC S123</w:t></w:r><w:r><w:t>4567D</w:t></w:r></w:p>`}), true},
		"an XLSX's shared string": {XLSX, office(t, map[string]string{"xl/sharedStrings.xml": `<sst><si><t>Name</t></si><si><t>F1234567N</t></si></sst>`}), true},
	} {
		if got := UnmaskedNRIC(c.mt, c.data); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}
