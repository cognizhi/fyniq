package differential

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schemas "github.com/cognizhi/fyniq/test/differential"
)

// Every golden employee-month conforms to payroll.schema.json, as the oracle's side checks them (QA-ENG-02; p1-nemura-payroll 1.5).
// req: QA-ENG-02
func TestTheGoldenPayrollMonthsConformToTheirSchema(t *testing.T) {
	schema := compile("payroll.schema.json", schemas.PayrollMonthSchema)
	files, _ := filepath.Glob("../../test/differential/payrolls/*.json")
	if len(files) == 0 {
		t.Fatal("no golden payroll months")
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		docs, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		list, ok := docs.([]any)
		if !ok {
			t.Fatalf("%s is not a list of months", f)
		}
		for i, d := range list {
			if err := schema.Validate(d); err != nil {
				t.Errorf("%s case %d: %v", f, i, err)
			}
		}
	}
}
