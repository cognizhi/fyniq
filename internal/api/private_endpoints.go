package api

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ registered private endpoints (SEC-37)

// A destination on a private network (a connector inside an organisation's network, or from p1-ai-assistant a
// self-hosted model) is reachable only once a Super admin, the platform operator in the app, registers it: its private
// address or range, its port, the key its TLS certificate must present, the organisation and service it is for, and the
// credential bound to it (p1-security-hardening design D3). Each change is a new version; retiring one is a version too.
// With two Super admins a registration waits for the other's approval, as an installation change of step
// private_endpoint, approved with approveInstallationChange (FR-ADM-07).

// stepPrivateEndpoint is the installation change's step for a private endpoint's registration.
const stepPrivateEndpoint = "private_endpoint"

// privateEndpointChangeName is the name an installation change gives a private endpoint.
func privateEndpointChangeName(id uuid.UUID) string { return "private-endpoint:" + id.String() }

// privateRanges are the ranges a private endpoint may be in: RFC 1918, the shared address space and unique-local IPv6.
var privateRanges = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("fc00::/7")}

// PrivateEndpoint is the current version of a registered private endpoint.
type PrivateEndpoint struct {
	ID             uuid.UUID  `json:"id" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e60"`
	Version        int        `json:"version" doc:"Its version, for If-Match when it is changed or retired." example:"1"`
	Address        string     `json:"address" doc:"The private address or range, in CIDR notation." example:"10.20.0.0/28"`
	Port           int        `json:"port" doc:"The port it is reached on, over TLS only." example:"443"`
	TLSPin         string     `json:"tls_pin" doc:"The base64 SHA-256 of the SubjectPublicKeyInfo its TLS certificate must present." example:"n4bQgYhMfWWaL+qgxVrQFaO/TxsrC4Is0V1sFbDwCgg="`
	Service        string     `json:"service" enum:"connector,ai_model" doc:"What it is for: a connector inside an organisation's network, or a self-hosted model (AI-79)." example:"connector"`
	OrganisationID uuid.UUID  `json:"organisation_id" doc:"The organisation it is for." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e00"`
	Credential     uuid.UUID  `json:"credential" doc:"The credential bound to it: for a connector, the organisation's connector. Only requests with that credential reach it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e61"`
	Retired        bool       `json:"retired" doc:"true once retired: nothing reaches it, and it is final." example:"false"`
	Reason         string     `json:"reason" doc:"Why this version was registered." example:"Acme's ledger API inside its own network"`
	RecordedBy     uuid.UUID  `json:"recorded_by" doc:"The Super admin who registered this version." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e41"`
	RecordedAt     time.Time  `json:"recorded_at" example:"2026-11-10T09:00:00Z"`
	ChangeID       *uuid.UUID `json:"change_id,omitempty" doc:"The installation change the other Super admin approved, when there were two." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e42"`
}

// PrivateEndpointPage is the registered private endpoints, the most recently recorded first.
type PrivateEndpointPage struct {
	Items      []PrivateEndpoint `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty" doc:"The cursor of the next page; absent on the last." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// PrivateEndpointRequest is a version of a private endpoint as a Super admin registers it.
type PrivateEndpointRequest struct {
	Replaces       string `json:"replaces,omitempty" format:"uuid" doc:"The endpoint this is a new version of, with If-Match; absent: a new endpoint." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e60"`
	Address        string `json:"address" maxLength:"43" doc:"The private address, or the narrowest range the service listens on: within 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 100.64.0.0/10 or fc00::/7, no wider than a /24 or a /64, and outside the platform's own networks." example:"10.20.0.0/28"`
	Port           int    `json:"port" minimum:"1" maximum:"65535" doc:"The port it listens on; only TLS connections are made to it." example:"443"`
	TLSPin         string `json:"tls_pin" maxLength:"44" doc:"The base64 SHA-256 of the SubjectPublicKeyInfo its TLS certificate must present: openssl x509 -pubkey -noout | openssl pkey -pubin -outform der | openssl dgst -sha256 -binary | base64." example:"n4bQgYhMfWWaL+qgxVrQFaO/TxsrC4Is0V1sFbDwCgg="`
	Service        string `json:"service" enum:"connector,ai_model" doc:"What it is for: a connector inside an organisation's network, or a self-hosted model (AI-79)." example:"connector"`
	OrganisationID string `json:"organisation_id" format:"uuid" doc:"The organisation it is for." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e00"`
	Credential     string `json:"credential" format:"uuid" doc:"The credential bound to it: for a connector, the organisation's connector (its ID in listOrganisationConnectors)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e61"`
	Retired        bool   `json:"retired,omitempty" doc:"With replaces: retire the endpoint. Nothing reaches it afterwards, and it is final." example:"false"`
	Reason         string `json:"reason" minLength:"3" maxLength:"500" doc:"Why it is registered, changed or retired: the installation's trail keeps it." example:"Acme's ledger API inside its own network"`
}

