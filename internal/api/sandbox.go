package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/seed"
	"github.com/cognizhi/fyniq/internal/seedload"
)

// ------------------------------------------------------------------ the sandbox organisation (FR-INT-18)
//
// The owner's decision of 21 Nov 2026: the sandbox is a flagged organisation of the same installation, enabled per deployment. It holds only the
// synthetic sample company, has API clients of its own (made as in any organisation), and sends nothing outside Fyniq: the connector runtime, the
// outbox and the webhook sender refuse or stub every outbound path for it (internal/connector, internal/outbox, internal/webhooks), and the API
// refuses to install a connector, to open a practice organisation or to register a webhook to a host that is not on the deployment's allow-list.
// Only a Super admin makes one, with two Super admins as the other's approved installation change (step sandbox), as a private endpoint is
// registered. Nothing turns a real organisation into a sandbox one or back: the database's registry takes a row only from the function that makes
// the organisation (migration 001250).

// SandboxConfig is the deployment's sandbox setting: off unless the deployment's operator turns it on.
type SandboxConfig struct {
	// Enabled lets a Super admin make sandbox organisations here.
	Enabled bool
	// WebhookHosts are the only hosts a sandbox organisation's webhooks may reach, exact host names (https, no private address), empty by default.
	WebhookHosts []string
}

// AllowsWebhookHost says whether a sandbox organisation's webhook may go to host.
func (c SandboxConfig) AllowsWebhookHost(host string) bool {
	return slices.Contains(c.WebhookHosts, strings.ToLower(strings.TrimSuffix(host, ".")))
}

// stepSandbox is the installation change's step for making a sandbox organisation.
const stepSandbox = "sandbox"

// sandboxChangeName is the name an installation change gives a sandbox organisation.
func sandboxChangeName(id uuid.UUID) string { return "sandbox:" + id.String() }

// sandboxChange is what an approval of a sandbox organisation executes.
type sandboxChange struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// SandboxOrganisation is the sandbox organisation made.
type SandboxOrganisation struct {
	ID           string `json:"id" format:"uuid" doc:"The sandbox organisation's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5b02"`
	Name         string `json:"name" doc:"Its name, always labelled \"Sandbox: \"." example:"Sandbox: Integration tests"`
	BaseCurrency string `json:"base_currency" doc:"Its books' currency." example:"SGD"`
	Sandbox      bool   `json:"sandbox" doc:"Always true: this organisation holds only the synthetic sample company and sends nothing outside Fyniq." example:"true"`
}

// NewSandboxOrganisation is what to call the sandbox organisation.
type NewSandboxOrganisation struct {
	Name string `json:"name" minLength:"1" maxLength:"150" doc:"A name after the label \"Sandbox: \"." example:"Integration tests"`
}

type createSandboxInput struct {
	KeyHeader
	PreferHeader
	Body NewSandboxOrganisation
}

type sandboxLoadInput struct {
	ID string `path:"organisation_id" format:"uuid" doc:"The sandbox organisation's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5b02"`
}

type sandboxLoadOutput struct{ Body SandboxSampleCompany }

// SandboxSampleCompany says what the sandbox organisation's sample company holds.
type SandboxSampleCompany struct {
	Accounts int `json:"accounts" doc:"Accounts in its chart." example:"60"`
	Periods  int `json:"periods" doc:"Its accounting periods (2026)." example:"12"`
	Journals int `json:"journals" doc:"Journals posted." example:"24"`
}

// refuseSandbox answers a request an organisation that is a sandbox one must not make: it sends nothing outside Fyniq.
func refuseSandbox(what string) error {
	return problem(CodeSandboxRefused, what+" is refused in a sandbox organisation: it holds only the sample company and sends nothing outside Fyniq.",
		"Do it in the organisation that keeps your books; a sandbox is for testing against the API")
}

// inSandbox says whether the caller's organisation is a sandbox one, read in the unit of work.
func inSandbox(ctx context.Context, u storage.UnitOfWork) (bool, error) { return u.Sandboxes().Is(ctx) }

// sandboxWebhookHost refuses a webhook URL of a sandbox organisation whose host is not on the deployment's allow-list (empty by default).
func (a *API) sandboxWebhookHost(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || !a.deps.Sandbox.AllowsWebhookHost(u.Hostname()) {
		return problem(CodeSandboxRefused, "A webhook of a sandbox organisation may reach only the hosts the deployment's operator allows, and this one is not among them.",
			"Ask the operator to allow the host, or test with a webhook receiver on the allowed list")
	}
	return nil
}

