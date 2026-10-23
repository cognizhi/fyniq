package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ email-in addresses (FR-DOC-12, FR-INT-22; rev 0.105)

// The email-in address codes.
const (
	CodeCaptureName     = "EML-05" // not a name an address can have
	CodeCaptureReserved = "EML-06" // a name kept for the mail system's own use
	CodeCaptureTaken    = "EML-07" // another organisation has the name
	CodeCaptureNoUser   = "EML-08" // the caller is not a person with an address of their own
)

// captureName is an address's name: 3 to 40 lower-case letters, digits, dots and hyphens, starting with a letter and
// ending with a letter or digit, no two dots together.
var captureName = regexp.MustCompile(`^[a-z][a-z0-9.-]{1,38}[a-z0-9]$`)

// reservedNames are the role mailboxes of RFC 2142 and the mail system's own, which no address takes.
var reservedNames = []string{"abuse", "admin", "administrator", "ftp", "hostmaster", "info", "mailer-daemon", "marketing", "news", "noc",
	"no-reply", "noreply", "postmaster", "root", "sales", "security", "support", "usenet", "uucp", "webmaster", "www"}

// CaptureAddress is an email-in address: mail sent to it is captured as documents for someone to confirm.
type CaptureAddress struct {
	Address   string    `json:"address" doc:"The whole address, to give to suppliers or to forward bills and receipts to." example:"bills@in.example.com"`
	Name      string    `json:"name" doc:"Its name, before the @." example:"bills"`
	CreatedAt time.Time `json:"created_at" doc:"When it was made." example:"2026-10-23T09:00:00Z"`
}

// CaptureAddresses are the email-in addresses the caller can see: the organisation's and their own.
type CaptureAddresses struct {
	Domain           string          `json:"domain,omitempty" doc:"The deployment's capture domain; absent when email-in is not set up." example:"in.example.com"`
	Organisation     *CaptureAddress `json:"organisation,omitempty" doc:"The organisation's address; absent until an Administrator makes one."`
	OrganisationETag string          `json:"organisation_etag" doc:"Send it in If-Match to change the organisation's address." example:"\"none\""`
	Mine             *CaptureAddress `json:"mine,omitempty" doc:"The caller's own address; absent until they make one."`
	MineETag         string          `json:"mine_etag" doc:"Send it in If-Match to change your own address." example:"\"none\""`
}

// CaptureAddressChange names an address, or leaves the name out to have none.
type CaptureAddressChange struct {
	Name string `json:"name,omitempty" maxLength:"40" doc:"The name before the @: 3 to 40 lower-case letters, digits, dots and hyphens. Leave it out to retire the address." example:"bills"`
}

type captureAddressInput struct {
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body CaptureAddressChange
}

type captureAddressesOutput struct{ Body CaptureAddresses }

type captureAddressOutput struct {
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     CaptureAddresses
}

func captureETag(c *storage.CaptureAddress) string {
	if c == nil {
		return etag("none")
	}
	return etag(c.ID)
}

// captureAddresses are the organisation's live address and user's (uuid.Nil: none of their own).
func (a *API) captureAddresses(ctx context.Context, u storage.UnitOfWork, user uuid.UUID) (CaptureAddresses, error) {
	live, err := u.CaptureAddresses().Live(ctx)
	if err != nil {
		return CaptureAddresses{}, err
	}
	out := CaptureAddresses{Domain: a.deps.Mail.CaptureDomain, OrganisationETag: etag("none"), MineETag: etag("none")}
	view := func(c storage.CaptureAddress) *CaptureAddress {
		return &CaptureAddress{Address: c.LocalPart + "@" + a.deps.Mail.CaptureDomain, Name: c.LocalPart, CreatedAt: c.CreatedAt.UTC()}
	}
	for i := range live {
		switch c := &live[i]; {
		case c.UserID == nil:
			out.Organisation, out.OrganisationETag = view(*c), captureETag(c)
		case user != uuid.Nil && *c.UserID == user:
			out.Mine, out.MineETag = view(*c), captureETag(c)
		}
	}
	return out, nil
}

// liveOf is the live address of user, or of the organisation when user is nil.
func liveOf(live []storage.CaptureAddress, user *uuid.UUID) *storage.CaptureAddress {
	i := slices.IndexFunc(live, func(c storage.CaptureAddress) bool {
		return (user == nil && c.UserID == nil) || (user != nil && c.UserID != nil && *c.UserID == *user)
	})
	if i < 0 {
		return nil
	}
	return &live[i]
}

// checkCaptureName reads a name: lower case, its form, and not a reserved one.
func checkCaptureName(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if !captureName.MatchString(name) || strings.Contains(name, "..") {
		return "", problem(CodeCaptureName, fmt.Sprintf("%q cannot be an address's name.", raw),
			"Use 3 to 40 lower-case letters, digits, dots and hyphens, starting with a letter, such as bills or mei.ling",
			FieldProblem{CodeCaptureName, "body.name", "not an address's name", "Use letters, digits, dots and hyphens, such as bills"})
	}
	if slices.Contains(reservedNames, name) {
		return "", problem(CodeCaptureReserved, fmt.Sprintf("%s is kept for the mail system's own use.", name), "Choose another name, such as bills or invoices",
			FieldProblem{CodeCaptureReserved, "body.name", "a reserved name", "Choose another name"})
	}
	return name, nil
}

