package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/mcpregister"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Third-party AI apps (PRD-001 AI-46, rev 0.611; p2-api-third-parties task 5, design D6). An Administrator registers, by hand, an AI app of another publisher: its
// name, who publishes it, what the organisation uses it for, where it processes data and the exact redirect addresses it signs people in on. Never automatic, never
// by a Client ID Metadata Document: nothing the Administrator names is fetched, and the same policy that checks a client registering itself (AI-90) checks the name and the
// redirects. The app is created as an agent-class public client with PKCE and the default read scope alone, and cannot connect until an Owner adds it to the apps that may connect
// (mcp.clients, a second approver's setting): the Owner's approval is the one AI-90 uses. It is off until an Owner turns on `mcp.third_party` with a second approver, and with
// AI apps through MCP on. The registration makes a data inventory entry (CMP-PD-02), a transfer to assess where the app processes outside Singapore (CMP-PD-06), and the app
// is in the quarterly access review (FR-CTL-06). Removing one is never held back: it narrows.

// The refusals of a third-party app's registration (ENG-23).
const (
	CodeThirdPartyOff   = "MCP-REG-05" // an Owner has not turned on third-party apps, or AI apps through MCP
	CodeThirdPartyApp   = "MCP-REG-06" // no such registered app, or it is removed already
	CodeThirdPartyField = "MCP-REG-07" // the publisher's address or a stated field is not acceptable
)

// ThirdPartyAppCodes are the refusals of the registration operations.
var ThirdPartyAppCodes = []string{CodeThirdPartyOff, CodeThirdPartyApp, CodeThirdPartyField, CodeRegPolicy, CodeRegLimit, CodeRegBackend, CodeRegClosed}

// ThirdPartyAppRegister is an app for an Administrator to register.
type ThirdPartyAppRegister struct {
	Name               string        `json:"name" minLength:"1" maxLength:"100" doc:"The name it is known by, in plain letters and digits; it cannot pass for Fyniq or a known brand." example:"Acme Books Assistant"`
	Publisher          string        `json:"publisher" minLength:"1" maxLength:"100" doc:"Who publishes it, as you have verified it yourself: Fyniq does not fetch or check it." example:"Acme Software Pte Ltd"`
	PublisherURL       string        `json:"publisher_url,omitempty" maxLength:"2048" doc:"The publisher's own https address, shown to the Owner; never fetched." example:"https://acme.example"`
	RedirectURIs       []RedirectURI `json:"redirect_uris" minItems:"1" maxItems:"5" doc:"The exact https (or loopback) addresses it signs people in on: no wildcard, query or fragment."`
	Purpose            string        `json:"purpose" minLength:"1" maxLength:"300" doc:"What the organisation uses it for, for the data inventory." example:"Ask questions about the management accounts from an AI assistant"`
	ProcessingLocation string        `json:"processing_location" minLength:"1" maxLength:"100" doc:"Where the publisher processes what it reads." example:"United States"`
	OutsideSingapore   bool          `json:"outside_singapore" doc:"True when it processes outside Singapore: a transfer to assess under the PDPA before an Owner allows it (CMP-PD-06)." example:"true"`
}

// ThirdPartyApp is a registered app as the organisation holds it.
type ThirdPartyApp struct {
	ClientID           string        `json:"client_id" doc:"Its client ID at Fyniq's OAuth server." example:"mcp-0123456789abcdef0123"`
	Name               string        `json:"name" example:"Acme Books Assistant"`
	Publisher          string        `json:"publisher" doc:"As the Administrator stated it: Fyniq has not verified it." example:"Acme Software Pte Ltd"`
	PublisherURL       string        `json:"publisher_url,omitempty" example:"https://acme.example"`
	RedirectURIs       []RedirectURI `json:"redirect_uris" nullable:"false" maxItems:"5"`
	Purpose            string        `json:"purpose" example:"Ask questions about the management accounts"`
	ProcessingLocation string        `json:"processing_location" example:"United States"`
	OutsideSingapore   bool          `json:"outside_singapore" example:"true"`
	InventoryEntry     string        `json:"inventory_entry" format:"uuid" doc:"The data inventory entry made with the registration." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c31"`
	Status             string        `json:"status" enum:"pending,approved,denied,expired,removed" doc:"pending until an Owner adds it to the apps that may connect; removed once an Administrator took it away." example:"pending"`
	RegisteredBy       string        `json:"registered_by" format:"uuid" doc:"The Administrator who registered it." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0e"`
	RegisteredAt       time.Time     `json:"registered_at" example:"2026-11-21T09:30:00Z"`
	RemovedAt          *time.Time    `json:"removed_at,omitempty" example:"2026-12-01T09:30:00Z"`
}

