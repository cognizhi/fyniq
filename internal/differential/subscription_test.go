package differential

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"

	schemas "github.com/cognizhi/fyniq/test/differential"

	"github.com/cognizhi/fyniq/internal/domain/subscription"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

type subscriptionCase struct {
	Name      string `json:"name"`
	Contracts []struct {
		ID         string `json:"id"`
		Currency   string `json:"currency"`
		StartsOn   string `json:"starts_on"`
		EndsOn     string `json:"ends_on"`
		MRR        string `json:"mrr"`
		Amendments []struct {
			Kind        string `json:"kind"`
			EffectiveOn string `json:"effective_on"`
			MRR         string `json:"mrr"`
		} `json:"amendments"`
	} `json:"contracts"`
	Checks []struct {
		On   string `json:"on"`
		Want []struct {
			Currency string `json:"currency"`
			MRR      string `json:"mrr"`
			Active   int    `json:"active"`
		} `json:"want"`
	} `json:"checks"`
}

// The hand-worked subscription cases (FR-AR-15, FR-KPI-02) conform to their schema and the engine reproduces each check: the contracted MRR on a day, by currency, with how many
// contracts are in force. The oracle's own module reproduces the same file (oracle/tests/test_subscription.py), which is how the two implementations are held to one rule (QA-ENG-02).
// req: QA-ENG-02, FR-AR-15, FR-KPI-02
func TestTheSubscriptionCasesConformAndTheEngineReproducesEachCheck(t *testing.T) {
	schema := compile("subscription.schema.json", schemas.SubscriptionSchema)
	raw, err := os.ReadFile("../../test/differential/subscriptions/subscriptions.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range doc.([]any) {
		if err := schema.Validate(d); err != nil {
			t.Errorf("case %d: %v", i, err)
		}
	}
	var list []subscriptionCase
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	day := func(s string) time.Time { v, _ := time.Parse(time.DateOnly, s); return v }
	checks := 0
	for _, c := range list {
		var contracts []storage.SubscriptionContract
		by := map[uuid.UUID][]storage.SubscriptionAmendment{}
		for _, k := range c.Contracts {
			id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(c.Name+k.ID))
			sc := storage.SubscriptionContract{ID: id, Currency: k.Currency, StartsOn: day(k.StartsOn), MRR: storage.Decimal(k.MRR)}
			if k.EndsOn != "" {
				e := day(k.EndsOn)
				sc.EndsOn = &e
			}
			contracts = append(contracts, sc)
			for _, a := range k.Amendments {
				by[id] = append(by[id], storage.SubscriptionAmendment{ContractID: id, Kind: a.Kind, EffectiveOn: day(a.EffectiveOn), MRR: storage.Decimal(a.MRR)})
			}
		}
		for _, ck := range c.Checks {
			checks++
			got, err := subscription.Totals(contracts, by, day(ck.On))
			if err != nil || len(got) != len(ck.Want) {
				t.Errorf("%s on %s: %+v, %v, want %d currencies", c.Name, ck.On, got, err, len(ck.Want))
				continue
			}
			for i, w := range ck.Want {
				if got[i].Currency != w.Currency || got[i].MRR.String() != w.MRR || got[i].Active != w.Active {
					t.Errorf("%s on %s: %s %s from %d, want %s %s from %d", c.Name, ck.On, got[i].Currency, got[i].MRR, got[i].Active, w.Currency, w.MRR, w.Active)
				}
			}
		}
	}
	if checks < 20 {
		t.Errorf("only %d checks ran", checks)
	}
}
