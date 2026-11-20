package hitpay

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Payment links, Model A (PRD-001 FR-AR-13, FR-PSP-02, rev 0.510; p2-bank-feeds-payments design D1): HitPay's create-payment-request
// call, `POST /v1/payment-requests` with a form body (docs.hitpayapp.com, "Online payments", retrieved 20 Nov 2026), the only write
// Fyniq makes at HitPay. The client sends that one request and refuses any other method or path before it leaves, so even a
// bug cannot charge, refund or pay out with it. HitPay documents no read-only key (the key already could), and the
// webhook parameter of the call is deprecated, so none is sent: the account's own webhook, set up once, reaches the
// receiver. [live-verify: the sandbox's answer to every field below; whether a currency other than SGD is offered.]

// PaymentRequestPath is the one path the link client calls.
const PaymentRequestPath = "/v1/payment-requests"

// sgt is HitPay's time zone for expiry_date ("time in SGT"): a fixed offset, so no zone database is needed.
var sgt = time.FixedZone("SGT", 8*60*60)

// Linker makes HitPay payment requests.
type Linker struct {
	Base string // ProductionBase or SandboxBase, or a mock's
	Key  string // the business API key
	HTTP *http.Client
}

var _ ports.PaymentLinks = (*Linker)(nil)

type postOnly struct{ next http.RoundTripper }

func (p postOnly) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodPost || r.URL.Path != PaymentRequestPath {
		return nil, fmt.Errorf("%w: refused %s %s: the link client only creates a payment request", ErrMethod, r.Method, r.URL.Path)
	}
	return p.next.RoundTrip(r)
}

func (l *Linker) client() *http.Client {
	base := l.HTTP
	if base == nil || base.Transport == nil {
		base = egress.Public()
	}
	wrapped := *base
	wrapped.Transport = postOnly{next: base.Transport}
	return &wrapped
}

// CreatePaymentRequest implements ports.PaymentLinks. The amount is sent as the exact decimal text it was given, in the
// currency's scale (ENG-12); an answer whose URL is not https is refused, because it is shown to customers.
func (l *Linker) CreatePaymentRequest(ctx context.Context, r ports.PaymentRequest) (ports.PaymentRequestMade, error) {
	cur, err := money.Lookup(r.Currency)
	if err != nil {
		return ports.PaymentRequestMade{}, fmt.Errorf("hitpay: currency %q: %w", r.Currency, err)
	}
	amount, err := money.Parse(r.Amount, cur)
	if err != nil || amount.Sign() <= 0 {
		return ports.PaymentRequestMade{}, fmt.Errorf("hitpay: the amount %q is not a positive amount in %s", r.Amount, cur.Code)
	}
	if strings.TrimSpace(r.Reference) == "" {
		return ports.PaymentRequestMade{}, fmt.Errorf("hitpay: a payment request needs the invoice's number as its reference")
	}
	form := url.Values{"amount": {amount.String()}, "currency": {cur.Code}, "reference_number": {r.Reference}}
	if r.Purpose != "" {
		form.Set("purpose", r.Purpose)
	}
	for _, m := range r.Methods {
		form.Add("payment_methods[]", m)
	}
	if r.RedirectURL != "" {
		form.Set("redirect_url", r.RedirectURL)
	}
	if r.ExpiresAt != nil {
		form.Set("expiry_date", r.ExpiresAt.In(sgt).Format("2006-01-02 15:04:05"))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(l.Base, "/")+PaymentRequestPath, strings.NewReader(form.Encode()))
	if err != nil {
		return ports.PaymentRequestMade{}, err
	}
	req.Header.Set("X-BUSINESS-API-KEY", l.Key)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := l.client().Do(req)
	if err != nil {
		return ports.PaymentRequestMade{}, fmt.Errorf("hitpay: %w", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return ports.PaymentRequestMade{}, fmt.Errorf("hitpay: reading the answer: %w", err)
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return ports.PaymentRequestMade{}, fmt.Errorf("hitpay: %w: %s answered %d", ports.ErrPaymentRequestRefused, PaymentRequestPath, res.StatusCode)
	}
	var doc obj
	if err := decode(body, &doc); err != nil {
		return ports.PaymentRequestMade{}, fmt.Errorf("hitpay: %s answered what is not its JSON: %w", PaymentRequestPath, err)
	}
	made := ports.PaymentRequestMade{ID: str(doc, "id"), URL: str(doc, "url"), Status: str(doc, "status")}
	if made.ID == "" || strings.ContainsAny(made.ID, "/?#% ") || !strings.HasPrefix(made.URL, "https://") {
		return ports.PaymentRequestMade{}, fmt.Errorf("hitpay: %w: the answer has no usable ID and https URL", ports.ErrPaymentRequestRefused)
	}
	if t := when(doc, "expiry_date"); !t.IsZero() {
		made.ExpiresAt = &t
	} else if r.ExpiresAt != nil {
		made.ExpiresAt = r.ExpiresAt
	}
	return made, nil
}
