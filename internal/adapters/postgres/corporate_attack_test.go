//go:build local

package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The corporate profile's records keep their rules whoever writes them (FR-CS-01, FR-CS-06; FY352, FY353): a detail its
// own keys, a day, a name, an office, a five-digit SSIC code and its lodgement on or after it; an officer their own keys,
// a known role, a natural person unless an auditor, an identifier as its last 4 characters or a firm's UEN, the days in
// order, a nominee only as a director and naming whom they act for, a member of the organisation, a later version naming
// an officer's first; and neither an NRIC or FIN in full.
// req: FR-CS-01
// req: FR-CS-06
// asvs: V8.2.1, V8.3.1
func TestAttackCorporateRecordsKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack corporate profile Pte Ltd")
	api := store(t, "fyniq_api")
	create := func(kind storage.DocumentType, payload string) (uuid.UUID, error) {
		var id uuid.UUID
		err := api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: kind, Payload: []byte(payload)})
			id = doc.ID
			return err
		})
		return id, err
	}
	detail := `{"date":"2026-11-16","from":"2026-03-02","name":"Cognizhi Pte. Ltd.","registered_office":"1 North Bridge Road","ssic":"62011"%s}`
	for name, payload := range map[string]string{
		"a key of its own":          fmt.Sprintf(detail, `,"uen":"202612345K"`),
		"no day":                    strings.Replace(fmt.Sprintf(detail, ``), `"from":"2026-03-02",`, ``, 1),
		"a blank name":              strings.Replace(fmt.Sprintf(detail, ``), `"Cognizhi Pte. Ltd."`, `" "`, 1),
		"a four-digit SSIC code":    strings.Replace(fmt.Sprintf(detail, ``), `"62011"`, `"6201"`, 1),
		"lodged before it":          fmt.Sprintf(detail, `,"lodged_on":"2026-03-01"`),
		"an NRIC in full in a note": fmt.Sprintf(detail, `,"note":"Director S1234567D"`),
		"an office of 501 letters":  strings.Replace(fmt.Sprintf(detail, ``), `"1 North Bridge Road"`, `"`+strings.Repeat("x", 501)+`"`, 1),
	} {
		if _, err := create(storage.DocumentCorporateDetail, payload); storage.CodeOf(err) != storage.CodeCorporateDetail {
			t.Errorf("a corporate detail with %s: %v", name, err)
		}
	}
	for name, payload := range map[string]string{
		"its lodgement":                     fmt.Sprintf(detail, `,"lodged_on":"2026-03-09"`),
		"an NRIC's shape inside a word":     fmt.Sprintf(detail, `,"note":"Ref ABS1234567DE"`),
		"an NRIC's shape in an ID's digits": fmt.Sprintf(detail, `,"note":"0193a3c2-6d0e-7a1b-9c4d-8f1234567abc"`),
	} {
		if _, err := create(storage.DocumentCorporateDetail, payload); err != nil {
			t.Errorf("a corporate detail with %s, as its rules allow: %v", name, err)
		}
	}

	officer := func(body string) string {
		return `{"date":"2026-11-16","role":"director","name":"Tan Mei Lin","kind":"individual","id_type":"nric","id_last4":"567A",` +
			`"appointed_on":"2026-03-02","nominee":false` + body + `}`
	}
	auditor := `{"date":"2026-11-16","role":"auditor","name":"Ledger & Co LLP","kind":"entity","id_type":"uen","registration_number":"T08LL1234A",` +
		`"appointed_on":"2026-05-01","nominee":false}`
	first, err := create(storage.DocumentCorporateOfficer, officer(fmt.Sprintf(`,"member_user_id":%q`, d.member)))
	if err != nil {
		t.Fatalf("an officer as their rules allow: %v", err)
	}
	for name, payload := range map[string]string{
		"a key of their own":            officer(`,"nric":"S1234567D"`),
		"an unknown role":               strings.Replace(officer(``), `"director"`, `"chairman"`, 1),
		"a company as a director":       strings.Replace(strings.Replace(officer(``), `"individual"`, `"entity"`, 1), `"id_type":"nric","id_last4":"567A",`, ``, 1),
		"an identifier of 5 characters": strings.Replace(officer(``), `"567A"`, `"4567A"`, 1),
		"an identifier with no type":    strings.Replace(officer(``), `"id_type":"nric",`, ``, 1),
		"an NRIC in full":               strings.Replace(officer(``), `"Tan Mei Lin"`, `"Tan Mei Lin S1234567D"`, 1),
		"no day appointed":              strings.Replace(officer(``), `"appointed_on":"2026-03-02",`, ``, 1),
		"ceased before appointed":       officer(`,"ceased_on":"2026-03-01"`),
		"lodged before appointed":       officer(`,"appointment_lodged_on":"2026-03-01"`),
		"a cessation lodged with none":  officer(`,"cessation_lodged_on":"2026-04-01"`),
		"a nominee for no one":          strings.Replace(officer(``), `"nominee":false`, `"nominee":true`, 1),
		"a nominee secretary":           strings.Replace(strings.Replace(officer(`,"nominator":"Lion Rock"`), `"nominee":false`, `"nominee":true`, 1), `"director"`, `"secretary"`, 1),
		"a nominator with no nominee":   officer(`,"nominator":"Lion Rock"`),
		"a nominee that is text":        strings.Replace(officer(``), `"nominee":false`, `"nominee":"no"`, 1),
		"someone else's member":         officer(fmt.Sprintf(`,"member_user_id":%q`, uuid.New())),
		"a later version of nothing":    officer(fmt.Sprintf(`,"officer_id":%q`, uuid.New())),
		"an audit firm with a member":   strings.Replace(auditor, `"nominee":false`, fmt.Sprintf(`"nominee":false,"member_user_id":%q`, d.member), 1),
		"an audit firm with no number":  strings.Replace(auditor, `"registration_number":"T08LL1234A",`, ``, 1),
	} {
		if _, err := create(storage.DocumentCorporateOfficer, payload); storage.CodeOf(err) != storage.CodeCorporateOfficer {
			t.Errorf("an officer with %s: %v", name, err)
		}
	}
	for name, payload := range map[string]string{
		"an audit firm": auditor,
		"a nominee":     strings.Replace(officer(`,"nominator":"Lion Rock Ventures Pte. Ltd."`), `"nominee":false`, `"nominee":true`, 1),
		"her cessation": officer(fmt.Sprintf(`,"officer_id":%q,"ceased_on":"2026-09-30","cessation_lodged_on":"2026-10-05"`, first)),
	} {
		if _, err := create(storage.DocumentCorporateOfficer, payload); err != nil {
			t.Errorf("%s as the rules allow: %v", name, err)
		}
	}
}