type privateEndpointListInput struct{ PageQuery }

type privateEndpointListOutput struct{ Body PrivateEndpointPage }

type privateEndpointRegisterInput struct {
	KeyHeader
	PreferHeader
	IfMatch string `header:"If-Match" doc:"With replaces: the endpoint's version as listed, quoted." example:"\"1\""`
	Body    PrivateEndpointRequest
}

// PrivateEndpointCodes are the refusals of a private endpoint's registration.
var PrivateEndpointCodes = []string{storage.CodePrivateEndpoint, storage.CodeVersion, CodeNotFound}

func privateEndpointOf(e storage.PrivateEndpoint) PrivateEndpoint {
	return PrivateEndpoint{ID: e.ID, Version: e.Version, Address: e.Address.String(), Port: e.Port, TLSPin: e.Pin, Service: e.Service,
		OrganisationID: e.Serves, Credential: e.Credential, Retired: e.Retired, Reason: e.Reason, RecordedBy: e.RecordedBy,
		RecordedAt: e.RecordedAt.UTC(), ChangeID: e.Change}
}

// privateEndpointChange checks a registration and returns it as a change at the version it follows.
func (a *API) privateEndpointChange(in *privateEndpointRegisterInput, current []storage.PrivateEndpoint) (storage.PrivateEndpointChange, error) {
	b := in.Body
	var bad []FieldProblem
	fail := func(field, message, fix string) {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body." + field, Message: message, Fix: fix})
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(b.Address))
	if a, aerr := netip.ParseAddr(strings.TrimSpace(b.Address)); aerr == nil {
		prefix, err = a.Prefix(a.BitLen())
	}
	switch {
	case err != nil:
		fail("address", "is not an address or a range", "Give the private address, such as 10.20.0.5, or its range, such as 10.20.0.0/28")
	case !inPrivateRange(prefix.Masked()):
		fail("address", "is not a private range of at most /24 (IPv4) or /64 (IPv6)",
			"Register the private address or the narrowest range the service listens on; a public address needs no registration")
	case a.deps.Connectors.Runtime != nil && a.deps.Connectors.Runtime.Egress != nil && a.deps.Connectors.Runtime.Egress.IsOwn(prefix.Masked()):
		fail("address", "is one of the platform's own networks", "Register the organisation's address; the platform's own services are never reachable")
	}
	if _, err := egress.ParsePin(b.TLSPin); err != nil {
		fail("tls_pin", "is not the base64 of a SHA-256", "Give the base64 SHA-256 of the certificate's SubjectPublicKeyInfo: 44 characters ending in =")
	}
	org, err := uuid.Parse(b.OrganisationID)
	if err != nil {
		fail("organisation_id", "is not an ID", "Give the organisation's ID")
	}
	cred, err := uuid.Parse(b.Credential)
	if err != nil {
		fail("credential", "is not an ID", "Give the ID of the credential bound to it: for a connector, the organisation's connector")
	}
	c := storage.PrivateEndpointChange{ID: uuid.Must(uuid.NewV7()), Address: prefix.Masked().String(), Port: b.Port, Pin: b.TLSPin,
		Service: b.Service, Serves: org, Credential: cred, Retired: b.Retired, Reason: strings.TrimSpace(b.Reason)}
	if b.Replaces == "" {
		if b.Retired {
			fail("retired", "is for an endpoint already registered", "Name it in replaces, or leave retired out")
		}
		if len(bad) > 0 {
			return c, invalid(bad)
		}
		return c, nil
	}
	id, err := uuid.Parse(b.Replaces)
	if err != nil {
		fail("replaces", "is not an ID", "Give the endpoint's ID as listed")
	}
	if len(bad) > 0 {
		return c, invalid(bad)
	}
	for _, e := range current {
		if e.ID == id {
			if err := checkIfMatch(in.IfMatch, etag(e.Version)); err != nil {
				return c, err
			}
			c.ID, c.Version = id, e.Version
			return c, nil
		}
	}
	return c, problem(CodeNotFound, "No private endpoint "+b.Replaces+" is registered.", "List the private endpoints to find their IDs")
}

