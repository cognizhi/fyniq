// Package capture is the document inbox's reading (PRD-001 FR-DOC-12, FR-DOC-14, FR-AP-04, FR-AP-12; p1-document-capture design D3 and D5): the
// shape of what the model reads from a file, each field with its confidence, and the platform's own checks of it. A reading is untrusted data
// (AI-18): it never decides a figure, a GST eligibility or a posting, and a check can only lower a field's confidence, never raise it.
package capture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// Kinds are what a reading says a document is; P1 drafts bills and claims, the rest are kept for the person.
var Kinds = []string{"bill", "receipt", "sales_invoice", "statement", "contract", "other"}

// LowConfidence is the confidence below which a field is highlighted for the person who reviews the draft (design D5; a default, FR-ORG-08).
const LowConfidence = 0.80

// Text is a field the model read: its value, null when the document does not show it, and how sure the model is, from 0 to 1.
type Text struct {
	Value      *string `json:"value" doc:"What the document shows, exactly as printed; null when it does not show it." example:"Acme Software Pte. Ltd."`
	Confidence float64 `json:"confidence" doc:"How sure the model is, from 0 to 1." example:"0.95"`
}

// Flag is a yes or no the model read, such as a document stating itself a tax invoice.
type Flag struct {
	Value      *bool   `json:"value" doc:"The yes or no the document shows; null when it does not show it." example:"true"`
	Confidence float64 `json:"confidence" doc:"How sure the model is, from 0 to 1." example:"0.9"`
}

// Line is a line of the document: what it is, its net amount, its GST code if the document prints one, the GST it states and its total.
type Line struct {
	Description Text `json:"description"`
	Net         Text `json:"net"`
	GSTCode     Text `json:"gst_code"`
	GST         Text `json:"gst"`
	Amount      Text `json:"amount"`
}

// Reading is what the model read from one document. Amounts are decimal strings exactly as printed, never numbers (ENG-10).
type Reading struct {
	Kind              string `json:"kind" enum:"bill,receipt,sales_invoice,statement,contract,other" doc:"What the document is." example:"bill"`
	Supplier          Text   `json:"supplier"`
	SupplierUEN       Text   `json:"supplier_uen"`
	GSTRegistrationNo Text   `json:"gst_registration_no"`
	SupplierAddress   Text   `json:"supplier_address"`
	Number            Text   `json:"number"`
	Date              Text   `json:"date"`
	DueDate           Text   `json:"due_date"`
	Currency          Text   `json:"currency"`
	Lines             []Line `json:"lines"`
	GSTTotal          Text   `json:"gst_total"`
	Total             Text   `json:"total"`
	// GSTInSGD is the GST in Singapore dollars as the document prints it, for a document in another currency (G4-029).
	GSTInSGD Text `json:"gst_in_sgd"`
	// TaxInvoiceLabel is whether the document is headed a tax invoice; CustomerNamed whether it names the customer.
	TaxInvoiceLabel Flag `json:"tax_invoice_label"`
	CustomerNamed   Flag `json:"customer_named"`
}

var decimal = regexp.MustCompile(`^-?[0-9]{1,15}(\.[0-9]{1,4})?$`)

