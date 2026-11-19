package api

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/features"
)

const capFeatures = "Features, packs, policies"

// Feature is one optional feature as the screen shows it.
type Feature struct {
	ID             string   `json:"id" doc:"The feature's key." example:"leave"`
	Name           string   `json:"name" doc:"As the screen names it." example:"Leave"`
	Phase          string   `json:"phase" enum:"P1,P2,P3" doc:"When it is built; a feature of a later phase cannot be switched on yet." example:"P1"`
	State          string   `json:"state" enum:"on,off,external" doc:"On, off, or held by another system." example:"on"`
	ExternalSystem string   `json:"external_system,omitempty" doc:"The system that holds its data, when external." example:"Acme Payroll"`
	Needs          []string `json:"needs" doc:"The features that must be on, or external, for this one to be on."`
}

// Features reads and sets the organisation's feature switches (the service does the dependency rules, the outstanding work and the audit).
type Features interface {
	List(ctx context.Context, s Session) (features.Switches, error)
	Set(ctx context.Context, s Session, id, state, system string, given []features.Arrangement) error
}

func init() {
}

type featuresInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
}

type featuresOutput struct {
	Body struct {
		Features []Feature `json:"features" doc:"Every optional feature, in the order the screen shows them. People records is always on and is not listed."`
	}
}

type setFeatureInput struct {
	Cookie  string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Feature string `path:"feature" doc:"The feature's key." example:"leave"`
	Body    struct {
		State          string `json:"state" enum:"on,off,external" doc:"The state to set." example:"on"`
		ExternalSystem string `json:"external_system,omitempty" maxLength:"100" doc:"The system that holds the data: when the state is external, and only then." example:"Acme Payroll"`
		Arrangements   []struct {
			What        string `json:"what" doc:"The outstanding thing, as the refusal named it." example:"leave requests waiting for approval"`
			Arrangement string `json:"arrangement" maxLength:"500" doc:"How it will be handled instead." example:"Approved in the old system until 31 Jan"`
		} `json:"arrangements,omitempty" doc:"A replacement arrangement for each outstanding thing, when the feature is switched off or handed to another system."`
	}
}

func featureError(err error) error {
	var r *features.Refusal
	switch {
	case errors.As(err, &r):
		return Err(r.Code, r.Message)
	}
	return huma.Error500InternalServerError("The feature could not be changed.")
}

// rightsTo reports whether the person holds a role with a right on capability.
func (a *API) featureRights(ctx context.Context, s Session, rights ...authz.Right) (bool, error) {
	for _, r := range rights {
		ok, err := a.rights(ctx, s, capFeatures, r)
		if err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

func (a *API) registerFeatures() {
	huma.Register(a.API, huma.Operation{
		OperationID: "listFeatures", Method: "GET", Path: "/v1/features", Tags: []string{"Features"}, Summary: "The feature switches",
		Description: "Lists each optional feature and its state: on, off, or external (another system holds its data), with what it needs. Anyone who may see the settings (HR officer, HR administrator, Payroll administrator, Administrator, Owner). It changes nothing.",
		Errors:      []int{401, 403},
	}, func(ctx context.Context, in *featuresInput) (*featuresOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		ok, err := a.featureRights(ctx, s, authz.View, authz.Create, authz.Approve)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, Err(CodeRoleRequired, "Only the people team and the Owner see the feature switches.")
		}
		cur, err := a.d.Features.List(ctx, s)
		if err != nil {
			return nil, featureError(err)
		}
		out := &featuresOutput{}
		out.Body.Features = []Feature{}
		for _, f := range features.Registry {
			st := cur[f.ID].State
			if st == "" {
				st = features.Off
			}
			needs := f.Needs
			if needs == nil {
				needs = []string{}
			}
			out.Body.Features = append(out.Body.Features, Feature{ID: f.ID, Name: f.Name, Phase: f.Phase, State: st, ExternalSystem: cur[f.ID].System, Needs: needs})
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setFeature", Method: "PUT", Path: "/v1/features/{feature}", Tags: []string{"Features"}, Summary: "Switch a feature",
		Description: "Sets a feature on, off or external. A feature another needs cannot be switched off, and one that needs another cannot be switched on without it (NM_FEATURE_REQUIRED); a feature of a later phase and Payroll, which needs the Owner and a second approver, cannot be switched on yet (NM_FEATURE_UNAVAILABLE); switching off with work still open needs a replacement arrangement for each (NM_FEATURE_OBLIGATIONS_OPEN). Switching off keeps the records. The HR administrator or the Owner. It is audited.",
		Errors:      []int{401, 403, 409, 422},
	}, func(ctx context.Context, in *setFeatureInput) (*struct{}, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		ok, err := a.featureRights(ctx, s, authz.Create, authz.Approve)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, Err(CodeRoleRequired, "Only the HR administrator and the Owner switch features.")
		}
		var given []features.Arrangement
		for _, g := range in.Body.Arrangements {
			given = append(given, features.Arrangement{What: g.What, Arrangement: g.Arrangement})
		}
		if err := a.d.Features.Set(ctx, s, in.Feature, in.Body.State, in.Body.ExternalSystem, given); err != nil {
			return nil, featureError(err)
		}
		return nil, nil
	})
}