func (a *API) sandboxSample() (seed.Company, error) {
	v, err := seed.Embedded()
	if err != nil {
		return seed.Company{}, err
	}
	return seed.Generate(v, 1, 24), nil
}

// loadSandbox loads the sample company into the sandbox organisation, through the posting engine as the seed does. It is run after the
// organisation's transaction has committed, so a failure leaves an organisation with no chart that the load operation fills.
func (a *API) loadSandbox(ctx context.Context, org uuid.UUID) (SandboxSampleCompany, error) {
	c, err := a.sandboxSample()
	if err != nil {
		return SandboxSampleCompany{}, err
	}
	r, err := seedload.Load(ctx, a.deps.Store, org, c)
	if err != nil {
		a.deps.Log.Error("api.sandbox.load_failed", "organisation", org.String(), "request_id", requestID(ctx), "error", err.Error())
		return SandboxSampleCompany{}, problem(CodeSandboxLoad, "The sandbox organisation was made, but its sample company is not loaded.",
			"Load it again with loadSandboxSampleCompany")
	}
	return SandboxSampleCompany{Accounts: r.Accounts + r.Standard, Periods: r.Periods, Journals: r.Journals}, nil
}

// applySandbox makes the organisation c by by, as approved by change (nil: none needed), and tells the Super admins.
func (a *API) applySandbox(ctx context.Context, u storage.UnitOfWork, by uuid.UUID, change *uuid.UUID, c sandboxChange) (SandboxOrganisation, error) {
	made, err := u.Sandboxes().Create(ctx, storage.NewSandbox{By: by, Change: change, ID: c.ID, Name: c.Name, At: a.deps.Clock.Now()})
	if err != nil {
		return SandboxOrganisation{}, err
	}
	n, err := a.deps.Installation.CountSuperAdmins(ctx)
	if err != nil {
		return SandboxOrganisation{}, err
	}
	title := "The sandbox organisation " + c.Name + " was made"
	if n <= 1 {
		title += " at once, since there is one Super admin"
	}
	return SandboxOrganisation{ID: made.OrganisationID.String(), Name: c.Name, BaseCurrency: "SGD", Sandbox: true},
		a.relay(ctx, u, notify.Event{Type: notify.SandboxType, RecordType: "organisation", RecordID: &made.OrganisationID,
			Title: title + ". If it was not you, tell the other Super admin and check the installation's trail.", Audience: notify.SuperAdmins()})
}

// approveSandboxChange approves pending installation change p, a sandbox organisation, and makes it in the same unit of work.
func (a *API) approveSandboxChange(ctx context.Context, u storage.UnitOfWork, by uuid.UUID, mfaAt time.Time, p storage.InstallationChange, hash []byte) (any, error) {
	var c sandboxChange
	if err := json.Unmarshal(p.Change, &c); err != nil {
		return nil, err
	}
	if _, err := u.Connectors().DecideInstallationChange(ctx, by, p.ID, hash, true, mfaAt, "", a.deps.Clock.Now()); err != nil {
		return nil, err
	}
	id := p.ID
	return a.applySandbox(ctx, u, by, &id, c)
}