// ThirdPartyApps are the organisation's registered third-party apps, newest first.
type ThirdPartyApps struct {
	Apps []ThirdPartyApp `json:"apps" nullable:"false" maxItems:"200" doc:"Newest first, removed ones included, for the access review."`
}

type (
	thirdPartyRegisterInput struct{ Body ThirdPartyAppRegister }
	thirdPartyOutput        struct{ Body ThirdPartyApp }
	thirdPartyListOutput    struct{ Body ThirdPartyApps }
	// ThirdPartyAppPath names a registered app.
	ThirdPartyAppPath struct {
		ClientID string `path:"client_id" pattern:"^mcp-[0-9a-f]{20}$" doc:"The app's client ID." example:"mcp-0123456789abcdef0123"`
	}
	thirdPartyRemoveInput struct{ ThirdPartyAppPath }
)

func registerThirdPartyApps(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	people := []string{CodeSessionRequired, CodeStepUpRequired, CodeMalformed, CodeTooLarge, CodeMediaType}

	register(a, authz.ListMCPThirdPartyApps, exempt(huma.Operation{
		OperationID: "listMCPThirdPartyApps", Method: http.MethodGet, Path: "/v1/connected-apps/third-party-apps", Tags: []string{"Connected apps"},
		Summary: "List the third-party AI apps an Administrator registered",
		Description: "The AI apps of other publishers registered by hand (AI-46), newest first, each with who publishes it as the Administrator stated it, what it is for, where it " +
			"processes data, its redirect addresses and whether an Owner has approved it. Read-only; no side effects.",
	}, "cursor", "The registered apps are capped at 200 and read whole, like the registrations waiting for an Owner (AI-46)."), locks, func(ctx context.Context, _ *struct{}) (*thirdPartyListOutput, error) {
		p, _ := authn.From(ctx)
		out := &thirdPartyListOutput{Body: ThirdPartyApps{Apps: []ThirdPartyApp{}}}
		var held []storage.MCPThirdPartyApp
		if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			held, err = u.MCPThirdPartyApps().List(ctx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, h := range held {
			out.Body.Apps = append(out.Body.Apps, a.thirdPartyAppOf(ctx, h))
		}
		return out, nil
	})

	register(a, authz.RegisterMCPThirdPartyApp, exempt(exempt(huma.Operation{
		OperationID: "registerMCPThirdPartyApp", Method: http.MethodPost, Path: "/v1/connected-apps/third-party-apps", Tags: []string{"Connected apps"}, DefaultStatus: http.StatusCreated,
		Summary: "Register a third-party AI app by hand",
		Description: "An Administrator, signed in and stepped up, registers an AI app of another publisher (AI-46): never automatic, never from a metadata document, nothing fetched. The name and " +
			"redirect addresses pass the policy that checks a self-registering app (AI-90): a public client with PKCE, exact https or loopback redirects, a name that cannot pass for Fyniq " +
			"or a known brand. The app is created as an agent with the default read scope alone and cannot connect until an Owner adds it to the apps that may connect. Refused (MCP-REG-05) " +
			"until an Owner turns on both \"AI apps through MCP\" and \"An Administrator may register an AI app of another publisher\", each with a second approver. It makes a data inventory " +
			"entry. Side effects: the client at the OAuth server, its registration, the app's record, the inventory entry and their audit events. Not idempotent: each call is a new client. " +
			"Never on the AI's or MCP's tool list.",
	}, "Idempotency-Key", "Each call is meant to make a new client, counted against the organisation's own allowance (AI-46)."),
		"Prefer", "A preview would show a client ID that does not exist and spend the organisation's registration allowance (AI-46)."),
		append(append(append([]string{}, people...), ThirdPartyAppCodes...), locks...), func(ctx context.Context, in *thirdPartyRegisterInput) (*thirdPartyOutput, error) {
			p, err := a.stepUp(ctx, "Registering an AI app")
			if err != nil {
				return nil, err
			}
			if !a.deps.MCPServerOn || a.deps.MCPClients == nil || a.deps.OAuthClients == nil || a.deps.MCPAudience == "" {
				return nil, regRefused(CodeRegClosed, "AI apps cannot register here.", "AI apps through MCP are not on in this deployment")
			}
			b := in.Body
			fields := thirdPartyFieldProblems(b)
			if len(fields) > 0 {
				return nil, invalid(fields)
			}
			var on bool
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				mcp, err := settingOnBool(ctx, a, u, "features.mcp")
				if err != nil {
					return err
				}
				third, err := settingOnBool(ctx, a, u, "mcp.third_party")
				on = mcp && third
				return err
			}); err != nil {
				return nil, err
			}
			if !on {
				return nil, problem(CodeThirdPartyOff, "Registering an AI app of another publisher is off for this organisation.",
					"Ask an Owner to turn on \"AI apps through MCP\" and \"An Administrator may register an AI app of another publisher\", each with a second approver")
			}
			pol, err := mcpregister.Load()
			if err != nil {
				return nil, err
			}
			reg, problems := pol.Check(mcpregister.Request{ClientName: b.Name, RedirectURIs: redirectStrings(b.RedirectURIs)})
			if len(problems) > 0 {
				return nil, regPolicy(problems)
			}
			now := a.deps.Clock.Now()
			made, err := a.registerClientFrom(ctx, reg, now, "org:"+p.Organisation.String())
			if err != nil {
				return nil, err
			}
			var saved storage.MCPThirdPartyApp
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				entry, err := u.Compliance().SaveInventoryEntry(ctx, thirdPartyInventoryEntry(b, made.Body.ClientID, uploader(ctx), now))
				if err != nil {
					return err
				}
				saved, err = u.MCPThirdPartyApps().Register(ctx, storage.MCPThirdPartyApp{ClientID: made.Body.ClientID, Name: reg.Name, Publisher: strings.TrimSpace(b.Publisher),
					PublisherURL: b.PublisherURL, RedirectURIs: reg.RedirectURIs, Purpose: strings.TrimSpace(b.Purpose), ProcessingLocation: strings.TrimSpace(b.ProcessingLocation),
					OutsideSingapore: b.OutsideSingapore, InventoryEntry: entry.EntryID, RegisteredBy: p.UserID, RegisteredAt: now})
				return err
			}); err != nil { // the client must not outlive a registration that did not record: it cannot sign anyone in
				a.undoThirdPartyClient(ctx, made.Body.ClientID, now)
				return nil, err
			}
			a.deps.Log.InfoContext(ctx, "third-party AI app registered", "event", "mcp.third_party_registered", "client", saved.ClientID, "by", p.UserID.String(), "outside_singapore", saved.OutsideSingapore)
			return &thirdPartyOutput{Body: a.thirdPartyAppOf(ctx, saved)}, nil
		})

	register(a, authz.RemoveMCPThirdPartyApp, exempt(exempt(huma.Operation{
		OperationID: "removeMCPThirdPartyApp", Method: http.MethodDelete, Path: "/v1/connected-apps/third-party-apps/{client_id}", Tags: []string{"Connected apps"},
		Summary: "Remove a third-party AI app", DefaultStatus: http.StatusNoContent,
		Description: "Removes a registered app at once (AI-46): it is taken out of the OAuth server, so it can sign no one in and its tokens stop, and a waiting registration is denied. " +
			"It narrows, so no second person is needed; the record and its inventory entry stay for the access review. Side effects: the client at the OAuth server, the registration's status and " +
			"the record's audit event. Never on the AI's or MCP's tool list.",
	}, "Idempotency-Key", "A removal happens once and a second call answers that nothing is registered, so a retry is safe without a key (AI-46)."),
		"Prefer", "A removal narrows and takes effect at once, never held back for a second person (AI-46)."),
		append(append([]string{CodeThirdPartyApp}, people[:2]...), locks...), func(ctx context.Context, in *thirdPartyRemoveInput) (*noContent, error) {
			p, err := a.stepUp(ctx, "Removing an AI app")
			if err != nil {
				return nil, err
			}
			now := a.deps.Clock.Now()
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				_, err := u.MCPThirdPartyApps().Remove(ctx, in.ClientID, p.UserID, now)
				return err
			}); errors.Is(err, ports.ErrNotFound) {
				return nil, problem(CodeThirdPartyApp, "There is no such registered app, or it is removed already.", "Open the third-party apps the organisation registered")
			} else if err != nil {
				return nil, err
			}
			a.undoThirdPartyClient(ctx, in.ClientID, now)
			a.deps.Log.InfoContext(ctx, "third-party AI app removed", "event", "mcp.third_party_removed", "client", in.ClientID, "by", p.UserID.String())
			return &noContent{}, nil
		})
}