// The nominee registers and the statements approval keep their rules whoever writes them (s386AL, FR-CS-03; FY354,
// FY355): an entry its own keys, a director among the officers or a contact marked shareholder or investor, whom they
// act for and its days in order; an approval its own keys, approved after the year ends and sent on or after it.
// req: FR-CS-01
// req: FR-CS-03
// asvs: V8.2.1
func TestAttackCorporateRegistersKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack corporate registers Pte Ltd")
	api := store(t, "fyniq_api")
	create := func(kind storage.DocumentType, payload string) (uuid.UUID, error) {
		var id uuid.UUID
		err := api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: kind, Payload: []byte(payload)})
			id = doc.ID
			return err
		})
		return id, err
	}
	officer, err := create(storage.DocumentCorporateOfficer, `{"date":"2026-11-16","role":"director","name":"Lee Wei","kind":"individual",`+
		`"appointed_on":"2026-06-18","nominee":true,"nominator":"Lion Rock"}`)
	if err != nil {
		t.Fatal(err)
	}
	holder := func(body string) string {
		return fmt.Sprintf(`{"date":"2026-11-16","kind":"shareholder","holder_id":%q,"nominator":"Fund I","since":"2026-06-18"%s}`, d.director.ID, body)
	}
	for name, payload := range map[string]string{
		"a key of its own":               holder(`,"shares":"100"`),
		"an unknown kind":                strings.Replace(holder(``), `"shareholder"`, `"secretary"`, 1),
		"a customer as its holder":       strings.Replace(holder(``), d.director.ID.String(), d.customer.ID.String(), 1),
		"a shareholder with an officer":  holder(fmt.Sprintf(`,"officer_id":%q`, officer)),
		"a director with no officer":     strings.Replace(strings.Replace(holder(``), `"shareholder"`, `"director"`, 1), fmt.Sprintf(`,"holder_id":%q`, d.director.ID), ``, 1),
		"a director of another document": fmt.Sprintf(`{"date":"2026-11-16","kind":"director","officer_id":%q,"nominator":"X","since":"2026-06-18"}`, uuid.New()),
		"for no one":                     strings.Replace(holder(``), `"Fund I"`, `" "`, 1),
		"no day":                         strings.Replace(holder(``), `,"since":"2026-06-18"`, ``, 1),
		"informed before":                holder(`,"informed_on":"2026-06-17"`),
		"ACRA told with no register":     holder(`,"central_on":"2026-07-02"`),
		"ACRA told before the register":  holder(`,"register_on":"2026-06-30","central_on":"2026-06-29"`),
		"ceased before":                  holder(`,"ceased_on":"2026-06-01"`),
		"a later version of nothing":     holder(fmt.Sprintf(`,"entry_id":%q`, uuid.New())),
		"an NRIC in full":                strings.Replace(holder(``), `"Fund I"`, `"S1234567D"`, 1),
	} {
		if _, err := create(storage.DocumentNomineeEntry, payload); storage.CodeOf(err) != storage.CodeNomineeEntry {
			t.Errorf("a nominee entry with %s: %v", name, err)
		}
	}
	first, err := create(storage.DocumentNomineeEntry, holder(`,"register_on":"2026-06-30","central_on":"2026-07-02"`))
	if err != nil {
		t.Fatalf("a nominee shareholder as the rules allow: %v", err)
	}
	for name, payload := range map[string]string{
		"a nominee director": fmt.Sprintf(`{"date":"2026-11-16","kind":"director","officer_id":%q,"nominator":"Lion Rock","since":"2026-06-18"}`, officer),
		"its cessation":      holder(fmt.Sprintf(`,"entry_id":%q,"ceased_on":"2026-10-01"`, first)),
	} {
		if _, err := create(storage.DocumentNomineeEntry, payload); err != nil {
			t.Errorf("%s as the rules allow: %v", name, err)
		}
	}

	approval := `{"date":"2026-11-16","fy_end":"2025-12-31","approved_on":"2026-04-15"%s}`
	for name, payload := range map[string]string{
		"a key of its own":           fmt.Sprintf(approval, `,"auditor":"x"`),
		"approved on the year's end": strings.Replace(fmt.Sprintf(approval, ``), `"2026-04-15"`, `"2025-12-31"`, 1),
		"sent before approval":       fmt.Sprintf(approval, `,"sent_on":"2026-04-14"`),
		"no year's end":              strings.Replace(fmt.Sprintf(approval, ``), `"fy_end":"2025-12-31",`, ``, 1),
	} {
		if _, err := create(storage.DocumentStatementsApproval, payload); storage.CodeOf(err) != storage.CodeStatementsApproval {
			t.Errorf("a statements approval with %s: %v", name, err)
		}
	}
	if _, err := create(storage.DocumentStatementsApproval, fmt.Sprintf(approval, `,"sent_on":"2026-04-20"`)); err != nil {
		t.Errorf("a statements approval as the rules allow: %v", err)
	}
}

