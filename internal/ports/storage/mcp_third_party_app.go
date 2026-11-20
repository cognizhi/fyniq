package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// MCPThirdPartyApp is an AI app of another publisher that an Administrator registered by hand (AI-46, rev 0.608): the organisation's record beside the installation's
// registration of the client (MCPClients). Publisher and purpose are what the Administrator stated and Fyniq never fetched; InventoryEntry is the data inventory entry made
// with it. The app cannot connect until an Owner adds it to the apps that may connect.
type MCPThirdPartyApp struct {
	ID                 uuid.UUID
	ClientID           string
	Name               string
	Publisher          string
	PublisherURL       string
	RedirectURIs       []string
	Purpose            string
	ProcessingLocation string
	OutsideSingapore   bool
	InventoryEntry     uuid.UUID
	RegisteredBy       uuid.UUID
	RegisteredAt       time.Time
	RemovedAt          *time.Time
	RemovedBy          *uuid.UUID
}

// MCPThirdPartyApps keeps an organisation's registered third-party AI apps. Each change is audited in its transaction.
type MCPThirdPartyApps interface {
	// Register records an app the Administrator registered, with the data inventory entry made with it.
	Register(ctx context.Context, a MCPThirdPartyApp) (MCPThirdPartyApp, error)
	// List is the organisation's apps, newest first, removed ones included.
	List(ctx context.Context) ([]MCPThirdPartyApp, error)
	// Get is the app registered under clientID, or ports.ErrNotFound.
	Get(ctx context.Context, clientID string) (MCPThirdPartyApp, error)
	// Remove notes that by removed the app; ports.ErrNotFound when it is not here or is removed already.
	Remove(ctx context.Context, clientID string, by uuid.UUID, at time.Time) (MCPThirdPartyApp, error)
}
