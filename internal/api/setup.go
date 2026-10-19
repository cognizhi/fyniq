package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/setupcode"
)

// SetupStatus says whether the installation has been set up.
type SetupStatus struct {
	Done bool `json:"done" doc:"Whether first-run setup has been completed; the setup wizard shows only while it has not." example:"false"`
}

// NewOrganisation is the first organisation of an installation.
type NewOrganisation struct {
	Name         string `json:"name" minLength:"1" maxLength:"200" doc:"The organisation's name." example:"Northwind Analytics Pte. Ltd."`
	BaseCurrency string `json:"base_currency,omitempty" pattern:"^[A-Z]{3}$" default:"SGD" doc:"The ISO 4217 currency its books are kept in; SGD when left out (FR-ORG-02)." example:"SGD"`
}

// SetupRequest completes first-run setup.
type SetupRequest struct {
	SetupCode    string          `json:"setup_code" minLength:"20" maxLength:"100" doc:"The one-time code created at deployment (printed by migrate setup-code; Secret Manager in the cloud). Case, spaces and dashes do not matter." example:"ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z"`
	DisplayName  string          `json:"display_name" minLength:"1" maxLength:"200" doc:"Your name, as other users will see it." example:"Mei Ling Tan"`
	Organisation NewOrganisation `json:"organisation" doc:"The first organisation; you become its Owner."`
}

// SetupResult is what setup created.
type SetupResult struct {
	UserID                     string `json:"user_id" format:"uuid" doc:"Your user: the Super admin." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	OrganisationID             string `json:"organisation_id" format:"uuid" doc:"The first organisation, of which you are the Owner." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02"`
	InstallationOrganisationID string `json:"installation_organisation_id" format:"uuid" doc:"The installation's own audit trail." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d03"`
	RecoveryCodes
}

type setupStatusOutput struct{ Body SetupStatus }

type completeSetupInput struct{ Body SetupRequest }

type completeSetupOutput struct{ Body SetupResult }

func registerSetup(a *API) {
	register(a, authz.GetSetupStatus, huma.Operation{
		OperationID: "getSetupStatus", Method: http.MethodGet, Path: "/v1/setup", Tags: []string{"Installation"},
		Summary:     "Get the setup status",
		Description: "Says whether first-run setup has been completed (FR-ADM-06). Public: it needs no credential, so the web app can show the setup wizard on a new installation. Read-only; no side effects.",
		Security:    []map[string][]string{{}}, // no credential: an empty requirement
	}, []string{CodeUnavailable}, func(ctx context.Context, _ *struct{}) (*setupStatusOutput, error) {
		if a.deps.Installation == nil {
			return nil, problem(CodeUnavailable, "The installation's database is not configured.", "Configure the database, then try again.")
		}
		done, err := a.deps.Installation.SetupDone(ctx)
		return &setupStatusOutput{Body: SetupStatus{Done: done}}, err
	})
	register(a, authz.CompleteSetup, oneTimeSecret(huma.Operation{
		OperationID: "completeSetup", Method: http.MethodPost, Path: "/v1/setup", Tags: []string{"Installation"}, DefaultStatus: http.StatusCreated,
		Summary: "Complete first-run setup",
		Description: "Claims a new installation with the one-time setup code created at deployment (FR-ADM-06). You become the Super admin " +
			"and the Owner of the first organisation, and receive your ten recovery codes, shown once (open question 36); the installation's " +
			"own audit trail is created with it. Side effects: your user and sign-in identity, the Super admin and their recovery codes, two " +
			"organisations, your membership and role, audit events. Works once: a wrong or used " +
			"code changes nothing. Needs a person signed in to Fyniq; an OAuth client or the AI is refused. It takes no Idempotency-Key: " +
			"the code itself makes it one-time.",
	}), []string{CodeSetupRefused, CodeSessionRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeUnavailable, storage.CodeAmount},
		func(ctx context.Context, in *completeSetupInput) (*completeSetupOutput, error) {
			if a.deps.Installation == nil {
				return nil, problem(CodeUnavailable, "The installation's database is not configured.", "Configure the database, then try again.")
			}
			p, _ := authn.From(ctx)
			if p.Via != authn.ViaSession || p.AI() || p.Issuer == "" || p.Subject == "" {
				return nil, problem(CodeSessionRequired, "First-run setup needs a person signed in to Fyniq, not an OAuth client or the AI.",
					"Sign in to Fyniq in the browser and complete the setup there.")
			}
			codes, hashes := setupcode.NewSet(setupcode.RecoveryCodes)
			base := in.Body.Organisation.BaseCurrency
			if base == "" {
				base = "SGD" // FR-ORG-02: the base currency defaults to SGD
			}
			res, err := a.deps.Installation.CompleteSetup(ctx, storage.NewSetup{CodeHash: setupcode.Hash(in.Body.SetupCode),
				DisplayName: in.Body.DisplayName, Issuer: p.Issuer, Subject: p.Subject,
				OrganisationName: in.Body.Organisation.Name, BaseCurrency: base,
				RecoveryCodeHashes: hashes, Now: a.deps.Clock.Now()})
			if storage.CodeOf(err) == storage.CodeSetup {
				a.deps.Log.Warn("api.setup.refused", "request_id", requestID(ctx)) // never the code
				return nil, problem(CodeSetupRefused, "The setup code is not valid, or this installation is set up already.",
					"Check the code printed at deployment. If setup is done, sign in instead.")
			}
			if err != nil {
				return nil, err
			}
			return &completeSetupOutput{Body: SetupResult{UserID: res.UserID.String(), OrganisationID: res.OrganisationID.String(),
				InstallationOrganisationID: res.InstallationOrganisationID.String(), RecoveryCodes: recoveryCodes(codes)}}, nil
		})
}
