package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/fieldseal"
)

// sealedTexts are the ciphertexts of sealed values in a JSON body.
var sealedTexts = regexp.MustCompile(`"` + SealedText + `":"[^"]*"`)

// outsideSealed is body with its ciphertexts left out: random base64 can spell a short value by chance, such as "Ada",
// which says nothing about the value being sealed.
func outsideSealed(body []byte) string {
	return sealedTexts.ReplaceAllString(string(body), `"`+SealedText+`":""`)
}

// A person's personal values in an event about them are sealed under their key (§11.8, rev 0.244; p1-pdpa-retention
// D8): every name, email address, phone number and address, at any depth, before and after; a proof, an empty value, a
// value sealed already and every other field stay as they are; each opens with its key and its own event and field,
// and nowhere else; masking leaves the sealed text alone; and the body keeps none of the values.
// req: AUD-02
// req: SEC-11
// req: data/backup-and-recovery Personal data in immutable records is crypto-shredded at the end of retention
// asvs: V11.3.2
func TestPersonalValuesAreSealed(t *testing.T) {
	org, event := uuid.New(), uuid.New()
	key, dek := uuid.New(), bytes.Repeat([]byte{3}, 32)
	seal := func(field string, plaintext []byte) (map[string]any, error) {
		ct, err := fieldseal.SealWith(dek, plaintext, SealAAD(org, event, field))
		if err != nil {
			return nil, err
		}
		return Sealed(key, ct), nil
	}
	proof := map[string]any{"fingerprint": FingerprintPrefix + "abc", "hint": "**89"}
	in := map[string]Change{
		"legal_name":       {nil, "Ada Tan"},
		"display_name":     {"Ada", "Ada Tan"},
		"billing_email":    {"", "ada@example.com"},
		"phone":            {nil, "+65 9123 4567"},
		"mailing_address":  {"1 Old Road", "2 New Road"},
		"payee":            {nil, map[string]any{"name": "Ada Tan", "bank_account_number": proof, "kind": "person"}},
		"emails":           {nil, []any{"a@example.com", "b@example.com"}},
		"user_id":          {nil, "01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"},
		"is_director":      {false, true},
		"bank_account_ref": {nil, proof},
	}
	out, err := SealPersonal(in, seal)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(out)
	for _, clear := range []string{"Ada", "ada@example.com", "9123", "Old Road", "New Road", "a@example.com"} {
		if strings.Contains(outsideSealed(body), clear) {
			t.Errorf("the sealed changes hold %q: %s", clear, body)
		}
	}
	open := func(field string, v any) string {
		t.Helper()
		k, ct, ok := SealedOf(v)
		if !ok || k != key {
			t.Fatalf("%s is not sealed: %v", field, v)
		}
		p, err := fieldseal.OpenWith(dek, ct, SealAAD(org, event, field))
		if err != nil {
			t.Fatalf("%s does not open: %v", field, err)
		}
		return string(p)
	}
	if open("legal_name", out["legal_name"][1]) != "Ada Tan" || open("display_name", out["display_name"][0]) != "Ada" ||
		open("payee.name", out["payee"][1].(map[string]any)["name"]) != "Ada Tan" || open("emails[1]", out["emails"][1].([]any)[1]) != "b@example.com" {
		t.Errorf("the values do not open as written: %s", body)
	}
	if out["billing_email"][0] != "" || out["user_id"][1] != in["user_id"][1] || out["is_director"][1] != true || out["legal_name"][0] != nil {
		t.Errorf("an empty, absent or other value changed: %s", body)
	}
	if p := out["payee"][1].(map[string]any); p["kind"] != "person" || p["bank_account_number"].(map[string]any)["fingerprint"] != proof["fingerprint"] {
		t.Errorf("the payee's other values changed: %v", p)
	}
	_, ct, _ := SealedOf(out["phone"][1])
	if _, err := fieldseal.OpenWith(dek, ct, SealAAD(org, event, "legal_name")); err == nil {
		t.Error("a value opened as another field's")
	}
	if _, err := fieldseal.OpenWith(dek, ct, SealAAD(org, uuid.New(), "phone")); err == nil {
		t.Error("a value opened in another event")
	}
	again, err := SealPersonal(out, func(string, []byte) (map[string]any, error) { t.Fatal("sealed twice"); return nil, nil })
	if err != nil || again["phone"][1].(map[string]any)[SealedText] != out["phone"][1].(map[string]any)[SealedText] {
		t.Errorf("a sealed value was sealed again: %v", err)
	}

	// Masking keeps the sealed text as written, even text an NRIC pattern would match.
	tricky := Sealed(key, []byte("xxS1234567Dxx"))
	tricky[SealedText] = "AAAA+S1234567D+AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if m := MaskValue("phone", tricky); m.(map[string]any)[SealedText] != tricky[SealedText] {
		t.Errorf("masking changed the sealed text: %v", m)
	}
	e, err := New(org, time.Date(2026, 10, 15, 1, 2, 3, 0, time.UTC), Actor{"user", "u1"}, "contact.updated", Object{Type: "contact", ID: org}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e.Changes = out
	canon, err := e.Canonical()
	if err != nil || !strings.Contains(string(canon), `"schema":"fyniq-audit-event@3"`) || strings.Contains(outsideSealed(canon), "Ada") {
		t.Errorf("the body: %s %v", canon, err)
	}
	if _, err := SealPersonal(in, func(string, []byte) (map[string]any, error) { return nil, context.DeadlineExceeded }); err == nil {
		t.Error("a seal that fails must fail the event, never keep the value")
	}
}