// undoThirdPartyClient takes a client out of the OAuth server and denies its registration if it was waiting: a failure is logged, and the hourly expiry takes what is left.
func (a *API) undoThirdPartyClient(ctx context.Context, clientID string, now time.Time) {
	if a.deps.MCPClients != nil {
		if _, err := a.deps.MCPClients.DecideClient(ctx, clientID, storage.MCPClientDenied, now); err != nil {
			a.deps.Log.WarnContext(ctx, "mcp.third_party_not_denied", "client", clientID, "error", err.Error())
		}
	}
	if a.deps.OAuthClients != nil {
		if err := a.deps.OAuthClients.RemoveClient(ctx, clientID); err != nil {
			a.deps.Log.WarnContext(ctx, "mcp.third_party_client_not_removed", "client", clientID, "error", err.Error())
		}
	}
}

// thirdPartyFieldProblems are the stated fields that are empty, or a publisher's address that is not a plain https address.
func thirdPartyFieldProblems(b ThirdPartyAppRegister) []FieldProblem {
	var bad []FieldProblem
	for _, f := range []struct{ field, v string }{{"name", b.Name}, {"publisher", b.Publisher}, {"purpose", b.Purpose}, {"processing_location", b.ProcessingLocation}} {
		if strings.TrimSpace(f.v) == "" {
			bad = append(bad, FieldProblem{Code: CodeThirdPartyField, Field: "body." + f.field, Message: "is empty", Fix: "Say " + strings.ReplaceAll(f.field, "_", " ") + ", in a sentence"})
		}
	}
	if b.PublisherURL != "" {
		if u, err := url.Parse(b.PublisherURL); err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			bad = append(bad, FieldProblem{Code: CodeThirdPartyField, Field: "body.publisher_url", Message: "is not an https address", Fix: "Give the publisher's own https address, or leave it out"})
		}
	}
	return bad
}