// Parse reads the model's answer strictly: the shape of Reading and nothing more, a kind it knows, amounts as decimal strings and every
// confidence from 0 to 1. A reading that does not fit is refused as a whole: nothing of it is kept.
func Parse(raw []byte) (Reading, error) {
	var r Reading
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return Reading{}, fmt.Errorf("the reading does not fit its shape: %w", err)
	}
	if d.More() {
		return Reading{}, fmt.Errorf("the reading has more than one object")
	}
	if !slices.Contains(Kinds, r.Kind) {
		return Reading{}, fmt.Errorf("the reading's kind %q is not one of %s", r.Kind, strings.Join(Kinds, ", "))
	}
	if len(r.Lines) > 100 {
		return Reading{}, fmt.Errorf("the reading has %d lines; the most is 100", len(r.Lines))
	}
	for name, f := range r.fields() {
		if f.Confidence < 0 || f.Confidence > 1 {
			return Reading{}, fmt.Errorf("%s: a confidence is from 0 to 1", name)
		}
		if f.Value != nil && len(*f.Value) > 500 {
			return Reading{}, fmt.Errorf("%s: a value is at most 500 characters", name)
		}
	}
	amounts := []struct {
		name string
		t    Text
	}{{"gst_total", r.GSTTotal}, {"total", r.Total}, {"gst_in_sgd", r.GSTInSGD}}
	for i, l := range r.Lines {
		for _, f := range []struct {
			n string
			t Text
		}{{"net", l.Net}, {"gst", l.GST}, {"amount", l.Amount}} {
			amounts = append(amounts, struct {
				name string
				t    Text
			}{fmt.Sprintf("lines[%d].%s", i, f.n), f.t})
		}
	}
	for _, a := range amounts {
		if a.t.Value != nil && !decimal.MatchString(*a.t.Value) {
			return Reading{}, fmt.Errorf("%s: %q is not a decimal string such as 100.00", a.name, *a.t.Value)
		}
	}
	return r, nil
}

// fields are every Text of a reading by its name, lines included.
func (r Reading) fields() map[string]Text {
	out := map[string]Text{"supplier": r.Supplier, "supplier_uen": r.SupplierUEN, "gst_registration_no": r.GSTRegistrationNo, "supplier_address": r.SupplierAddress,
		"number": r.Number, "date": r.Date, "due_date": r.DueDate, "currency": r.Currency, "gst_total": r.GSTTotal, "total": r.Total, "gst_in_sgd": r.GSTInSGD,
		"tax_invoice_label": {Confidence: r.TaxInvoiceLabel.Confidence}, "customer_named": {Confidence: r.CustomerNamed.Confidence}}
	for i, l := range r.Lines {
		p := fmt.Sprintf("lines[%d].", i)
		out[p+"description"], out[p+"net"], out[p+"gst_code"], out[p+"gst"], out[p+"amount"] = l.Description, l.Net, l.GSTCode, l.GST, l.Amount
	}
	return out
}

// Severities of a check: a warning wants the person's eye before the draft is made; info says what the document lacks.
const (
	Warning = "warning"
	Info    = "info"
)

// Check is one finding of the platform's own checks of a reading (design D5), in plain words with the field it is about.
type Check struct {
	Code     string `json:"code" doc:"The check's code." example:"CAP-TOTALS"`
	Field    string `json:"field,omitempty" doc:"The field it is about." example:"total"`
	Severity string `json:"severity" enum:"warning,info" doc:"warning wants the person's eye before the draft is made; info says what the document lacks." example:"warning"`
	Message  string `json:"message" doc:"In plain words." example:"The lines come to 109.00, but the document's total is 190.00; check each against the document"`
}

// The codes of the checks.
const (
	CheckTotals     = "CAP-TOTALS"     // the lines and their GST do not add up to the total
	CheckDate       = "CAP-DATE"       // a date that is not a date, or not plausible
	CheckCurrency   = "CAP-CURRENCY"   // a currency Fyniq does not know
	CheckGSTNumber  = "CAP-GST-NUMBER" // a GST registration number that does not look like one
	CheckParticular = "CAP-PARTICULAR" // an IRAS tax-invoice particular the document lacks
	CheckMissing    = "CAP-MISSING"    // a field a bill needs that the document does not show
	CheckGSTForeign = "CAP-GST-SGD"    // a foreign-currency document that states GST without its SGD amount
)

// gstNumber is the form of a Singapore GST registration number, with its spaces and hyphens taken out: M and a digit or letter, seven digits and
// a check character (M2-8503432-K), or the UEN of a registered business, which is also its GST number [verify against IRAS before relying on the form].
var gstNumber = regexp.MustCompile(`^(M[0-9A-Z][0-9]{7}[0-9A-Z]|[0-9]{8,9}[A-Z]|[TSR][0-9]{2}[A-Z]{2}[0-9]{4}[A-Z])$`)

