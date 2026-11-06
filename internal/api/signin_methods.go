package api

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ sign-in providers (FR-ADM-08, rev 0.197)

// The providers come from the deployment's configuration (the owner's decision of 6 Nov 2026); a Super admin turns each
// on or off, optionally ending its sessions, and sets the email domains it accepts. With two Super admins a change waits
// for the other's approval, as an installation change of step sign_in, approved with approveInstallationChange.

// stepSignIn is the installation change's step for a sign-in provider's switches.
const stepSignIn = "sign_in"

// signInChangeName is the name an installation change gives a sign-in provider.
func signInChangeName(provider string) string { return "sign-in:" + provider }

// EmailDomain is an email domain a sign-in provider accepts.
type EmailDomain string

// Schema implements huma.SchemaProvider: a lower-case domain, with an example.
func (EmailDomain) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", MaxLength: new(253), Description: "An email domain, lower case: the part of an address after the @.",
		Examples: []any{"cognizhi.com"}}
}

// SignInMethod is one of the installation's sign-in providers, with its switches.
type SignInMethod struct {
	Provider          string              `json:"provider" pattern:"^[a-z0-9][a-z0-9-]{0,62}$" doc:"The provider's ID in the deployment's configuration." example:"zoho"`
	Name              string              `json:"name" doc:"Its name on the sign-in page." example:"Zoho"`
	Enabled           bool                `json:"enabled" doc:"Whether it signs people in; turned off, new sign-ins through it are refused." example:"true"`
	AllowedDomains    []EmailDomain       `json:"allowed_domains" doc:"The email domains it accepts, an account's verified email on one of them; empty: any."`
	SignIns           int                 `json:"sign_ins" doc:"Sessions it started in the last 30 days." example:"96"`
	LastForSuperAdmin bool                `json:"last_for_super_admin" doc:"true when a Super admin signs in with it alone: turning it off, or narrowing its domains past their email, is refused." example:"false"`
	Version           int                 `json:"version" doc:"Its switches' version, for If-Match: 0 until a Super admin first changes them." example:"2"`
	ChangedAt         *time.Time          `json:"changed_at,omitempty" doc:"When its switches last changed; absent until they first do." example:"2026-11-06T09:00:00Z"`
	Pending           *InstallationChange `json:"pending,omitempty" doc:"A change of its switches waiting for the other Super admin, if any."`
}

// SignInMethodPage is the installation's sign-in providers, in the configuration's order.
type SignInMethodPage struct {
	Items      []SignInMethod `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty" doc:"The cursor of the next page; absent on the last." example:"eyJhZnRlciI6Imdvb2dsZSJ9"`
}

// SignInMethodRequest is a provider's switches as a Super admin wants them.
type SignInMethodRequest struct {
	Enabled        bool          `json:"enabled" doc:"Whether it signs people in." example:"false"`
	AllowedDomains []EmailDomain `json:"allowed_domains" maxItems:"20" doc:"The email domains it accepts, such as example.com; empty: any."`
	EndSessions    bool          `json:"end_sessions,omitempty" doc:"With enabled false: also sign out every session it started, at once." example:"true"`
}

// SignInMethodPath names a sign-in provider.
type SignInMethodPath struct {
	Provider string `path:"provider" pattern:"^[a-z0-9][a-z0-9-]{0,62}$" doc:"The provider's ID." example:"zoho"`
}

type signInMethodListInput struct{ PageQuery }

type signInMethodListOutput struct{ Body SignInMethodPage }

type signInMethodChangeInput struct {
	SignInMethodPath
	KeyHeader
	PreferHeader
	IfMatch string `header:"If-Match" doc:"The provider's version as listed, quoted: \"0\" until it first changes." example:"\"0\""`
	Body    SignInMethodRequest
}

var domainName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// SignInMethodCodes are the refusals of a sign-in provider's change.
var SignInMethodCodes = []string{storage.CodeSignInMethod, CodeNotFound}