// An ACRA extract's officers and registered office keep their rules whoever writes them (FR-CS-01, design D3; FY297):
// a list of officers of their own keys, each a name, a known role and a day appointed on or before the profile's day; an
// office of at most 500 characters; no NRIC or FIN in full. An extract typed before, with neither, stays valid.
// req: FR-CS-01
// asvs: V8.2.1
func TestAttackAcraExtractOfficersKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack ACRA officers Pte Ltd")
	class := shareCapital(t, d)
	api := store(t, "fyniq_api")
	create := func(payload string) error {
		return api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentAcraExtract, Payload: []byte(payload)})
			return err
		})
	}
	extract := func(extra string) string {
		return fmt.Sprintf(`{"date":"2026-11-16","profile_on":"2026-10-01","holdings":[{"holder_id":%q,"class_account_id":%q,"shares":"700000"}]%s}`,
			d.director.ID, class, extra)
	}
	officer := `{"name":"Tan Mei Lin","role":"director","appointed_on":"2026-03-02"}`
	for name, payload := range map[string]string{
		"officers that are no list":   extract(`,"officers":{}`),
		"an officer's key of its own": extract(`,"officers":[` + strings.Replace(officer, `}`, `,"nric":"567A"}`, 1) + `]`),
		"an unknown role":             extract(`,"officers":[` + strings.Replace(officer, `"director"`, `"chairman"`, 1) + `]`),
		"no name":                     extract(`,"officers":[` + strings.Replace(officer, `"Tan Mei Lin"`, `" "`, 1) + `]`),
		"appointed after its day":     extract(`,"officers":[` + strings.Replace(officer, `"2026-03-02"`, `"2026-10-02"`, 1) + `]`),
		"an NRIC in full":             extract(`,"officers":[` + strings.Replace(officer, `"Tan Mei Lin"`, `"Tan Mei Lin S1234567D"`, 1) + `]`),
		"an office of 501 characters": extract(`,"registered_office":"` + strings.Repeat("x", 501) + `"`),
	} {
		if err := create(payload); storage.CodeOf(err) != storage.CodeAcraExtract {
			t.Errorf("an extract with %s: %v", name, err)
		}
	}
	for name, payload := range map[string]string{"neither": extract(``),
		"officers and an office": extract(`,"officers":[` + officer + `],"registered_office":"1 North Bridge Road"`)} {
		if err := create(payload); err != nil {
			t.Errorf("an extract with %s, as its rules allow: %v", name, err)
		}
	}
}

