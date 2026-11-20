package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// The careers page (PRD-002 NM-REC-02): an organisation's public list of its published job posts under an address it chose. Anyone reads it with no sign-in; it names the company, the posts' titles, teams and
// words and the day each went up, and nothing else: no salary, no person, no requisition. The Recruiter, the HR administrator and the Owner choose the address and switch the page on.

// CareersPost is a published post on the careers page.
type CareersPost struct {
	ID          string `json:"id" format:"uuid" doc:"The post." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3b01"`
	Title       string `json:"title" doc:"The post's title." example:"Data scientist"`
	Department  string `json:"department" doc:"The team." example:"Engineering"`
	Body        string `json:"body" doc:"The post's text." example:"You will analyse data and build models."`
	PublishedOn string `json:"published_on" format:"date" doc:"The day it was published." example:"2026-10-05"`
}

// CareersView is what a visitor reads.
type CareersView struct {
	Found   bool          `json:"found" doc:"False for an address that is unknown or whose page is switched off, alike." example:"true"`
	Company string        `json:"company,omitempty" doc:"The company's name." example:"Acme Pte. Ltd."`
	Intro   string        `json:"intro,omitempty" doc:"The words above the list." example:"We are a small team building tools for accountants."`
	Posts   []CareersPost `json:"posts" nullable:"false" doc:"The published posts, the newest first."`
}

// CareersSettings is the organisation's careers page.
type CareersSettings struct {
	Configured bool   `json:"configured" doc:"False until an address was chosen." example:"true"`
	Slug       string `json:"slug,omitempty" doc:"The address: the page is at /careers/<slug>." example:"acme-careers"`
	Enabled    bool   `json:"enabled" doc:"Whether anyone can read the page." example:"true"`
	Company    string `json:"company,omitempty" doc:"The company's name shown on the page." example:"Acme Pte. Ltd."`
	Intro      string `json:"intro,omitempty" doc:"The words above the list." example:"We are a small team building tools for accountants."`
}

// Careers is the careers page behind the API.
type Careers interface {
	Public(ctx context.Context, slug string) (CareersView, error)
	Settings(ctx context.Context, s Session) (CareersSettings, error)
	Save(ctx context.Context, s Session, in CareersSettings) (CareersSettings, error)
	Allow(ctx context.Context, scope, key string, window time.Duration, limit int) (bool, error)
}

type careersInput struct {
	Slug string `path:"slug" minLength:"3" maxLength:"40" pattern:"^[a-z0-9-]+$" doc:"The company's careers address." example:"acme-careers"`
}
type careersOutput struct{ Body CareersView }
type careersSettingsOutput struct{ Body CareersSettings }
type careersSaveInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		Slug    string `json:"slug" minLength:"3" maxLength:"40" doc:"3 to 40 lower-case letters, digits and single hyphens." example:"acme-careers"`
		Enabled bool   `json:"enabled" doc:"Whether anyone can read the page." example:"true"`
		Company string `json:"company" minLength:"1" maxLength:"120" doc:"The company's name shown on the page." example:"Acme Pte. Ltd."`
		Intro   string `json:"intro,omitempty" maxLength:"1000" doc:"The words above the list." example:"We are a small team building tools for accountants."`
	}
}

const rateCareers = 600 // reads of any careers page in a minute, in all, counted across every API instance

func (a *API) registerCareers() {
	huma.Register(a.API, huma.Operation{
		OperationID: "readCareersPage", Method: http.MethodGet, Path: "/v1/careers/{slug}", Tags: []string{"Hiring"}, Summary: "Read a company's careers page",
		Description: "PRD-002 NM-REC-02. With no sign-in: the company's name, its words and each published, approved job post that is open (title, team, text, day published). No salary and no person are shown. An address that is unknown or whose page is " +
			"switched off answers found false, alike. At most 600 reads a minute in all, counted across every API instance (NM_RATE_LIMITED). It changes nothing and is on the never-list for the AI and MCP surfaces.", Errors: []int{422, 429, 503},
	}, func(ctx context.Context, in *careersInput) (*careersOutput, error) {
		if a.d.Careers == nil {
			return nil, Err(CodeUnavailable, "Careers pages are not set up in this deployment.")
		}
		ok, err := a.d.Careers.Allow(ctx, "careers-public", "all", time.Minute, rateCareers)
		if err != nil {
			return nil, Err(CodeUnavailable, "The careers page could not be read just now. Try again in a while.")
		}
		if !ok {
			return nil, Err("NM_RATE_LIMITED", "Too many reads just now. Wait a minute and try again.")
		}
		v, err := a.d.Careers.Public(ctx, in.Slug)
		if err != nil {
			return nil, huma.Error500InternalServerError("The careers page could not be read.")
		}
		if v.Posts == nil {
			v.Posts = []CareersPost{}
		}
		return &careersOutput{Body: v}, nil
	})
}
