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

// Investor rights and controller entries keep their rules whoever writes them (FR-EQ-22, FR-EQ-25; FY295, FY296):
// their own keys only, a holder marked shareholder or investor; rights with 0 to 20 board seats and a list of consent
// matters; a controller with known grounds and no reason, entered in the company's register on or after becoming one and
// in ACRA's after it; a holder who is not one with a reason, and no grounds, register days or notices.
// req: FR-EQ-22
// req: FR-EQ-25
// asvs: V8.2.1
func TestAttackHolderRightsKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	d := newDirectorBooks(t, "Attack holder rights Pte Ltd")
	api := store(t, "fyniq_api")
	create := func(kind storage.DocumentType, payload string) error {
		return api.Do(ctx, d.org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Documents().Create(ctx, storage.NewDocument{Type: kind, Payload: []byte(payload)})
			return err
		})
	}
	rights := func(holder uuid.UUID, extra string) string {
		return fmt.Sprintf(`{"date":"2026-11-07","holder_id":%q,"from":"2026-06-18","board_seats":1,"observer":false,"board_majority":true,`+
			`"pro_rata":true,"consents":["Issuing new shares"],"agreements":"Side letter"%s}`, holder, extra)
	}
	for name, payload := range map[string]string{
		"a key of its own":         rights(d.director.ID, `,"controls":true`),
		"a customer as its holder": rights(d.customer.ID, ``),
		"21 board seats":           strings.Replace(rights(d.director.ID, ``), `"board_seats":1`, `"board_seats":21`, 1),
		"half a board seat":        strings.Replace(rights(d.director.ID, ``), `"board_seats":1`, `"board_seats":1.5`, 1),
		"consents that are text":   strings.Replace(rights(d.director.ID, ``), `["Issuing new shares"]`, `"Issuing new shares"`, 1),
		"an observer that is text": strings.Replace(rights(d.director.ID, ``), `"observer":false`, `"observer":"no"`, 1),
	} {
		if err := create(storage.DocumentInvestorRights, payload); storage.CodeOf(err) != storage.CodeInvestorRights {
			t.Errorf("investor rights with %s: %v", name, err)
		}
	}
	if err := create(storage.DocumentInvestorRights, rights(d.director.ID, ``)); err != nil {
		t.Errorf("investor rights as their rules allow: %v", err)
	}

	entry := func(holder uuid.UUID, body string) string {
		return fmt.Sprintf(`{"date":"2026-11-07","holder_id":%q,%s}`, holder, body)
	}
	controller := `"controller":true,"grounds":["shares"],"since":"2025-01-02","company_register_on":"2025-01-05","notices":[]`
	notOne := `"controller":false,"grounds":[],"reason":"Their consent matters protect the investment","since":"2026-10-15","notices":[]`
	for name, payload := range map[string]string{
		"a key of its own":                     entry(d.director.ID, controller+`,"percent":"30"`),
		"a customer as its holder":             entry(d.customer.ID, controller),
		"no grounds":                           entry(d.director.ID, strings.Replace(controller, `["shares"]`, `[]`, 1)),
		"an unknown ground":                    entry(d.director.ID, strings.Replace(controller, `["shares"]`, `["friendship"]`, 1)),
		"a controller with a reason":           entry(d.director.ID, controller+`,"reason":"none"`),
		"no day":                               entry(d.director.ID, strings.Replace(controller, `"since":"2025-01-02",`, ``, 1)),
		"the company's register before":        entry(d.director.ID, strings.Replace(controller, `"2025-01-05"`, `"2024-12-31"`, 1)),
		"the central register before":          entry(d.director.ID, controller+`,"central_register_on":"2025-01-04"`),
		"the central register with no company": entry(d.director.ID, strings.Replace(controller, `,"company_register_on":"2025-01-05"`, `,"central_register_on":"2025-01-07"`, 1)),
		"not one, with no reason":              entry(d.director.ID, strings.Replace(notOne, `"reason":"Their consent matters protect the investment",`, ``, 1)),
		"not one, with grounds":                entry(d.director.ID, strings.Replace(notOne, `"grounds":[]`, `"grounds":["board"]`, 1)),
		"not one, with a register day":         entry(d.director.ID, notOne+`,"company_register_on":"2026-10-16"`),
		"not one, with a notice":               entry(d.director.ID, strings.Replace(notOne, `"notices":[]`, `"notices":[{"sent_on":"2026-10-16"}]`, 1)),
	} {
		if err := create(storage.DocumentControllerEntry, payload); storage.CodeOf(err) != storage.CodeControllerEntry {
			t.Errorf("a controller entry with %s: %v", name, err)
		}
	}
	for name, payload := range map[string]string{"a controller": entry(d.director.ID, controller+`,"central_register_on":"2025-01-07"`),
		"not one": entry(d.director.ID, notOne)} {
		if err := create(storage.DocumentControllerEntry, payload); err != nil {
			t.Errorf("%s as its rules allow: %v", name, err)
		}
	}
}
