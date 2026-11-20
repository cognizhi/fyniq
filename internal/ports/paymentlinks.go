package ports

import (
	"context"
	"errors"
	"time"
)

// PaymentLinks is a payment-provider connector's one write (PRD-001 FR-AR-13, FR-PSP-02, rev 0.510; p2-bank-feeds-payments design
// D1): it asks the provider for a payment request, a page where a customer pays an invoice's open balance, and nothing else. It is
// a port of its own because PaymentProvider reads only (FR-INT-27): a connector that offers it declares the capability
// "payment-link", which the API's command for a person alone triggers, and the AI and MCP have no route to. It cannot charge,
// refund, cancel or pay out: a test holds the method set to this one method.
type PaymentLinks interface {
	// CreatePaymentRequest makes the payment request. The amount is what the invoice has open, read by the caller on the server.
	CreatePaymentRequest(ctx context.Context, r PaymentRequest) (PaymentRequestMade, error)
}

// PaymentRequest is what a payment request asks for.
type PaymentRequest struct {
	// Reference is the invoice's number, which the provider keeps and echoes on its charges.
	Reference string
	// Amount is an exact decimal string in major units of Currency (ENG-12).
	Amount   string
	Currency string
	// Purpose is the line the customer sees, such as "Invoice INV-0150".
	Purpose string
	// Methods are the payment methods the page offers, in the provider's words (HitPay: "paynow_online", "card").
	Methods []string
	// RedirectURL is where the customer lands after paying; empty: the provider's page.
	RedirectURL string
	// ExpiresAt is when the link stops working; nil: the provider's default.
	ExpiresAt *time.Time
}

// PaymentRequestMade is the provider's payment request.
type PaymentRequestMade struct {
	ID        string
	URL       string
	Status    string
	ExpiresAt *time.Time
}

// ErrPaymentRequestRefused is a provider's refusal of a payment request, with no body (the body is never kept).
var ErrPaymentRequestRefused = errors.New("the provider refused the payment request")
