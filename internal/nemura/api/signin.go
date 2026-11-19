package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

type signInOutput struct {
	Body struct {
		Providers []Provider `json:"providers" doc:"The providers people may sign in with, in the order to offer them."`
		SetupDone bool       `json:"setup_done" doc:"False until first-run setup has made the first organisation: the sign-in page then says Nemura is not set up." example:"true"`
	}
}

func (a *API) registerSignIn() {
	huma.Register(a.API, huma.Operation{
		OperationID: "getSignIn", Method: "GET", Path: "/v1/sign-in", Tags: []string{"Sign-in"}, Summary: "What the sign-in page shows",
		Description: "Lists the sign-in providers people may use, in order, and whether first-run setup is done. It needs no sign-in, shows nothing of any organisation and changes nothing; a browser signs in by going to /auth/signin with a provider's ID.",
	}, func(ctx context.Context, _ *struct{}) (*signInOutput, error) {
		out := &signInOutput{}
		out.Body.Providers = []Provider{}
		if a.d.SignIn == nil {
			return out, nil
		}
		out.Body.Providers = append(out.Body.Providers, a.d.SignIn.Providers()...)
		done, err := a.d.SignIn.SetupDone(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("Whether setup is done could not be read.")
		}
		out.Body.SetupDone = done
		return out, nil
	})
}