func registerSandbox(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType}, locks...)
	admin := append([]string{CodeNotSuperAdmin, CodeStepUpRequired, CodeSessionRequired, CodeUnavailable}, locks...)

	register(a, authz.CreateSandboxOrganisation, pendsChange(a, huma.Operation{
		OperationID: "createSandboxOrganisation", Method: http.MethodPost, Path: "/v1/installation/sandbox-organisations", Tags: []string{"Installation"},
		DefaultStatus: http.StatusCreated, Summary: "Make a sandbox organisation",
		Description: "Makes a sandbox organisation (FR-INT-18): a new organisation of this installation, labelled \"Sandbox: \", flagged for good, holding only the " +
			"synthetic sample company, where an integrator tests the API with clients of its own. It sends nothing outside Fyniq: no email, payment provider, bank, " +
			"InvoiceNow, AI model or webhook to a host the deployment's operator has not allowed. The caller becomes its Owner. Only when the deployment enables " +
			"sandboxes (otherwise API-SANDBOX-01); no organisation becomes a sandbox or stops being one. Needs an active Super admin who passed step-up in the " +
			"last 5 minutes. With two Super admins the answer is 202 with the change the other one approves with approveInstallationChange (FR-ADM-07); with one " +
			"it is made at once. Side effects: the organisation, its membership and Owner role, the sample company's accounts, periods and journals, audit " +
			"events in both trails and a security notice to every Super admin. Idempotent with Idempotency-Key; Prefer: return=preview checks it and makes nothing.",
	}, reflect.TypeFor[SandboxOrganisation]()), append(append([]string{CodeInvalid, CodeSandboxOff, CodeSandboxLoad, storage.CodeSandbox}, admin...), writes...),
		func(ctx context.Context, in *createSandboxInput) (*connectorWriteOutput, error) {
			by, err := a.superAdmin(ctx, "Making a sandbox organisation")
			if err != nil {
				return nil, err
			}
			if !a.deps.Sandbox.Enabled {
				return nil, problem(CodeSandboxOff, "Sandbox organisations are not enabled on this deployment.", "Ask the operator who runs this deployment to enable them")
			}
			name := "Sandbox: " + strings.TrimSpace(in.Body.Name)
			if strings.TrimSpace(in.Body.Name) == "" || utf8.RuneCountInString(name) > 200 {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.name", Message: "The name must be 1 to 150 characters.", Fix: "Give the sandbox a name of 1 to 150 characters"}})
			}
			p, _ := authn.From(ctx)
			c := sandboxChange{ID: uuid.Must(uuid.NewV7()), Name: name}
			w, err := inInstallationWrite(ctx, a, by, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (any, error) {
				n, err := a.deps.Installation.CountSuperAdmins(ctx)
				if err != nil {
					return nil, err
				}
				if n >= 2 {
					summary := "Make the sandbox organisation " + name
					ch, err := connector.RequestChange(ctx, u, a.deps.Clock, by, p.StepUpAt, nil, sandboxChangeName(c.ID), stepSandbox, summary, c)
					if err != nil {
						return nil, err
					}
					if err := a.relay(ctx, u, notify.Event{Type: "approval.requested", RecordType: "installation_change", RecordID: &ch.ID,
						Title: summary + ": a sandbox organisation waits for another Super admin's approval", Audience: notify.SuperAdmins(by)}); err != nil {
						return nil, err
					}
					return installationChangeOf(ch), nil
				}
				return a.applySandbox(ctx, u, by, nil, c)
			})
			if err != nil {
				return nil, err
			}
			out := &connectorWriteOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
			if _, ok := w.Body.(InstallationChange); ok {
				out.Status = http.StatusAccepted
			} else if made, ok := w.Body.(SandboxOrganisation); ok && !w.Previewed && !w.Replayed {
				if _, err := a.loadSandbox(ctx, uuid.MustParse(made.ID)); err != nil {
					return nil, err
				}
			}
			return out, nil
		})

	register(a, authz.LoadSandboxSampleCompany, exempt(exempt(huma.Operation{
		OperationID: "loadSandboxSampleCompany", Method: http.MethodPost, Path: "/v1/installation/sandbox-organisations/{organisation_id}/sample-company", Tags: []string{"Installation"},
		Summary: "Load the sample company into a sandbox organisation",
		Description: "Loads the synthetic sample company (FR-UX-10, DEV-04) through the posting engine into a sandbox organisation that has no chart yet: for one made " +
			"while the load failed. Refused (API-SANDBOX-02) for an organisation that is not a sandbox one or already has books. " + authz.LoadSandboxSampleCompany.Note +
			" Side effects: the accounts, periods and journals and their audit events. Repeating it is refused once the books are there.",
	}, "Prefer", "Loading the sample company is refused once there are books, so a preview would only repeat the refusal (FR-INT-14)."),
		"Idempotency-Key", "A repeat is refused once the sample company is loaded, so it never loads twice (FR-INT-08)."), append(append([]string{CodeSandboxRefused, CodeSandboxLoad, CodeNotFound}, admin...), writes...), func(ctx context.Context, in *sandboxLoadInput) (*sandboxLoadOutput, error) {
		by, err := a.superAdmin(ctx, "Loading a sandbox organisation's sample company")
		if err != nil {
			return nil, err
		}
		id, err := parseID("organisation_id", in.ID)
		if err != nil {
			return nil, err
		}
		var sandbox, empty bool
		if err := a.deps.Store.Do(actingAs(ctx, by), id, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			if sandbox, err = inSandbox(ctx, u); err != nil {
				return err
			}
			accounts, err := u.Accounts().List(ctx)
			empty = len(accounts) == 0
			return err
		}); err != nil {
			return nil, err
		}
		if !sandbox || !empty {
			return nil, problem(CodeSandboxRefused, "Only a sandbox organisation that has no chart yet takes the sample company.", "List the organisations you work in")
		}
		r, err := a.loadSandbox(ctx, id)
		return &sandboxLoadOutput{Body: r}, err
	})
}