// inPrivateRange reports whether p is in a private range and no wider than a /24 or a /64.
func inPrivateRange(p netip.Prefix) bool {
	limit := 24
	if p.Addr().Is6() {
		limit = 64
	}
	if !p.IsValid() || p.Bits() < limit {
		return false
	}
	for _, r := range privateRanges {
		if r.Contains(p.Addr()) && r.Bits() <= p.Bits() {
			return true
		}
	}
	return false
}

// privateEndpointSummary says in a few words what a registration does.
func privateEndpointSummary(c storage.PrivateEndpointChange) string {
	where := c.Address + " port " + strconv.Itoa(c.Port)
	switch {
	case c.Retired:
		return "Retire the private endpoint " + where
	case c.Version > 0:
		return "Change the private endpoint " + where + " for organisation " + c.Serves.String()
	default:
		return "Register the private endpoint " + where + " for organisation " + c.Serves.String()
	}
}

// applyPrivateEndpoint registers c by by, as approved by change (nil: none needed), and tells the Super admins.
func (a *API) applyPrivateEndpoint(ctx context.Context, u storage.UnitOfWork, by uuid.UUID, change *uuid.UUID,
	c storage.PrivateEndpointChange) (PrivateEndpoint, error) {
	e, err := u.PrivateEndpoints().Register(ctx, by, change, c, a.deps.Clock.Now())
	if err != nil {
		return PrivateEndpoint{}, err
	}
	n, err := a.deps.Installation.CountSuperAdmins(ctx)
	if err != nil {
		return PrivateEndpoint{}, err
	}
	title := privateEndpointSummary(c) + ": done"
	if n <= 1 {
		title += " at once, since there is one Super admin"
	}
	return privateEndpointOf(e), a.relay(ctx, u, notify.Event{Type: notify.PrivateEndpointType, RecordType: "private_endpoint", RecordID: &e.ID,
		Title: title + ". If it was not you, tell the other Super admin and check the installation's trail.", Audience: notify.SuperAdmins()})
}

// approvePrivateEndpointChange approves pending installation change p, a private endpoint's registration, and applies it
// in the same unit of work.
func (a *API) approvePrivateEndpointChange(ctx context.Context, u storage.UnitOfWork, by uuid.UUID, mfaAt time.Time, p storage.InstallationChange,
	hash []byte) (any, error) {
	var c storage.PrivateEndpointChange
	if err := json.Unmarshal(p.Change, &c); err != nil {
		return nil, err
	}
	if _, err := u.Connectors().DecideInstallationChange(ctx, by, p.ID, hash, true, mfaAt, "", a.deps.Clock.Now()); err != nil {
		return nil, err
	}
	id := p.ID
	return a.applyPrivateEndpoint(ctx, u, by, &id, c)
}