// Result is a reading checked: its findings, and each field's confidence after the checks, which never exceeds what the model said.
type Result struct {
	Checks []Check            `json:"checks"`
	Fields map[string]float64 `json:"confidence"`
	// Low are the fields below LowConfidence, for the draft's highlights.
	Low []string `json:"low"`
}

// Verify checks a reading as the platform would, as at today: the lines and the GST add up to the total, the dates and the currency are
// plausible, the GST registration number looks like one, and the IRAS tax-invoice particulars (FR-AR-05) the document lacks are named. A check
// lowers the confidence of the fields it is about to at most 0.5, so the draft highlights them; it never raises any.
func Verify(r Reading, today time.Time) Result {
	var res Result
	conf := map[string]float64{}
	shown := map[string]bool{} // a field the document shows: one it does not show is not a doubt to highlight
	for name, f := range r.fields() {
		conf[name], shown[name] = f.Confidence, f.Value != nil
	}
	shown["tax_invoice_label"], shown["customer_named"] = r.TaxInvoiceLabel.Value != nil, r.CustomerNamed.Value != nil
	flag := func(code, field, severity, msg string, lower ...string) {
		res.Checks = append(res.Checks, Check{Code: code, Field: field, Severity: severity, Message: msg})
		for _, f := range lower {
			conf[f], shown[f] = min(conf[f], 0.5), true
		}
	}
	val := func(t Text) string {
		if t.Value == nil {
			return ""
		}
		return strings.TrimSpace(*t.Value)
	}
	// The fields a bill needs.
	if r.Kind == "bill" || r.Kind == "receipt" {
		for _, f := range []struct {
			name string
			t    Text
		}{{"supplier", r.Supplier}, {"date", r.Date}, {"total", r.Total}, {"currency", r.Currency}} {
			if val(f.t) == "" {
				flag(CheckMissing, f.name, Warning, "The document does not show its "+strings.ReplaceAll(f.name, "_", " ")+"; type it in", f.name)
			}
		}
	}
	// The dates.
	for _, f := range []struct {
		name string
		t    Text
	}{{"date", r.Date}, {"due_date", r.DueDate}} {
		if v := val(f.t); v != "" {
			d, err := time.Parse(time.DateOnly, v)
			switch {
			case err != nil:
				flag(CheckDate, f.name, Warning, fmt.Sprintf("%q is not a date; check it against the document", v), f.name)
			case d.Year() < 2000 || d.After(today.AddDate(0, 0, 400)):
				flag(CheckDate, f.name, Warning, fmt.Sprintf("%s is far from today; check it against the document", v), f.name)
			}
		}
	}
	// The currency, and the arithmetic in it.
	var cur money.Currency
	curOK := false
	if v := strings.ToUpper(val(r.Currency)); v != "" {
		c, err := money.Lookup(v)
		if err != nil {
			flag(CheckCurrency, "currency", Warning, fmt.Sprintf("%q is not a currency Fyniq knows", v), "currency")
		} else {
			cur, curOK = c, true
		}
	}
	if curOK {
		res.addTotals(r, cur, conf, flag)
		if cur.Code != "SGD" && val(r.GSTTotal) != "" && val(r.GSTInSGD) == "" {
			flag(CheckGSTForeign, "gst_in_sgd", Info, "The document is in "+cur.Code+" and states GST without its Singapore-dollar amount: the GST in SGD must be printed on it to claim the input tax (G4-029)", "gst_in_sgd")
		}
	}
	// The GST registration number's form.
	if v := val(r.GSTRegistrationNo); v != "" {
		if !gstNumber.MatchString(strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(v))) {
			flag(CheckGSTNumber, "gst_registration_no", Warning, fmt.Sprintf("%q does not look like a Singapore GST registration number; check it against the document [verify]", v), "gst_registration_no")
		}
	}
	// The IRAS tax-invoice particulars (FR-AR-05) a document claimed as a tax invoice must show: the label, the supplier's name, address and GST
	// registration number, the number, the date, each line's description and the GST total with the total payable.
	if r.Kind == "bill" || r.Kind == "receipt" {
		var lacks []string
		for _, p := range []struct {
			what string
			ok   bool
		}{{"the words Tax Invoice", r.TaxInvoiceLabel.Value != nil && *r.TaxInvoiceLabel.Value}, {"the supplier's GST registration number", val(r.GSTRegistrationNo) != ""},
			{"the supplier's address", val(r.SupplierAddress) != ""}, {"an invoice number", val(r.Number) != ""}, {"the GST amount", val(r.GSTTotal) != ""}} {
			if !p.ok {
				lacks = append(lacks, p.what)
			}
		}
		if len(lacks) > 0 {
			flag(CheckParticular, "", Info, "It lacks what a tax invoice shows: "+strings.Join(lacks, ", ")+". Its GST stays to confirm until you decide what the document is (FR-AP-20)")
		}
	}
	res.Fields = conf
	for name, c := range conf {
		if c < LowConfidence && shown[name] {
			res.Low = append(res.Low, name)
		}
	}
	slices.Sort(res.Low)
	if res.Checks == nil {
		res.Checks = []Check{}
	}
	return res
}