// A corporate document keeps its rules whoever writes it (FR-CS-05; FY356): its own keys, a known kind, a template named
// for its kind with a version, its fields and paper as objects, signed on or after it was drafted, a note of at most
// 1,000 characters, and no NRIC or FIN in full anywhere in it.
// req: FR-CS-05
// asvs: V8.2.1, V8.3.1
func TestAttackCorporateDocumentsKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack corporate documents Pte Ltd")
	api := store(t, "fyniq_api")
	create := func(payload string) error {
		return api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentCorporateDocument, Payload: []byte(payload)})
			return err
		})
	}
	doc := `{"date":"2027-01-05","kind":"directors.approve_statements","template":"directors.approve_statements@1","fields":{"fy_end":"2026-12-31"},` +
		`"paper":{"title":"Directors' resolution in writing","sections":[{"items":["That the statements be approved."]}]}%s}`
	if err := create(fmt.Sprintf(doc, `,"signed_on":"2027-01-10","note":"For the AGM"`)); err != nil {
		t.Fatalf("a corporate document as its rules allow: %v", err)
	}
	for name, payload := range map[string]string{
		"a key of its own":              fmt.Sprintf(doc, `,"signed_by":"Tan Mei Lin"`),
		"an unknown kind":               strings.Replace(fmt.Sprintf(doc, ``), `"kind":"directors.approve_statements"`, `"kind":"directors.sell_company"`, 1),
		"another kind's template":       strings.Replace(fmt.Sprintf(doc, ``), `"template":"directors.approve_statements@1"`, `"template":"agm.notice@1"`, 1),
		"a template with no version":    strings.Replace(fmt.Sprintf(doc, ``), `@1"`, `"`, 1),
		"a template whose dot is any":   strings.Replace(fmt.Sprintf(doc, ``), `"template":"directors.approve_statements@1"`, `"template":"directorsXapprove_statements@1"`, 1),
		"fields that are not an object": strings.Replace(fmt.Sprintf(doc, ``), `{"fy_end":"2026-12-31"}`, `["2026-12-31"]`, 1),
		"no paper":                      strings.Replace(fmt.Sprintf(doc, ``), `"paper":{"title":"Directors' resolution in writing","sections":[{"items":["That the statements be approved."]}]}`, `"paper":"text"`, 1),
		"signed before it was drafted":  fmt.Sprintf(doc, `,"signed_on":"2027-01-04"`),
		"a note of 1,001 letters":       fmt.Sprintf(doc, `,"note":"`+strings.Repeat("x", 1001)+`"`),
		"an NRIC in full in its paper":  strings.Replace(fmt.Sprintf(doc, ``), `That the statements be approved.`, `That S1234567D be appointed.`, 1),
		"an NRIC in full in a field":    strings.Replace(fmt.Sprintf(doc, ``), `{"fy_end":"2026-12-31"}`, `{"fy_end":"2026-12-31","name":"Tan T7654321A"}`, 1),
		"no day":                        strings.Replace(fmt.Sprintf(doc, ``), `"date":"2027-01-05",`, ``, 1),
	} {
		if err := create(payload); storage.CodeOf(err) != storage.CodeCorporateDocument {
			t.Errorf("a corporate document with %s: %v", name, err)
		}
	}
}
