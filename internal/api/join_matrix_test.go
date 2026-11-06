//go:build local

package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// matrixJoinRequest returns a new pending request to join the organisation in Standard mode by the domain matrix.test:
// the provider "matrix" lets people join by it and the organisation claimed it (set up once, as the migrator, as a Super
// admin and an Owner would), and a new person signs in through it with a verified email there and asks. Not e's own
// organisation: a request approved there would end its founder bootstrap, and the member rows after it would need an
// approval.
func matrixJoinRequest(e *env) string {
	e.t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, db.URL("fyniq_migrator"))
	if err != nil {
		e.t.Fatal(err)
	}
	defer c.Close(ctx)
	user := matrixApprover(e)
	if _, err := c.Exec(ctx, `INSERT INTO fyniq.sign_in_method (provider, enabled, allowed_domains, join_by_domain, changed_by, changed_at)
		VALUES ('matrix', true, '{matrix.test}', true, $1, now()) ON CONFLICT (provider) DO NOTHING`, user); err != nil {
		e.t.Fatal(err)
	}
	if _, err := c.Exec(ctx, `INSERT INTO fyniq.join_domain (domain, claiming_organisation, claimed_by, claimed_at)
		VALUES ('matrix.test', $1, $2, now()) ON CONFLICT (domain) DO NOTHING`, inStandardMode(e).books.Org, user); err != nil {
		e.t.Fatal(err)
	}
	token := uuid.NewString()
	hash := authn.HashToken(token)
	now := e.clock.Now()
	if err := e.store.StartSession(ctx, storage.NewSession{TokenHash: hash, Issuer: "urn:fyniq:matrix", Subject: "applicant-" + token[:8],
		Provider: "matrix", Now: now, Expires: now.Add(authn.SessionLifetime), StepUpAt: now}); err != nil {
		e.t.Fatal(err)
	}
	if err := e.store.RecordSessionEmail(ctx, hash, "applicant-"+token[:8]+"@matrix.test"); err != nil {
		e.t.Fatal(err)
	}
	r, err := e.store.RequestToJoin(ctx, hash, "Matrix applicant", now)
	if err != nil {
		e.t.Fatal(err)
	}
	return r.ID.String()
}

// matrixJoin are the authorisation matrix's rows for joining by email domain (FR-ADM-08): the person's own two need a
// browser session without an account, which the matrix's principals lack, so an allowed role is refused past its policy
// (API-AUTHZ-04); TestJoinByDomain runs them. An Owner's decisions run on real requests, in the organisation in Standard mode.
var matrixJoin = map[string]func(e *env, tok string) (response, func() bool){
	"getJoinOffer": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/session/join-offer", tok, nil), nil
	},
	"requestToJoin": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/session/join-requests", tok, map[string]any{"display_name": "Matrix"}), nil
	},
	"listJoinRequests": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/members/join-requests", tok, nil), nil
	},
	"approveJoinRequest": func(e *env, tok string) (response, func() bool) {
		return inStandardMode(e).call(http.MethodPost, "/v1/members/join-requests/"+matrixJoinRequest(e)+"/approve", e.asIn(tok, e.standardBo()), nil), nil
	},
	"rejectJoinRequest": func(e *env, tok string) (response, func() bool) {
		return inStandardMode(e).call(http.MethodPost, "/v1/members/join-requests/"+matrixJoinRequest(e)+"/reject", e.asIn(tok, e.standardBo()), nil), nil
	},
	"listJoinDomains": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/organisation/join-domains", tok, nil), nil
	},
	"claimJoinDomain": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPut, "/v1/organisation/join-domains/claim-"+uuid.NewString()[:8]+".test", steppedUp(e, tok), nil), nil
	},
	"releaseJoinDomain": func(e *env, tok string) (response, func() bool) {
		d := "release-" + uuid.NewString()[:8] + ".test"
		e.call(http.MethodPut, "/v1/organisation/join-domains/"+d, steppedUp(e, e.as(authz.Owner)), nil)
		return e.call(http.MethodDelete, "/v1/organisation/join-domains/"+d, steppedUp(e, tok), nil), nil
	},
}