// thirdPartyInventoryEntry is the data inventory entry (CMP-PD-02) made with a registration: the processing path, and a transfer to assess where the app is outside Singapore
// (CMP-PD-06). The Retention is the publisher's, which Fyniq cannot know: it says so, for the DPO to fill in.
func thirdPartyInventoryEntry(b ThirdPartyAppRegister, clientID, by string, at time.Time) storage.InventoryEntry {
	transfers := "Processed in Singapore, as the Administrator stated it: no transfer."
	if b.OutsideSingapore {
		transfers = "Processes outside Singapore (" + strings.TrimSpace(b.ProcessingLocation) + "): a transfer to assess under the PDPA (CMP-PD-06) before an Owner allows the app."
	}
	return storage.InventoryEntry{EntryID: uuid.Must(uuid.NewV7()), Version: 1, What: "What the AI app " + strings.TrimSpace(b.Name) + " (" + clientID + ") reads for a person who connects it: the figures its read tools return, filtered to that person's rights",
		Why: strings.TrimSpace(b.Purpose), Where: strings.TrimSpace(b.ProcessingLocation), Who: strings.TrimSpace(b.Publisher) + " and the person who connected it",
		Retention: "As the publisher's terms say: not known to Fyniq; the DPO records it [verify]", Transfers: transfers, NeedsDPIA: b.OutsideSingapore, State: "open", SavedBy: by, SavedAt: at}
}

// thirdPartyAppOf is a record as the contract shows it, with the registration's status at the OAuth server's own record.
func (a *API) thirdPartyAppOf(ctx context.Context, h storage.MCPThirdPartyApp) ThirdPartyApp {
	status := "pending"
	switch {
	case h.RemovedAt != nil:
		status = "removed"
	case a.deps.MCPClients != nil:
		if s, err := a.deps.MCPClients.ClientStatus(ctx, h.ClientID); err == nil && s != "" {
			status = s
		}
	}
	return ThirdPartyApp{ClientID: h.ClientID, Name: h.Name, Publisher: h.Publisher, PublisherURL: h.PublisherURL, RedirectURIs: redirects(h.RedirectURIs), Purpose: h.Purpose,
		ProcessingLocation: h.ProcessingLocation, OutsideSingapore: h.OutsideSingapore, InventoryEntry: h.InventoryEntry.String(), Status: status, RegisteredBy: h.RegisteredBy.String(),
		RegisteredAt: h.RegisteredAt.UTC(), RemovedAt: h.RemovedAt}
}

// redirectStrings is the redirect addresses as plain strings, for the registration policy.
func redirectStrings(in []RedirectURI) []string {
	out := make([]string, len(in))
	for i, u := range in {
		out[i] = string(u)
	}
	return out
}
