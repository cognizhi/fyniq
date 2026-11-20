package invoicenow_test

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/invoicenow"
)

func day(d int) time.Time { return time.Date(2027, 4, d, 0, 0, 0, 0, time.UTC) }

// The gate waits for every document of the period (G4-030 to G4-034): an acknowledged one passes, so does an excluded
// one with its reason; a queued, sent, failed or never-queued one blocks, listed in date order.
// req: FR-TAX-10
// req: FR-TAX-11
func TestTheGateListsWhatIsMissing(t *testing.T) {
	items := []invoicenow.Item{
		{DocumentID: "a", Number: "INV-1", Kind: "sale", Date: day(2)},
		{DocumentID: "b", Number: "INV-2", Kind: "sale", Date: day(3)},
		{DocumentID: "c", Number: "BILL-1", Kind: "purchase", Date: day(4)},
		{DocumentID: "d", Number: "BILL-2", Kind: "purchase", Date: day(1)},
		{DocumentID: "e", Number: "BILL-3", Kind: "purchase", Date: day(5)},
		{DocumentID: "f", Number: "CN-1", Kind: "credit_note", Date: day(6)},
	}
	subs := []invoicenow.Submission{
		{DocumentID: "a", Version: 1, Status: invoicenow.Acknowledged, Ack: "IRAS-1"},
		{DocumentID: "b", Version: 1, Status: invoicenow.Sent},
		{DocumentID: "c", Version: 1, Status: invoicenow.Failed},
		{DocumentID: "f", Version: 1, Status: invoicenow.Acknowledged, Ack: ""}, // an acknowledgement with no ID is no acknowledgement
	}
	excl := map[string]invoicenow.Reason{"e": invoicenow.UnregisteredSupplier}
	r := invoicenow.Evaluate(true, items, subs, excl, nil)
	if r.Ready || r.Acknowledged != 1 || r.Excluded != 1 || len(r.Missing) != 4 {
		t.Fatalf("the gate is %+v", r)
	}
	var got []string
	for _, m := range r.Missing {
		got = append(got, m.Number+":"+m.State)
	}
	want := []string{"BILL-2:none", "INV-2:sent", "BILL-1:failed", "CN-1:acknowledged"}
	if len(got) != len(want) {
		t.Fatalf("missing %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("missing %v, want %v", got, want)
		}
	}
}

// A correction is a new version of the same document: the gate reads the latest, so a corrected invoice whose second
// version is not yet acknowledged is missing although the first was (G4-032).
// req: FR-TAX-10
func TestTheLatestVersionDecides(t *testing.T) {
	items := []invoicenow.Item{{DocumentID: "a", Number: "INV-1", Kind: "sale", Date: day(2)}}
	subs := []invoicenow.Submission{
		{DocumentID: "a", Version: 1, Status: invoicenow.Acknowledged, Ack: "IRAS-1"},
		{DocumentID: "a", Version: 2, Status: invoicenow.Queued},
	}
	if r := invoicenow.Evaluate(true, items, subs, nil, nil); r.Ready || len(r.Missing) != 1 || r.Missing[0].State != "queued" {
		t.Fatalf("the gate is %+v", r)
	}
	subs[1] = invoicenow.Submission{DocumentID: "a", Version: 2, Status: invoicenow.Acknowledged, Ack: "IRAS-2"}
	if r := invoicenow.Evaluate(true, items, subs, nil, nil); !r.Ready || r.Acknowledged != 1 {
		t.Fatalf("the gate is %+v", r)
	}
}

// Only a contingency of two different people, with a reason and evidence, lets a return be filed with a gap (G4-033).
// req: FR-TAX-11
func TestOnlyAValidContingencyOpensTheGate(t *testing.T) {
	items := []invoicenow.Item{{DocumentID: "a", Number: "INV-1", Kind: "sale", Date: day(2)}}
	for name, tc := range map[string]struct {
		c  invoicenow.Contingency
		ok bool
	}{
		"valid":           {invoicenow.Contingency{RequestedBy: "member:1", ApprovedBy: "member:2", Reason: "the access point was down 28-30 April", Evidence: 1}, true},
		"the same person": {invoicenow.Contingency{RequestedBy: "member:1", ApprovedBy: "member:1", Reason: "outage", Evidence: 1}, false},
		"no evidence":     {invoicenow.Contingency{RequestedBy: "member:1", ApprovedBy: "member:2", Reason: "outage", Evidence: 0}, false},
		"no reason":       {invoicenow.Contingency{RequestedBy: "member:1", ApprovedBy: "member:2", Reason: " ", Evidence: 1}, false},
		"not approved":    {invoicenow.Contingency{RequestedBy: "member:1", Reason: "outage", Evidence: 1}, false},
	} {
		t.Run(name, func(t *testing.T) {
			c := tc.c
			r := invoicenow.Evaluate(true, items, nil, nil, &c)
			if r.Ready != tc.ok || (r.Contingency != nil) != tc.ok || len(r.Missing) != 1 {
				t.Fatalf("the gate is %+v", r)
			}
		})
	}
}

// Until the tax agent says the requirement applies (question 2) the gate is not assessed and blocks nothing.
// req: FR-TAX-10
func TestWithoutTheRequirementTheGateIsNotAssessed(t *testing.T) {
	r := invoicenow.Evaluate(false, []invoicenow.Item{{DocumentID: "a"}}, nil, nil, nil)
	if r.Applies || !r.Ready || len(r.Missing) != 0 {
		t.Fatalf("the gate is %+v", r)
	}
}

// A submission moves forward only: queued, sent, acknowledged, or to failed, and a failed one is queued again.
// req: FR-TAX-10
func TestASubmissionMovesOnlyAlongTheLegalPath(t *testing.T) {
	for _, tc := range []struct {
		from, to invoicenow.Status
		ok       bool
	}{
		{invoicenow.Queued, invoicenow.Sent, true}, {invoicenow.Sent, invoicenow.Acknowledged, true}, {invoicenow.Sent, invoicenow.Failed, true},
		{invoicenow.Failed, invoicenow.Queued, true}, {invoicenow.Queued, invoicenow.Acknowledged, false}, {invoicenow.Acknowledged, invoicenow.Queued, false},
		{invoicenow.Acknowledged, invoicenow.Failed, false}, {invoicenow.Failed, invoicenow.Acknowledged, false},
	} {
		if invoicenow.Legal(tc.from, tc.to) != tc.ok {
			t.Errorf("%s to %s: want %v", tc.from, tc.to, tc.ok)
		}
	}
}
