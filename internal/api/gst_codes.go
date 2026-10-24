package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// GSTCode is one GST code for purchases, as the sg-gst pack defines it.
type GSTCode struct {
	Code        string `json:"code" doc:"The code, as a bill line takes it." example:"TX"`
	Description string `json:"description" doc:"What it is for, in the pack's words." example:"Standard-rated purchase whose GST can be claimed"`
	Claimable   bool   `json:"claimable" doc:"Whether input tax on it can be claimed (it has an input-tax box in the GST F5 return), once the organisation is GST-registered on the bill's date (FR-AP-20)." example:"true"`
}

// PurchaseGSTCodes are the GST codes a bill dated on a day is read with.
type PurchaseGSTCodes struct {
	Version string    `json:"version" doc:"The sg-gst version they come from (ENG-31)." example:"sg-gst@2026.1"`
	Rate    string    `json:"rate,omitempty" doc:"The standard rate of GST on the day, as a fraction." example:"0.09"`
	Codes   []GSTCode `json:"codes" nullable:"false" doc:"The codes for purchases, in the pack's order."`
}

type purchaseGSTCodesInput struct {
	On string `query:"on" format:"date" doc:"The bill's date; left out, the codes of the newest version." example:"2026-10-14"`
}

type purchaseGSTCodesOutput struct{ Body PurchaseGSTCodes }

// SalesGSTCode is one GST code for supplies, as the sg-gst pack defines it.
type SalesGSTCode struct {
	Code        string `json:"code" doc:"The code, as an invoice line takes it." example:"SR"`
	Description string `json:"description" doc:"What it is for, in the pack's words." example:"Standard-rated supply"`
	Rated       bool   `json:"rated" doc:"Whether the line bears GST at the standard rate once the organisation is GST-registered on the invoice's date." example:"true"`
}

// SalesGSTCodes are the GST codes an invoice dated on a day is read with.
type SalesGSTCodes struct {
	Version string         `json:"version" doc:"The sg-gst version they come from (ENG-31)." example:"sg-gst@2026.1"`
	Rate    string         `json:"rate,omitempty" doc:"The standard rate of GST on the day, as a fraction." example:"0.09"`
	Codes   []SalesGSTCode `json:"codes" nullable:"false" doc:"The codes for supplies, in the pack's order."`
}

type salesGSTCodesInput struct {
	On string `query:"on" format:"date" doc:"The invoice's date; left out, the codes of the newest version." example:"2026-10-14"`
}

type salesGSTCodesOutput struct{ Body SalesGSTCodes }

// gstDay reads the day a list of GST codes is for.
func gstDay(on string) (time.Time, error) {
	if on == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.DateOnly, on)
	if err != nil {
		return time.Time{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
	}
	return t, nil
}

func registerGSTCodes(a *API) {
	register(a, authz.GetPurchaseGSTCodes, huma.Operation{
		OperationID: "getPurchaseGSTCodes", Method: http.MethodGet, Path: "/v1/purchases/gst-codes", Tags: []string{"Purchases"},
		Summary: "Get the GST codes for purchases",
		Description: "The GST codes a bill dated on the day given is read with (FR-AP-02, ENG-31): the organisation's active sg-gst " +
			"version in force then, else the one this release carries; its standard rate on the day; and each code for purchases " +
			"with whether its input tax can be claimed. Read-only; no side effects.",
	}, []string{CodeInvalid}, func(ctx context.Context, in *purchaseGSTCodesInput) (*purchaseGSTCodesOutput, error) {
		var on time.Time
		if in.On != "" {
			t, err := time.Parse(time.DateOnly, in.On)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
			}
			on = t
		}
		p, _ := authn.From(ctx)
		out := purchaseGSTCodesOutput{Body: PurchaseGSTCodes{Codes: []GSTCode{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := bill.PurchaseCodes(ctx, u, on)
			if err != nil {
				return err
			}
			out.Body.Version, out.Body.Rate = c.Version, c.Rate
			for _, x := range c.Codes {
				out.Body.Codes = append(out.Body.Codes, GSTCode{Code: x.Code, Description: x.Description, Claimable: x.GSTBox > 0})
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetSalesGSTCodes, huma.Operation{
		OperationID: "getSalesGSTCodes", Method: http.MethodGet, Path: "/v1/sales/gst-codes", Tags: []string{"Sales"},
		Summary: "Get the GST codes for sales",
		Description: "The GST codes an invoice dated on the day given is read with (FR-AR-03, ENG-31): the organisation's active sg-gst " +
			"version in force then, else the one this release carries; its standard rate on the day; and each code for supplies, " +
			"with whether it bears GST at the standard rate. Read-only; no side effects.",
	}, []string{CodeInvalid}, func(ctx context.Context, in *salesGSTCodesInput) (*salesGSTCodesOutput, error) {
		on, err := gstDay(in.On)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := salesGSTCodesOutput{Body: SalesGSTCodes{Codes: []SalesGSTCode{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := gst.CodesOn(ctx, u, on, pack.DirSupply)
			if err != nil {
				return err
			}
			out.Body.Version, out.Body.Rate = c.Version, c.Rate
			for _, x := range c.Codes {
				out.Body.Codes = append(out.Body.Codes, SalesGSTCode{Code: x.Code, Description: x.Description, Rated: x.StandardRated})
			}
			return nil
		})
		return &out, err
	})
}
