package ports

import "context"

// BillingCatalog is Google's Cloud Billing Catalog for Agent Platform (PRD-001 AI-82 (b)): the public prices of its SKUs,
// at most 12 hours old. Production: the gcp adapter, with the service's own account [live-verify: which SKUs price
// Claude and Gemini on Agent Platform, and in what unit]. Local: none, and the models are priced from the next source.
type BillingCatalog interface {
	// SKUs are Agent Platform's SKUs as the catalogue lists them now, in US dollars.
	SKUs(ctx context.Context) ([]BillingSKU, error)
}

// BillingSKU is one SKU of the catalogue: what it prices, where, in what unit, and its tiers.
type BillingSKU struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Regions     []string `json:"regions,omitempty"`
	// UsageUnit is the unit usage is priced in, as the catalogue names it, such as "count".
	UsageUnit string        `json:"usage_unit"`
	Tiers     []BillingTier `json:"tiers"`
}

// BillingTier is one tier of a SKU's price: the usage after which it applies and the price per usage unit, both as
// exact decimal text (the price from the catalogue's whole units and nanos).
type BillingTier struct {
	StartUsage string `json:"start_usage"`
	Currency   string `json:"currency"`
	Price      string `json:"price"`
}
