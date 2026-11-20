//go:build local

package local_test

import (
	"testing"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

// The local IRAS stand-in answers every step of the contract the real adapter must answer (FR-TAX-03).
// req: FR-TAX-03
func TestTheLocalIRASFilerKeepsTheContract(t *testing.T) {
	f := local.NewIRASFiler()
	porttest.IRASFilingContract(t, f, f, "../../../contracts/connectors/iras-filing.contract.json")
}
