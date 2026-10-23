//go:build local

package postgres_test

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AP-19 and G4-010 (rev 0.90, the owner's decisions of 23 Oct 2026): a bill of a supplier resident outside Singapore
// records the nature of the payment and posts with the tax agent's decision in force for it. Without one it shows ⚠,
// posts without withholding and its payment is held (BILLPAY-11). A US SaaS subscription decided "not applicable" is then
// paid normally. A non-resident consultant working on site is held until the agent decides to withhold 15%; the bill,
// edited, then posts the gross expense, the tax to 2330 and what the supplier is owed to 2010, and is paid net. A later
// decision never changes a posted bill: the replay finds the decision in force when each entry was recorded. Decisions
// never change or go (FY174).
// req: FR-AP-19
// req: RND-WHT
// req: G4-010
func TestWithholdingIsDecidedAndPostedWithTheBill(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ap-specialist"}})
	s := store(t, "fyniq_api")
	b := newPurchasesBooks(t, ctx, s, "Withholding Pte Ltd")
	bills, payments := journal.Bills{Store: s}, journal.BillPayments{Store: s}
	sgd := money.MustLookup("SGD")
	var saas, consultant, local storage.Contact
	var current storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if _, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "2330", Name: "Withholding tax payable", Type: storage.Liability,
			Role: storage.AccountRole(ledger.RoleWithholdingTax)}); err != nil {
			return err
		}
		if current, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true}}); err != nil {
			return err
		}
		if saas, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Linear Orbit Inc", Vendor: true, Country: "US"}); err != nil {
			return err
		}
		// Its address is in Singapore, but it is resident for tax in Australia: the tax residence decides.
		if consultant, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Ava Consulting", Vendor: true, Country: "SG", TaxResidence: "AU"}); err != nil {
			return err
		}
		local, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Local Pte Ltd", Vendor: true, Country: "SG"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	draft := func(c storage.Contact, ref, nature, net string) bill.Draft {
		return bill.Draft{SupplierID: c.ID, SupplierRef: ref, Date: porttest.Day(10, 14), Currency: sgd, Nature: nature,
			Lines: []bill.Line{{AccountID: b.Rent.ID, Net: sgdOf(t, net), GSTCode: "NR", GST: money.Zero(sgd)}}}
	}
	warned := func(c journal.Checked, code string) bool {
		return slices.ContainsFunc(c.Prepared.Warnings, func(w bill.Warning) bool { return w.Code == code })
	}
	pay := func(doc storage.Document, supplier storage.Contact, amount string) error {
		_, err := payments.Record(ctx, b.Org, billpayment.Draft{SupplierID: supplier.ID, Date: porttest.Day(10, 20), AccountID: current.ID, Currency: sgd,
			Bills: []billpayment.Part{{BillID: doc.ID, Amount: sgdOf(t, amount)}}, Unapplied: money.Zero(sgd)})
		return err
	}
	record := func(d storage.NewWithholdingDecision) (storage.WithholdingDecision, error) {
		var out storage.WithholdingDecision
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = bill.RecordWithholding(ctx, u, d, sgd)
			return err
		})
		return out, err
	}

	// A non-resident supplier's bill without a nature of payment does not post.
	noNature, err := bills.Create(ctx, b.Org, draft(saas, "LIN-1", "", "120.00"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bills.Post(ctx, b.Org, noNature.ID); !hasProblem(err, bill.CodeNature) {
		t.Fatalf("a bill without a nature: %v; want %s", err, bill.CodeNature)
	}
	// A resident supplier's bill needs none, and withholds nothing.
	if _, err := bills.Create(ctx, b.Org, draft(local, "L-1", "", "50.00")); err != nil {
		t.Fatal(err)
	}

	// G4-010, the SaaS: ⚠ and posted without withholding; held until the decision "not applicable", then paid.
	saasBill, err := bills.Create(ctx, b.Org, draft(saas, "LIN-2", bill.NatureSoftwareUse, "120.00"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := bills.Check(ctx, b.Org, saasBill.ID, nil)
	if err != nil || !warned(c, bill.WarnWHTUndecided) || c.Prepared.Withholding.Residence != "US" {
		t.Fatalf("the SaaS bill's check: %+v %v", c.Prepared, err)
	}
	if e, err := bills.Post(ctx, b.Org, saasBill.ID); err != nil || legsOf(t, s, b.Org, e) != "6100 120 SGD = 120; 2010 -120 SGD = -120" {
		t.Fatalf("the SaaS bill posted: %s %v", legsOf(t, s, b.Org, e), err)
	}
	if err := pay(saasBill, saas, "120.00"); !hasProblem(err, billpayment.CodeHeld) {
		t.Fatalf("paying the undecided SaaS bill: %v; want %s", err, billpayment.CodeHeld)
	}
	if _, err := record(storage.NewWithholdingDecision{SupplierID: saas.ID, Nature: bill.NatureSoftwareUse, Outcome: bill.OutcomeNotApplicable,
		TaxAgent: "Grace Ho", Note: "Software bought for own use: not subject to withholding", BillID: &saasBill.ID}); err != nil {
		t.Fatal(err)
	}
	if err := pay(saasBill, saas, "120.00"); err != nil {
		t.Errorf("paying the SaaS bill once decided: %v", err)
	}

	// G4-010, the consultant on site: held until the tax agent decides, then the edit withholds 15% and it is paid net.
	work, err := bills.Create(ctx, b.Org, draft(consultant, "AVA-7", bill.NatureServicesIn, "5000.00"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bills.Post(ctx, b.Org, work.ID); err != nil {
		t.Fatal(err)
	}
	if err := pay(work, consultant, "5000.00"); !hasProblem(err, billpayment.CodeHeld) {
		t.Fatalf("paying the undecided consultant: %v", err)
	}
	if _, err := record(storage.NewWithholdingDecision{SupplierID: consultant.ID, Nature: bill.NatureServicesIn, Outcome: bill.OutcomeWithhold,
		RatePercent: "15", TaxAgent: "Grace Ho", BillID: &work.ID}); err != nil {
		t.Fatal(err)
	}
	if err := pay(work, consultant, "5000.00"); !hasProblem(err, billpayment.CodeHeld) {
		t.Fatalf("paying the consultant before the edit applies the decision: %v", err)
	}
	ed, err := bills.Edit(ctx, b.Org, work.ID, draft(consultant, "AVA-7", bill.NatureServicesIn, "5000.00"), "The tax agent decided to withhold 15%", "")
	if err != nil || legsOf(t, s, b.Org, ed.Entry) != "6100 5000 SGD = 5000; 2330 -750 SGD = -750; 2010 -4250 SGD = -4250" {
		t.Fatalf("the edit: %s %v", legsOf(t, s, b.Org, ed.Entry), err)
	}
	if err := pay(work, consultant, "4250.01"); !hasProblem(err, billpayment.CodeOverpaid) {
		t.Errorf("paying more than is owed after withholding: %v", err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		l, err := payables.Open(ctx, u, sgd, b.AP.ID, porttest.Day(10, 31))
		for _, sup := range l.Suppliers {
			for _, it := range sup.Items {
				if it.DocumentID == work.ID && it.Open.String() != "4250.00" {
					t.Errorf("the AP sub-ledger has %s open of the consultant's bill; want 4250.00", it.Open)
				}
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := pay(work, consultant, "4250.00"); err != nil {
		t.Errorf("paying the consultant net: %v", err)
	}

	// A later decision changes no posted bill: the replay finds the one in force when each entry was recorded.
	if _, err := record(storage.NewWithholdingDecision{SupplierID: consultant.ID, Nature: bill.NatureServicesIn, Outcome: bill.OutcomeTreaty,
		RatePercent: "10", TaxAgent: "Grace Ho", Note: "Certificate of residence for 2026 received"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err != nil {
			return err
		}
		if len(r.Mismatches) > 0 {
			t.Errorf("the replay: %+v", r.Mismatches)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Decisions Fyniq does not record.
	for name, d := range map[string]storage.NewWithholdingDecision{
		"a resident supplier":        {SupplierID: local.ID, Nature: bill.NatureServicesIn, Outcome: bill.OutcomeNotApplicable, TaxAgent: "Grace Ho"},
		"withholding with no rate":   {SupplierID: consultant.ID, Nature: bill.NatureServicesIn, Outcome: bill.OutcomeWithhold, TaxAgent: "Grace Ho"},
		"not applicable with a rate": {SupplierID: consultant.ID, Nature: bill.NatureServicesIn, Outcome: bill.OutcomeNotApplicable, RatePercent: "15", TaxAgent: "Grace Ho"},
		"no tax agent":               {SupplierID: consultant.ID, Nature: bill.NatureServicesIn, Outcome: bill.OutcomeNotApplicable, TaxAgent: "  "},
		"an unknown nature":          {SupplierID: consultant.ID, Nature: "consulting", Outcome: bill.OutcomeNotApplicable, TaxAgent: "Grace Ho"},
		"another supplier's bill":    {SupplierID: consultant.ID, Nature: bill.NatureServicesIn, Outcome: bill.OutcomeNotApplicable, TaxAgent: "Grace Ho", BillID: &saasBill.ID},
		"a rate above 100":           {SupplierID: consultant.ID, Nature: bill.NatureServicesIn, Outcome: bill.OutcomeWithhold, RatePercent: "117", TaxAgent: "Grace Ho"},
	} {
		if _, err := record(d); !hasCode(err, bill.CodeWithholding) {
			t.Errorf("%s: %v; want %s", name, err, bill.CodeWithholding)
		}
	}

	// A decision never changes or goes, and the API's role writes nothing but new ones.
	for _, c := range []struct{ role, stmt, state string }{
		{"fyniq_api", "UPDATE fyniq.withholding_decision SET rate_percent = 5", "42501"},
		{"fyniq_api", "DELETE FROM fyniq.withholding_decision", "42501"},
		{"fyniq_migrator", "UPDATE fyniq.withholding_decision SET rate_percent = 5", "FY174"},
		{"fyniq_migrator", "DELETE FROM fyniq.withholding_decision", "FY174"},
		{"fyniq_api", "INSERT INTO fyniq.withholding_decision (supplier_id, nature, outcome, rate_percent, tax_agent, recorded_by) VALUES ('" +
			consultant.ID.String() + "', 'interest', 'not_applicable', 15, 'x', 'user:x')", "23514"},
		{"fyniq_worker", "INSERT INTO fyniq.withholding_decision (supplier_id, nature, outcome, tax_agent, recorded_by) VALUES ('" +
			consultant.ID.String() + "', 'interest', 'not_applicable', 'x', 'user:x')", "42501"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}

// hasProblem reports whether err is ledger problems holding code.
func hasProblem(err error, code string) bool {
	var ps ledger.Problems
	return asProblems(err, &ps) && ps.Has(code)
}