// signInMethods lists the configured providers with their switches, usage and pending changes.
func (a *API) signInMethods(ctx context.Context, u storage.UnitOfWork) ([]SignInMethod, error) {
	sessions, ok := a.deps.Installation.(storage.Sessions)
	if !ok || a.deps.SignIn == nil {
		return []SignInMethod{}, nil
	}
	all, err := u.SignInMethods().List(ctx)
	if err != nil {
		return nil, err
	}
	essential, err := u.SignInMethods().Essential(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := sessions.SignInCounts(ctx, a.deps.Clock.Now().AddDate(0, 0, -30))
	if err != nil {
		return nil, err
	}
	changes, err := u.Connectors().InstallationChanges(ctx)
	if err != nil {
		return nil, err
	}
	out := []SignInMethod{}
	for _, p := range a.deps.SignIn.Providers() {
		m := storage.MethodOf(all, p.ID)
		s := SignInMethod{Provider: p.ID, Name: p.Name, Enabled: m.Enabled, AllowedDomains: []EmailDomain{}, SignIns: counts[p.ID],
			LastForSuperAdmin: slices.Contains(essential, p.ID), Version: m.Version}
		for _, d := range m.AllowedDomains {
			s.AllowedDomains = append(s.AllowedDomains, EmailDomain(d))
		}
		if m.Version > 0 {
			t := m.ChangedAt.UTC()
			s.ChangedAt = &t
		}
		for _, c := range changes {
			if c.Connector == signInChangeName(p.ID) && c.Status == "pending" {
				ch := installationChangeOf(c)
				s.Pending = &ch
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// signInSummary says in a few words what a change of a provider's switches does.
func signInSummary(name string, before storage.SignInMethod, c storage.SignInMethodChange) string {
	var parts []string
	switch {
	case before.Enabled && !c.Enabled && c.EndSessions:
		parts = append(parts, "turn "+name+" sign-in off and end its sessions")
	case before.Enabled && !c.Enabled:
		parts = append(parts, "turn "+name+" sign-in off")
	case !before.Enabled && c.Enabled:
		parts = append(parts, "turn "+name+" sign-in on")
	}
	if !slices.Equal(before.AllowedDomains, c.AllowedDomains) {
		if len(c.AllowedDomains) == 0 {
			parts = append(parts, "accept any email domain with "+name)
		} else {
			parts = append(parts, "accept only "+strings.Join(c.AllowedDomains, ", ")+" with "+name)
		}
	}
	if len(parts) == 0 {
		return "Keep " + name + " sign-in as it is"
	}
	s := strings.Join(parts, "; ")
	return strings.ToUpper(s[:1]) + s[1:]
}

// applySignInMethod applies c by by, as approved by change (nil: none needed), and tells the Super admins.
func (a *API) applySignInMethod(ctx context.Context, u storage.UnitOfWork, by uuid.UUID, change *uuid.UUID, name string,
	c storage.SignInMethodChange) error {
	all, err := u.SignInMethods().List(ctx)
	if err != nil {
		return err
	}
	before := storage.MethodOf(all, c.Provider)
	if _, err := u.SignInMethods().Set(ctx, by, change, c, a.deps.Clock.Now()); err != nil {
		return err
	}
	n, err := a.deps.Installation.CountSuperAdmins(ctx)
	if err != nil {
		return err
	}
	title := signInSummary(name, before, c) + ": done"
	if n <= 1 {
		title += " at once, since there is one Super admin"
	}
	return a.relay(ctx, u, notify.Event{Type: notify.SignInProviderType, RecordType: "sign_in_method", Link: "/settings/sign-in-providers",
		Title: title + ". If it was not you, tell the other Super admin and check the installation's trail.", Audience: notify.SuperAdmins()})
}

func (a *API) signInMethodNow(ctx context.Context, provider string) (storage.SignInMethod, error) {
	all, err := a.deps.Installation.(storage.Sessions).SignInMethods(ctx)
	if err != nil {
		return storage.SignInMethod{}, err
	}
	return storage.MethodOf(all, provider), nil
}

// approveSignInChange approves pending installation change p, a change of a sign-in provider's switches, and applies it
// in the same unit of work.
func (a *API) approveSignInChange(ctx context.Context, u storage.UnitOfWork, by uuid.UUID, mfaAt time.Time, p storage.InstallationChange,
	hash []byte) (any, error) {
	var c storage.SignInMethodChange
	if err := json.Unmarshal(p.Change, &c); err != nil {
		return nil, err
	}
	if _, err := u.Connectors().DecideInstallationChange(ctx, by, p.ID, hash, true, mfaAt, "", a.deps.Clock.Now()); err != nil {
		return nil, err
	}
	name := c.Provider
	for _, x := range a.deps.SignIn.Providers() {
		if x.ID == c.Provider {
			name = x.Name
		}
	}
	id := p.ID
	if err := a.applySignInMethod(ctx, u, by, &id, name, c); err != nil {
		return nil, err
	}
	all, err := a.signInMethods(ctx, u)
	if err != nil {
		return nil, err
	}
	for _, m := range all {
		if m.Provider == c.Provider {
			return m, nil
		}
	}
	return nil, problem(CodeNotFound, "The provider "+c.Provider+" is not configured any more.", "Ask for the change again.")
}

func registerSignInMethods(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType}, locks...)
	admin := append([]string{CodeNotSuperAdmin, CodeStepUpRequired, CodeSessionRequired, CodeUnavailable}, locks...)

	register(a, authz.ListSignInMethods, huma.Operation{
		OperationID: "listSignInMethods", Method: http.MethodGet, Path: "/v1/installation/sign-in-methods", Tags: []string{"Session"},
		Summary: "List the sign-in providers and their switches",
		Description: "Every sign-in provider the deployment configures (FR-ADM-08), in its order: whether it is on, the email domains it " +
			"accepts, its sessions in the last 30 days, whether a Super admin signs in with it alone (then turning it off is refused), its " +
			"version for If-Match and a change waiting for the other Super admin. Providers are added in the deployment's configuration; " +
			"adding them here comes in P2 or P3. " + authz.ListSignInMethods.Note + " Read-only; no side effects.",
	}, append([]string{CodeCursor}, admin...), func(ctx context.Context, in *signInMethodListInput) (*signInMethodListOutput, error) {
		by, err := a.superAdmin(ctx, "Listing the sign-in providers")
		if err != nil {
			return nil, err
		}
		out := SignInMethodPage{Items: []SignInMethod{}}
		err = a.inInstallation(ctx, by, func(ctx context.Context, _ uuid.UUID, u storage.UnitOfWork) error {
			all, err := a.signInMethods(ctx, u)
			if err != nil {
				return err
			}
			out.Items, out.NextCursor, err = pageOf(all, func(m SignInMethod) string { return m.Provider }, in.PageQuery)
			return err
		})
		return &signInMethodListOutput{Body: out}, err
	})

	register(a, authz.ChangeSignInMethod, pendsChange(a, huma.Operation{
		OperationID: "changeSignInMethod", Method: http.MethodPatch, Path: "/v1/installation/sign-in-methods/{provider}", Tags: []string{"Session"},
		Summary: "Turn a sign-in provider on or off, or set its domains",
		Description: "Sets a provider's switches (FR-ADM-08): on or off (off refuses new sign-ins through it, and with end_sessions signs " +
			"out every session it started), and the email domains it accepts (an account's verified email on one of them; empty: any). " +
			"Refused when it leaves a Super admin no way to sign in. Needs an active Super admin who passed step-up in the last 5 minutes. " +
			"With two Super admins the answer is 202 with the change the other one approves with approveInstallationChange (FR-ADM-07); " +
			"with one it applies at once. Side effects: the switches, or the change request, their event in the installation's trail and a " +
			"security notice to every Super admin. Idempotent with Idempotency-Key; Prefer: return=preview checks it and changes nothing.",
	}, reflect.TypeFor[SignInMethod]()), append(append(append([]string{CodeInvalid, CodeIfMatchRequired, CodeStale, storage.CodeVersion}, admin...),
		writes...), SignInMethodCodes...),
		func(ctx context.Context, in *signInMethodChangeInput) (*connectorWriteOutput, error) {
			by, err := a.superAdmin(ctx, "Changing a sign-in provider")
			if err != nil {
				return nil, err
			}
			var name string
			if a.deps.SignIn != nil {
				for _, p := range a.deps.SignIn.Providers() {
					if p.ID == in.Provider {
						name = p.Name
					}
				}
			}
			if name == "" {
				return nil, problem(CodeNotFound, "No sign-in provider "+in.Provider+" is configured.", "List the sign-in providers to find their IDs")
			}
			domains := []string{}
			var bad []FieldProblem
			for i, x := range in.Body.AllowedDomains {
				d := strings.ToLower(strings.TrimSpace(string(x)))
				switch {
				case !domainName.MatchString(d):
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.allowed_domains[" + strconv.Itoa(i) + "]",
						Message: "is not a domain name", Fix: "Give the domain as people's email addresses end, such as example.com"})
				case !slices.Contains(domains, d):
					domains = append(domains, d)
				}
			}
			if len(bad) > 0 {
				return nil, invalid(bad)
			}
			slices.Sort(domains)
			before, err := a.signInMethodNow(ctx, in.Provider)
			if err != nil {
				return nil, err
			}
			if err := checkIfMatch(in.IfMatch, etag(before.Version)); err != nil {
				return nil, err
			}
			c := storage.SignInMethodChange{Provider: in.Provider, Version: before.Version, Enabled: in.Body.Enabled, AllowedDomains: domains,
				EndSessions: in.Body.EndSessions && !in.Body.Enabled}
			p, _ := authn.From(ctx)
			w, err := inInstallationWrite(ctx, a, by, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (any, error) {
				n, err := a.deps.Installation.CountSuperAdmins(ctx)
				if err != nil {
					return nil, err
				}
				if n >= 2 {
					summary := signInSummary(name, before, c)
					ch, err := connector.RequestChange(ctx, u, a.deps.Clock, by, p.StepUpAt, nil, signInChangeName(in.Provider), stepSignIn, summary, c)
					if err != nil {
						return nil, err
					}
					if err := a.relay(ctx, u, notify.Event{Type: "approval.requested", RecordType: "installation_change", RecordID: &ch.ID,
						Link: "/settings/sign-in-providers", Title: summary + ": a change of a sign-in provider waits for another Super admin's approval",
						Audience: notify.SuperAdmins(by)}); err != nil {
						return nil, err
					}
					return installationChangeOf(ch), nil
				}
				if err := a.applySignInMethod(ctx, u, by, nil, name, c); err != nil {
					return nil, err
				}
				all, err := a.signInMethods(ctx, u)
				if err != nil {
					return nil, err
				}
				for _, m := range all {
					if m.Provider == in.Provider {
						return m, nil
					}
				}
				return nil, problem(CodeNotFound, "No sign-in provider "+in.Provider+" is configured.", "List the sign-in providers to find their IDs")
			})
			if err != nil {
				return nil, err
			}
			out := &connectorWriteOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
			if _, ok := w.Body.(InstallationChange); ok {
				out.Status = http.StatusAccepted
			} else if m, ok := w.Body.(SignInMethod); ok && !w.Previewed {
				out.ETag = etag(m.Version)
			}
			return out, nil
		})
}
