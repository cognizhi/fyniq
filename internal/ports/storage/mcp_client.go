package storage

import (
	"context"
	"time"
)

// CodeMCPClient is the database's refusal of a registered client's step (FY343): a change to what it registered, a step out of
// its course, or a delete.
const CodeMCPClient = "STORAGE-MCP-CLIENT-01"

// The statuses of a registered AI app (AI-90).
const (
	MCPClientPending  = "pending"
	MCPClientApproved = "approved"
	MCPClientDenied   = "denied"
	MCPClientExpired  = "expired"
)

// MCPClientRegistration is an AI app that registered itself.
type MCPClientRegistration struct {
	ID, Name     string
	RedirectURIs []string
	Network      string // the registering address as a /24 or /48
	At           time.Time
}

// MCPClientLimits are what one registration is counted against.
type MCPClientLimits struct{ PerHour, PerDay, PendingCap int }

// MCPPendingClient is a registration waiting for an Owner.
type MCPPendingClient struct {
	ID, Name     string
	RedirectURIs []string
	RegisteredAt time.Time
}

// MCPClients is the installation's record of the AI apps that registered themselves (AI-90). It is global, no organisation's:
// the store reaches it through the database's functions, which hold the policy's database half.
type MCPClients interface {
	// RegistrationOpen is whether any organisation has registration on at now.
	RegistrationOpen(ctx context.Context, now time.Time) (bool, error)
	// Register records a registration; it returns "" when it did, otherwise why not: limit_hour, limit_day or pending_full.
	Register(ctx context.Context, c MCPClientRegistration, l MCPClientLimits) (refused string, err error)
	// ClientStatus is a registered client's status; "" for one that never registered.
	ClientStatus(ctx context.Context, id string) (string, error)
	// DecideClient approves or denies a pending registration, once; it says whether this call decided it.
	DecideClient(ctx context.Context, id, status string, now time.Time) (bool, error)
	// TouchClient notes that an approved client was used.
	TouchClient(ctx context.Context, id string, now time.Time) error
	// PendingClients are the registrations waiting for an Owner, newest first.
	PendingClients(ctx context.Context) ([]MCPPendingClient, error)
	// ExpireClients marks as expired the registrations never approved within pending, and the approved ones never used within
	// unused; it returns their IDs, for the caller to remove from the OAuth server.
	ExpireClients(ctx context.Context, now time.Time, pending, unused time.Duration) ([]string, error)
}
