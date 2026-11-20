//go:build local

package local_test

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/domain/invoicenow"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

func goodDocument(t *testing.T) []byte {
	t.Helper()
	sgd := money.MustLookup("SGD")
	m := func(s string) money.Money {
		v, err := money.Parse(s, sgd)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	addr := invoicenow.Address{Line1: "1 Raffles Place", PostCode: "048616", Country: "SG"}
	b, err := invoicenow.Build(invoicenow.Document{Kind: invoicenow.Invoice, Number: "INV-1", UUID: "0a1b2c3d-1111-4222-8333-444455556666",
		Issued: time.Date(2027, 2, 3, 0, 0, 0, 0, time.UTC), Currency: sgd,
		Seller: invoicenow.Party{Name: "Seller", UEN: "202012345K", GST: "202012345K", Address: addr}, Buyer: invoicenow.Party{Name: "Buyer", UEN: "201912345A", Address: addr},
		Lines: []invoicenow.Line{{ID: "1", Name: "Work", Quantity: "1", Unit: "C62", NetPrice: "100.00", Net: m("100.00"), Category: "SR", Rate: "9"}},
		Tax:   []invoicenow.TaxSubtotal{{Category: "SR", Rate: "9", Taxable: m("100.00"), Tax: m("9.00")}},
		Net:   m("100.00"), TaxTotal: m("9.00"), Gross: m("109.00")})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The local access point answers every step of the contract the real adapter must answer (FR-AR-12, FR-AP-05).
// req: FR-AR-12
func TestTheLocalAccessPointKeepsTheContract(t *testing.T) {
	ap := local.NewAccessPoint()
	porttest.AccessPointContract(t, ap, ap, "../../../contracts/connectors/peppol-access-point.contract.json", goodDocument(t), []byte("<Invoice/>"))
}