// addTotals checks that the lines' net amounts and the GST add up to the total, and the lines' own totals, when the document shows them.
func (res *Result) addTotals(r Reading, cur money.Currency, conf map[string]float64, flag func(code, field, severity, msg string, lower ...string)) {
	parse := func(t Text) (money.Money, bool) {
		if t.Value == nil {
			return money.Money{}, false
		}
		m, err := money.Parse(*t.Value, cur)
		return m, err == nil
	}
	total, ok := parse(r.Total)
	if !ok || len(r.Lines) == 0 {
		return
	}
	sum := money.Zero(cur)
	for i, l := range r.Lines {
		net, okNet := parse(l.Net)
		gst, _ := parse(l.GST)
		amt, okAmt := parse(l.Amount)
		switch {
		case okNet:
			sum, _ = sum.Add(net)
			if g, ok := parse(l.GST); ok {
				sum, _ = sum.Add(g)
			}
		case okAmt:
			sum, _ = sum.Add(amt)
		default:
			return // a line with no amount: the sum cannot be checked
		}
		if okNet && okAmt {
			if want, err := net.Add(gst); err == nil {
				if eq, _ := want.Cmp(amt); eq != 0 {
					flag(CheckTotals, fmt.Sprintf("lines[%d].amount", i), Warning, fmt.Sprintf("Line %d: the net plus its GST is %s, but the line's amount is %s", i+1, want, amt),
						fmt.Sprintf("lines[%d].net", i), fmt.Sprintf("lines[%d].gst", i), fmt.Sprintf("lines[%d].amount", i))
				}
			}
		}
	}
	if eq, err := sum.Cmp(total); err == nil && eq != 0 {
		flag(CheckTotals, "total", Warning, fmt.Sprintf("The lines come to %s, but the document's total is %s; check each against the document", sum, total), "total")
	}
}

// Name is a supplier's name as it is compared with the organisation's contacts: lower case, its punctuation and spacing taken out, and the
// words that only say what kind of company it is (Pte Ltd, Limited) left out.
func Name(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer(".", " ", ",", " ", "&", " and ", "-", " ", "'", "").Replace(s)
	var words []string
	for _, w := range strings.Fields(s) {
		switch w {
		case "pte", "ltd", "limited", "private", "llp", "inc", "co", "company", "corp", "corporation", "sdn", "bhd":
			continue
		}
		words = append(words, w)
	}
	return strings.Join(words, " ")
}
