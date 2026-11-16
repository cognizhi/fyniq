package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/ports"
)

// CloudBillingAPI is the Cloud Billing Catalog API.
const CloudBillingAPI = "https://cloudbilling.googleapis.com"

// AgentPlatformServices are the names Agent Platform's service may carry in the catalogue [live-verify: its display
// name since Vertex AI became Agent Platform].
var AgentPlatformServices = []string{"Agent Platform", "Vertex AI"}

// billingPages caps the pages of one listing: Agent Platform has a few thousand SKUs, at 5,000 a page.
const billingPages = 20

// BillingCatalog is Google's Cloud Billing Catalog for Agent Platform (PRD-001 AI-82 (b)), read with the service's own
// account (cloud-billing.readonly) through the platform's egress guard [live-verify]. Prices are read from their whole
// units and nanos, never as binary floats.
type BillingCatalog struct {
	Endpoint string
	Names    []string // the service's display names, AgentPlatformServices unless a test sets them
	r        rest
}

var _ ports.BillingCatalog = (*BillingCatalog)(nil)

// NewBillingCatalog returns the adapter for the catalogue at endpoint.
func NewBillingCatalog(endpoint string, tokens oauth2.TokenSource, client *http.Client) *BillingCatalog {
	return &BillingCatalog{Endpoint: endpoint, Names: AgentPlatformServices, r: rest{tokens: tokens, client: client}}
}

type billingService struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type billingSKU struct {
	SkuID          string   `json:"skuId"`
	Description    string   `json:"description"`
	ServiceRegions []string `json:"serviceRegions"`
	PricingInfo    []struct {
		PricingExpression struct {
			UsageUnit   string `json:"usageUnit"`
			TieredRates []struct {
				StartUsageAmount json.Number `json:"startUsageAmount"`
				UnitPrice        struct {
					CurrencyCode string      `json:"currencyCode"`
					Units        string      `json:"units"`
					Nanos        json.Number `json:"nanos"`
				} `json:"unitPrice"`
			} `json:"tieredRates"`
		} `json:"pricingExpression"`
	} `json:"pricingInfo"`
}

// service finds Agent Platform's service by its display name.
func (b *BillingCatalog) service(ctx context.Context) (string, error) {
	token := ""
	for range billingPages {
		var page struct {
			Services      []billingService `json:"services"`
			NextPageToken string           `json:"nextPageToken"`
		}
		q := url.Values{"pageSize": {"5000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		if err := b.r.do(ctx, http.MethodGet, b.Endpoint+"/v1/services?"+q.Encode(), "", nil, &page); err != nil {
			return "", fmt.Errorf("the billing catalogue's services: %w", err)
		}
		for _, s := range page.Services {
			if slices.Contains(b.Names, s.DisplayName) {
				return s.Name, nil
			}
		}
		if token = page.NextPageToken; token == "" {
			return "", fmt.Errorf("the billing catalogue lists no service named %s", strings.Join(b.Names, " or "))
		}
	}
	return "", fmt.Errorf("the billing catalogue's services run past %d pages", billingPages)
}

// SKUs implements ports.BillingCatalog: Agent Platform's SKUs, every page, in US dollars, with the current price of each.
func (b *BillingCatalog) SKUs(ctx context.Context) ([]ports.BillingSKU, error) {
	service, err := b.service(ctx)
	if err != nil {
		return nil, err
	}
	var out []ports.BillingSKU
	token := ""
	for range billingPages {
		var page struct {
			Skus          []billingSKU `json:"skus"`
			NextPageToken string       `json:"nextPageToken"`
		}
		q := url.Values{"currencyCode": {"USD"}, "pageSize": {"5000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		if err := b.r.do(ctx, http.MethodGet, b.Endpoint+"/v1/"+service+"/skus?"+q.Encode(), "", nil, &page); err != nil {
			return nil, fmt.Errorf("the billing catalogue's SKUs: %w", err)
		}
		for _, s := range page.Skus {
			sku, err := skuOf(s)
			if err != nil {
				return nil, fmt.Errorf("the billing catalogue's SKU %s: %w", s.SkuID, err)
			}
			out = append(out, sku)
		}
		if token = page.NextPageToken; token == "" {
			return out, nil
		}
	}
	return nil, fmt.Errorf("the billing catalogue's SKUs run past %d pages", billingPages)
}

// skuOf is a SKU with its current price (the catalogue's last pricing entry), each tier's price as exact decimal text.
func skuOf(s billingSKU) (ports.BillingSKU, error) {
	out := ports.BillingSKU{ID: s.SkuID, Description: s.Description, Regions: s.ServiceRegions}
	if len(s.PricingInfo) == 0 {
		return out, nil
	}
	e := s.PricingInfo[len(s.PricingInfo)-1].PricingExpression
	out.UsageUnit = e.UsageUnit
	for _, t := range e.TieredRates {
		price, err := moneyText(t.UnitPrice.Units, string(t.UnitPrice.Nanos))
		if err != nil {
			return out, err
		}
		start := string(t.StartUsageAmount)
		if start == "" {
			start = "0"
		}
		out.Tiers = append(out.Tiers, ports.BillingTier{StartUsage: start, Currency: t.UnitPrice.CurrencyCode, Price: price})
	}
	return out, nil
}

// moneyText is google.type.Money's whole units and nanos as exact decimal text.
func moneyText(units, nanos string) (string, error) {
	if units == "" {
		units = "0"
	}
	if nanos == "" {
		nanos = "0"
	}
	u, err := strconv.ParseInt(units, 10, 64)
	if err != nil {
		return "", fmt.Errorf("units %q are not a whole number", units)
	}
	n, err := strconv.ParseInt(nanos, 10, 64)
	if err != nil || n <= -1e9 || n >= 1e9 || (u > 0 && n < 0) || (u < 0 && n > 0) {
		return "", fmt.Errorf("nanos %q are not a fraction of units %q", nanos, units)
	}
	sign := ""
	if u < 0 || n < 0 {
		sign, u, n = "-", -u, -n
	}
	text := strconv.FormatInt(u, 10)
	if frac := strings.TrimRight(fmt.Sprintf("%09d", n), "0"); frac != "" {
		text += "." + frac
	}
	return sign + text, nil
}