func registerPrivateEndpoints(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType}, locks...)
	admin := append([]string{CodeNotSuperAdmin, CodeStepUpRequired, CodeSessionRequired, CodeUnavailable}, locks...)

	register(a, authz.ListPrivateEndpoints, huma.Operation{
		OperationID: "listPrivateEndpoints", Method: http.MethodGet, Path: "/v1/installation/private-endpoints", Tags: []string{"Installation"},
		Summary: "List the registered private endpoints",
		Description: "Every registered private endpoint (SEC-37), at its current version, retired ones included: its private address or range, " +
			"its port, the key its TLS certificate must present, the organisation and service it is for, and the credential bound to it. " +
			"Only requests with that credential reach it, over TLS with that key; an address an organisation's user types never reaches a " +
			"private network. " + authz.ListPrivateEndpoints.Note + " Read-only; no side effects.",
	}, append([]string{CodeCursor}, admin...), func(ctx context.Context, in *privateEndpointListInput) (*privateEndpointListOutput, error) {
		by, err := a.superAdmin(ctx, "Listing the private endpoints")
		if err != nil {
			return nil, err
		}
		out := PrivateEndpointPage{Items: []PrivateEndpoint{}}
		err = a.inInstallation(ctx, by, func(ctx context.Context, _ uuid.UUID, u storage.UnitOfWork) error {
			all, err := u.PrivateEndpoints().List(ctx)
			if err != nil {
				return err
			}
			items := make([]PrivateEndpoint, 0, len(all))
			for _, e := range all {
				items = append(items, privateEndpointOf(e))
			}
			slices.SortFunc(items, func(x, y PrivateEndpoint) int { // the most recently recorded first, then by ID
				return cmp.Or(y.RecordedAt.Compare(x.RecordedAt), strings.Compare(x.ID.String(), y.ID.String()))
			})
			out.Items, out.NextCursor, err = pageOf(items, func(e PrivateEndpoint) string { return e.ID.String() }, in.PageQuery)
			return err
		})
		return &privateEndpointListOutput{Body: out}, err
	})

	register(a, authz.RegisterPrivateEndpoint, pendsChange(a, huma.Operation{
		OperationID: "registerPrivateEndpoint", Method: http.MethodPost, Path: "/v1/installation/private-endpoints", Tags: []string{"Installation"},
		DefaultStatus: http.StatusCreated, Summary: "Register, change or retire a private endpoint",
		Description: "Registers a private endpoint (SEC-37), or with replaces and If-Match a new version of one, or retires it: a private " +
			"address or range (no wider than a /24 or a /64, outside the platform's own networks), its port, the base64 SHA-256 of the key its " +
			"TLS certificate must present, the organisation and service it is for, and the credential bound to it. Only that credential's " +
			"requests reach it, over TLS with that key; it follows no redirect, and the metadata service and the platform's own services stay " +
			"refused. Needs an active Super admin who passed step-up in the last 5 minutes. With two Super admins the answer is 202 with the " +
			"change the other one approves with approveInstallationChange (FR-ADM-07); with one it applies at once. Side effects: the new " +
			"version, or the change request, its event in the installation's trail and a security notice to every Super admin. Idempotent " +
			"with Idempotency-Key; Prefer: return=preview checks it and changes nothing.",
	}, reflect.TypeFor[PrivateEndpoint]()), append(append(append([]string{CodeInvalid, CodeIfMatchRequired, CodeStale}, admin...), writes...),
		PrivateEndpointCodes...),
		func(ctx context.Context, in *privateEndpointRegisterInput) (*connectorWriteOutput, error) {
			by, err := a.superAdmin(ctx, "Registering a private endpoint")
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			status := http.StatusCreated
			if in.Body.Replaces != "" {
				status = http.StatusOK
			}
			w, err := inInstallationWrite(ctx, a, by, in.IdempotencyKey, in.Prefer, in, status, func(ctx context.Context, u storage.UnitOfWork) (any, error) {
				current, err := u.PrivateEndpoints().List(ctx)
				if err != nil {
					return nil, err
				}
				c, err := a.privateEndpointChange(in, current)
				if err != nil {
					return nil, err
				}
				n, err := a.deps.Installation.CountSuperAdmins(ctx)
				if err != nil {
					return nil, err
				}
				if n >= 2 {
					summary := privateEndpointSummary(c)
					ch, err := connector.RequestChange(ctx, u, a.deps.Clock, by, p.StepUpAt, nil, privateEndpointChangeName(c.ID), stepPrivateEndpoint, summary, c)
					if err != nil {
						return nil, err
					}
					if err := a.relay(ctx, u, notify.Event{Type: "approval.requested", RecordType: "installation_change", RecordID: &ch.ID,
						Title: summary + ": a private endpoint waits for another Super admin's approval", Audience: notify.SuperAdmins(by)}); err != nil {
						return nil, err
					}
					return installationChangeOf(ch), nil
				}
				return a.applyPrivateEndpoint(ctx, u, by, nil, c)
			})
			if err != nil {
				return nil, err
			}
			out := &connectorWriteOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
			if _, ok := w.Body.(InstallationChange); ok {
				out.Status = http.StatusAccepted
			} else if e, ok := w.Body.(PrivateEndpoint); ok && !w.Previewed {
				out.ETag = etag(e.Version)
			}
			return out, nil
		})
}
