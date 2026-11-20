package ports

import (
	"reflect"
	"slices"
	"testing"
)

// The payment-provider port only reads: its methods are these four, and none writes (a charge, a refund, a payout, a
// retry, a cancellation). A new method must be added here on purpose, with the reason it still does not write.
// req: FR-INT-27
func TestThePaymentProviderPortHasNoMethodThatWrites(t *testing.T) {
	typ := reflect.TypeOf((*PaymentProvider)(nil)).Elem()
	var got []string
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	slices.Sort(got)
	if want := []string{"Balance", "List", "Name", "Object"}; !slices.Equal(got, want) {
		t.Fatalf("the port's methods are %v, want %v: a provider connector is read-only (FR-INT-27)", got, want)
	}
}

// The payment-link port is the connector's only write, and it is one method: asking for a payment request. A new method must be
// added here on purpose, with the reason it cannot move money (design D1).
// req: FR-INT-27
// req: FR-PSP-02
func TestThePaymentLinksPortHasOneMethod(t *testing.T) {
	typ := reflect.TypeOf((*PaymentLinks)(nil)).Elem()
	var got []string
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	if want := []string{"CreatePaymentRequest"}; !slices.Equal(got, want) {
		t.Fatalf("the port's methods are %v, want %v", got, want)
	}
}