// setCaptureAddress makes, replaces or retires the live address of user (nil: the organisation's) over ifMatch.
func (a *API) setCaptureAddress(ctx context.Context, in *captureAddressInput, user *uuid.UUID, caller uuid.UUID) (*captureAddressOutput, error) {
	if a.deps.Mail.CaptureDomain == "" {
		return nil, problem(CodeUnavailable, "Email-in is not set up for this deployment.", "Ask the deployment's operators to set its capture domain")
	}
	w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (CaptureAddresses, error) {
		live, err := u.CaptureAddresses().Live(ctx)
		if err != nil {
			return CaptureAddresses{}, err
		}
		current := liveOf(live, user)
		if err := checkIfMatch(in.IfMatch, captureETag(current)); err != nil {
			return CaptureAddresses{}, err
		}
		switch {
		case in.Body.Name == "" && current != nil:
			if _, err := u.CaptureAddresses().Retire(ctx, current.ID); err != nil {
				return CaptureAddresses{}, err
			}
		case in.Body.Name != "":
			name, err := checkCaptureName(in.Body.Name)
			if err != nil {
				return CaptureAddresses{}, err
			}
			if current == nil || current.LocalPart != name {
				_, err = u.CaptureAddresses().Create(ctx, name, user)
				if storage.CodeOf(err) == storage.CodeCaptureName {
					return CaptureAddresses{}, problem(CodeCaptureTaken, fmt.Sprintf("%s@%s belongs to another organisation.", name, a.deps.Mail.CaptureDomain),
						"Choose another name: a name stays with its organisation for good, so mail meant for one never reaches another",
						FieldProblem{CodeCaptureTaken, "body.name", "taken", "Choose another name"})
				}
				if err != nil {
					return CaptureAddresses{}, err
				}
			}
		}
		return a.captureAddresses(ctx, u, caller)
	})
	if err != nil {
		return nil, err
	}
	return &captureAddressOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
}

func registerCapture(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeUnavailable, CodeCaptureName, CodeCaptureReserved, CodeCaptureTaken}, locks...)

	register(a, authz.GetCaptureAddresses, huma.Operation{
		OperationID: "getCaptureAddresses", Method: http.MethodGet, Path: "/v1/organisation/email/capture-addresses", Tags: []string{"Organisation"},
		Summary: "Get the email-in addresses",
		Description: "The organisation's email-in address and the caller's own, on the deployment's capture domain (FR-DOC-12, FR-INT-22; the " +
			"owner's decisions of 23 Oct 2026): bills and receipts sent or forwarded to one are captured as documents for someone to confirm. " +
			"Mail from the organisation's members and its contacts' billing and remittance emails is accepted; other senders wait in " +
			"quarantine. Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*captureAddressesOutput, error) {
		p, _ := authn.From(ctx)
		var out CaptureAddresses
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			out, err = a.captureAddresses(ctx, u, p.UserID)
			return err
		})
		return &captureAddressesOutput{Body: out}, err
	})

	register(a, authz.SetOrganisationCaptureAddress, huma.Operation{
		OperationID: "setOrganisationCaptureAddress", Method: http.MethodPut, Path: "/v1/organisation/email/capture-addresses/organisation",
		Tags: []string{"Organisation"}, Summary: "Name the organisation's email-in address",
		Description: "Makes the organisation's email-in address with a readable name, such as bills, replacing the one it has, or retires it " +
			"when the name is left out (FR-INT-22). A name is 3 to 40 lower-case letters, digits, dots and hyphens (EML-05), not one kept for " +
			"the mail system (EML-06), and stays with its organisation for good, so another's is refused (EML-07); mail to a replaced address " +
			"is no longer captured. Side effects: the address and its audit events. Needs If-Match with organisation_etag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, writes, func(ctx context.Context, in *captureAddressInput) (*captureAddressOutput, error) {
		p, _ := authn.From(ctx)
		return a.setCaptureAddress(ctx, in, nil, p.UserID)
	})

	register(a, authz.SetMyCaptureAddress, huma.Operation{
		OperationID: "setMyCaptureAddress", Method: http.MethodPut, Path: "/v1/organisation/email/capture-addresses/mine",
		Tags: []string{"Organisation"}, Summary: "Name your own email-in address",
		Description: "Makes the caller's own email-in address in the organisation, such as mei.ling, replacing the one they have, or retires " +
			"it when the name is left out (FR-INT-22): mail from their own sign-in email to it is captured for them. Refused for a caller " +
			"who is not a person with a membership (EML-08). The name rules are the organisation address's (EML-05, EML-06, EML-07). Side " +
			"effects: the address and its audit events. Needs If-Match with mine_etag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and saves nothing.",
	}, append(writes, CodeCaptureNoUser), func(ctx context.Context, in *captureAddressInput) (*captureAddressOutput, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil || p.Class != authn.ClassUser {
			return nil, problem(CodeCaptureNoUser, "Only a person who is a member has an email-in address of their own.",
				"Sign in as yourself, or use the organisation's address")
		}
		user := p.UserID
		return a.setCaptureAddress(ctx, in, &user, p.UserID)
	})
}