// Which person an event is about (D8): a member by their user, an invitee and a person who asked to join before they
// are members, and a contact (a person only when an individual or a director, which the adapter checks); nothing else.
// req: data/backup-and-recovery Personal data in immutable records is crypto-shredded at the end of retention
func TestTheSubjectOfAnEvent(t *testing.T) {
	id, user := uuid.New(), uuid.New()
	for _, c := range []struct {
		objectType string
		changes    map[string]Change
		want       string
	}{
		{"super_admin", nil, "member:" + id.String()},
		{"app_user", nil, "member:" + id.String()},
		{"user_membership", map[string]Change{"user_id": {nil, user}}, "member:" + user.String()},
		{"user_membership", map[string]Change{"user_id": {user.String(), nil}}, "member:" + user.String()},
		{"member_invitation", nil, "invitation:" + id.String()},
		{"join_request", nil, "join_request:" + id.String()},
		{"contact", nil, "contact:" + id.String()},
		{"user_membership", nil, ""},
		{"document", nil, ""},
		{"email_message", nil, ""},
	} {
		got, ok := Subject(c.objectType, id, c.changes)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s %v: %q %v, want %q", c.objectType, c.changes, got, ok, c.want)
		}
	}
	for key, want := range map[string]bool{"legal_name": true, "display_name": true, "billing_email": true, "remittance_email": true, "phone": true,
		"mailing_address": true, "name": true, "mobile": true, "user_id": false, "is_director": false, "status": false, "roles": false} {
		if Personal(key) != want {
			t.Errorf("Personal(%s) = %v", key, !want)
		}
	}
}

// A value is sealed for the financial year that holds its day (D8): with an April-to-March year, 2 Apr 2026 belongs
// to the year ending 31 Mar 2027 and 31 Mar 2026 to the one before; a day before the books start to the first year;
// and while the years are not set, the calendar year.
// req: data/backup-and-recovery Personal data in immutable records is crypto-shredded at the end of retention
func TestTheCohortIsTheFinancialYear(t *testing.T) {
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }
	cal := period.Calendar{BooksStart: d(2025, 4, 1), FirstYearEnd: d(2026, 3, 31)}
	for _, c := range []struct {
		day, want time.Time
		set       bool
	}{
		{d(2026, 4, 2), d(2027, 3, 31), true},
		{d(2026, 3, 31), d(2026, 3, 31), true},
		{d(2024, 12, 1), d(2026, 3, 31), true},
		{time.Date(2026, 4, 1, 23, 0, 0, 0, time.UTC), d(2027, 3, 31), true},
		{d(2026, 4, 2), d(2026, 12, 31), false},
	} {
		if got := Cohort(cal, c.set, c.day); !got.Equal(c.want) {
			t.Errorf("Cohort(%s, %v) = %s, want %s", c.day.Format(time.DateOnly), c.set, got.Format(time.DateOnly), c.want.Format(time.DateOnly))
		}
	}
}
