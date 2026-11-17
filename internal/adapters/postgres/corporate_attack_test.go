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
