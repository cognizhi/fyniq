package api

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/apiclient"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the link to Nemura (PRD-001 §8.26, FR-PPL-01, 06, 15, 16; p1-payroll-link)
//
// Settings → People and HR shows where each feature lives and the link's state, epoch and health (FR-PPL-01, FR-PPL-08). Linking and
// unlinking are dual controlled: an Admin or the Owner asks, the Owner and a second approver approve (golden rule 6), and only then is the
// link's client registered with the OAuth server and the link recorded. Nemura calls back with that client, holding Appendix E's scopes and
// nothing else, and every event it sends is an envelope signed with the link key (FR-PPL-15): the receiving operations check the
// envelope against the link as Fyniq holds it, never against anything the message carries, apply the event once, and keep an older
// version of a person from undoing a newer one. A pay or bank detail never reaches a log, the inbox or a task.

// The codes of the link's own refusals.
const (
	CodeNoLink      = "PPL-07" // the organisation has no active link
	CodeLinkClient  = "PPL-08" // the call did not come through the link's client
	CodeLinkMinimum = "PPL-09" // the payload carries a field Appendix E does not list
	CodeLinkExists  = "PPL-10" // the organisation is linked already
)

// linkFixes say how to fix each of the link's refusals (ENG-23).
var linkFixes = map[string]string{
	peoplelink.CodeMalformed: "Send the envelope of PRD-002 NM-INT-10 with a payload in the shape of the event's schema",
	peoplelink.CodeSignature: "Sign the header with the link key the organisation approved",
	peoplelink.CodeDigest:    "Send the payload whose canonical SHA-256 the signed header carries",
	peoplelink.CodeReplay:    "Sign the event again with a current time and send it within 5 minutes",
	peoplelink.CodeEpoch:     "Send the link's current epoch, shown on getPeopleLink",
	peoplelink.CodeOrg:       "Send the event from the linked Nemura organisation to this organisation",
	CodeLinkMinimum:          "Send only the fields Appendix E lists: never NRIC or FIN, medical, disciplinary or review data",
}

func linkRefusal(code, detail string) *Problem { return problem(code, detail, linkFixes[code]) }

// PeopleFeature is a feature People and HR settings place: records, payroll, leave or letters.
type PeopleFeature string

// Schema implements huma.SchemaProvider.
func (PeopleFeature) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Enum: []any{"records", "payroll", "leave", "letters"}, Description: "A feature that can live in Nemura.", Examples: []any{"payroll"}}
}

// PeopleLinkSource is where one feature lives (FR-PPL-01).
type PeopleLinkSource struct {
	Feature       string    `json:"feature" enum:"records,payroll,leave,letters" doc:"The feature." example:"payroll"`
	Source        string    `json:"source" enum:"nemura,external,none" doc:"Where it lives: Nemura, an external system, or nowhere (payroll posted by manual journal)." example:"nemura"`
	EffectiveFrom string    `json:"effective_from" format:"date" doc:"The day it started there; the start day of a change of source (FR-PPL-16)." example:"2026-11-20"`
	SetAt         time.Time `json:"set_at" doc:"When it was set." example:"2026-11-20T09:00:00Z"`
}

// PeopleLinkInbox counts the events the link received by outcome (FR-PPL-08).
type PeopleLinkInbox struct {
	Outcome string     `json:"outcome" enum:"applied,ignored_older,refused" doc:"What became of them." example:"applied"`
	Count   int        `json:"count" doc:"How many." example:"12"`
	LastAt  *time.Time `json:"last_at,omitempty" doc:"When the last arrived." example:"2026-11-20T09:00:00Z"`
}

// PeopleLink is the organisation's link to the people platform and where each feature lives.
type PeopleLink struct {
	State                string             `json:"state" enum:"none,active,suspended,unlinking,unlinked" doc:"none: never linked; unlinked is final, and a later link starts at a higher epoch." example:"active"`
	Epoch                int                `json:"epoch,omitempty" doc:"The link's epoch: every event carries it and an earlier one is refused (FR-PPL-16)." example:"1"`
	NemuraOrganisationID string             `json:"nemura_organisation_id,omitempty" format:"uuid" doc:"The Nemura organisation it is linked to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	ClientID             string             `json:"client_id,omitempty" format:"uuid" doc:"The API client Nemura calls with: its scopes are Appendix E's and no other." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	Version              int                `json:"version,omitempty" doc:"The link's version." example:"1"`
	LinkedAt             *time.Time         `json:"linked_at,omitempty" doc:"When it was linked." example:"2026-11-20T09:00:00Z"`
	OAuthClientID        string             `json:"oauth_client_id,omitempty" doc:"The ID the link's client has at Fyniq's OAuth server: what Nemura signs its token request as. Give it to Nemura when it approves the link." example:"4f6c1b52-8d2a-4c93-9a43-0c2d7e9b1a11"`
	FyniqLinkKey         string             `json:"fyniq_link_key,omitempty" doc:"Fyniq's Ed25519 public key for what Fyniq sends (payroll.run.posted, payroll.payments.status), 32 bytes as base64url without padding: Nemura checks every event against it. Give it to Nemura when it approves the link." example:"EJ6DPOspy58Ak9Q0e0rNKaX-nWfsuBEvOeZl0Y9qPzE"`
	FyniqClientKey       map[string]any     `json:"fyniq_client_public_key,omitempty" doc:"The public half of the key Fyniq's own client signs its token request to Nemura's OAuth server with, as a JWK: Nemura registers it for Fyniq's client. Never the private key."`
	FyniqAskKey          map[string]any     `json:"fyniq_ask_client_public_key,omitempty" doc:"The public half of the key Fyniq's read-only client signs its token request with, as a JWK: Nemura registers it for the client that asks whether a person is held at the release of a payment. Never the private key."`
	NemuraAskClientID    string             `json:"nemura_ask_client_id,omitempty" doc:"The ID Fyniq's read-only client has at Nemura's OAuth server, as Nemura chose it when it proposed the link." example:"fyniq-ask-0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44"`
	NemuraClientID       string             `json:"nemura_client_id,omitempty" doc:"The ID Fyniq's client has at Nemura's OAuth server, as Nemura chose it when it proposed the link." example:"fyniq-link-0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44"`
	Sources              []PeopleLinkSource `json:"sources" nullable:"false" doc:"Where each feature lives now."`
	Inbox                []PeopleLinkInbox  `json:"inbox" nullable:"false" doc:"The events received, by outcome."`
}

// PeopleLinkRequest asks to link the organisation to a Nemura organisation.
type PeopleLinkRequest struct {
	NemuraOrganisationID string          `json:"nemura_organisation_id" format:"uuid" doc:"The Nemura organisation to link to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	NemuraOrigin         string          `json:"nemura_origin" pattern:"^https?://[a-z0-9.-]{1,200}(:[0-9]{1,5})?$" doc:"Where Nemura's web app is served from, such as https://people.example.com: a passkey assertion made there names it, and Fyniq checks a pay run's approval against it (one relying-party ID, two origins)." example:"https://people.example.com"`
	LinkKey              string          `json:"link_key" minLength:"43" maxLength:"43" doc:"Nemura's Ed25519 public key for the link's envelopes, 32 bytes as base64url without padding: shown on Nemura's link page. Every event is checked against it." example:"EJ6DPOspy58Ak9Q0e0rNKaX-nWfsuBEvOeZl0Y9qPzE"`
	ClientName           string          `json:"client_name" minLength:"1" maxLength:"80" doc:"The name of the client Nemura calls with." example:"Nemura link"`
	NemuraClientID       string          `json:"nemura_client_id" minLength:"1" maxLength:"200" pattern:"^[A-Za-z0-9._:-]+$" doc:"The ID Fyniq's own client will have at Nemura's OAuth server, as Nemura chose it when it proposed the link: shown on Nemura's link page. Fyniq signs in to Nemura as this client to send its events." example:"fyniq-link-0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44"`
	NemuraAskClientID    string          `json:"nemura_ask_client_id" minLength:"1" maxLength:"200" pattern:"^[A-Za-z0-9._:-]+$" doc:"The ID Fyniq's read-only client, which asks whether a person is held at the release of a payment, will have at Nemura's OAuth server, as Nemura chose it when it proposed the link: shown on Nemura's link page." example:"fyniq-ask-0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44"`
	ClientPublicKey      map[string]any  `json:"client_public_key" doc:"The public half of the key Nemura's client signs its private_key_jwt assertions with, as a JWK: an EC P-256 key or an RSA key of 2048 bits or more. Never the private key."`
	ExpiresAt            time.Time       `json:"expires_at" doc:"When the client stops, within a year (SEC-07); link again before then." example:"2027-11-17T00:00:00Z"`
	Features             []PeopleFeature `json:"features" minItems:"1" maxItems:"4" uniqueItems:"true" doc:"The features that live in Nemura from now on: Fyniq never loads them itself (FR-PPL-01)."`
}

// PeopleUnlinkRequest asks to end the link.
type PeopleUnlinkRequest struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why the link ends, for the access review." example:"The company moved to another payroll provider."`
}

// PeopleLinkHeader is the header of an envelope (PRD-002 NM-INT-10): what the event says about itself, signed with the link key.
type PeopleLinkHeader struct {
	EventID            string `json:"event_id" format:"uuid" doc:"The event's ID: a repeat does nothing." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	Type               string `json:"type" doc:"The event type, such as people.person.upserted." example:"people.person.upserted"`
	SchemaVersion      int    `json:"schema_version" minimum:"1" doc:"The payload schema's version." example:"1"`
	SourceSystem       string `json:"source_system" enum:"nemura" doc:"The sending system." example:"nemura"`
	SourceOrganisation string `json:"source_organisation" format:"uuid" doc:"The Nemura organisation." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	TargetOrganisation string `json:"target_organisation" format:"uuid" doc:"This organisation." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	Epoch              int    `json:"epoch" minimum:"1" doc:"The link's epoch; an earlier or a later one is refused." example:"1"`
	Sequence           int64  `json:"sequence" minimum:"0" doc:"The sender's running number on this link, for gap repair and the reconciliation checkpoint." example:"41"`
	ObjectType         string `json:"object_type" minLength:"1" maxLength:"50" doc:"The kind of object the event is about." example:"person"`
	ObjectID           string `json:"object_id" minLength:"1" maxLength:"100" doc:"The object's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	ObjectVersion      int    `json:"object_version" minimum:"1" doc:"The object's version: an older one than applied is ignored." example:"2"`
	EffectiveAt        string `json:"effective_at" format:"date-time" doc:"When the change took effect, RFC 3339 in UTC." example:"2026-11-20T09:00:00Z"`
	RecordedAt         string `json:"recorded_at" format:"date-time" doc:"When the sender recorded it." example:"2026-11-20T09:00:00Z"`
	SignedAt           string `json:"signed_at" format:"date-time" doc:"When it was signed: more than 5 minutes from now is refused." example:"2026-11-20T09:00:01Z"`
	CorrelationID      string `json:"correlation_id" format:"uuid" doc:"Ties the event to its cause." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	PayloadSHA256      string `json:"payload_sha256" pattern:"^[0-9a-f]{64}$" doc:"The SHA-256 of the payload's canonical JSON (contracts/peoplelink)." example:"54a65415ad370228851a1da4b31b6fd42dc58b19a50d35cae759325f7388ce64"`
}

// PeopleLinkEvent is an event or call between the apps: the envelope of NM-INT-10.
type PeopleLinkEvent struct {
	Header    PeopleLinkHeader `json:"header"`
	Signature string           `json:"signature" minLength:"1" maxLength:"200" doc:"Ed25519 over the canonical header, base64url without padding, with the link key (contracts/peoplelink)." example:"EJ6DPOspy58Ak9Q0e0rNKaX-nWfsuBEvOeZl0Y9qPzE5S1xv2o8mZl7sQd0pTuHk3r9iV6bYw4cG1nXfA2JgQ"`
	Payload   map[string]any   `json:"payload" doc:"The event's data: only the fields PRD-002 Appendix E lists for its type."`
}

// PeopleLinkReceipt says what became of an event.
type PeopleLinkReceipt struct {
	EventID string `json:"event_id" format:"uuid" doc:"The event's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	Outcome string `json:"outcome" enum:"applied,ignored_older,duplicate" doc:"applied; ignored_older: a version no newer than the one applied, recorded and changing nothing; duplicate: the event ID was received before." example:"applied"`
}

type (
	peopleLinkOutput struct {
		Body PeopleLink
	}
	peopleLinkRequestInput struct {
		KeyHeader
		PreferHeader
		Body PeopleLinkRequest
	}
	peopleUnlinkRequestInput struct {
		KeyHeader
		PreferHeader
		Body PeopleUnlinkRequest
	}
	peopleLinkAskOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     Approval
	}
	peopleEventInput struct {
		RawBody []byte
		Body    PeopleLinkEvent
	}
	peopleEventOutput struct{ Body PeopleLinkReceipt }
)

// linkFeatures are the features People and HR settings place.
var linkFeatures = []string{storage.FeatureRecords, storage.FeaturePayroll, storage.FeatureLeave, storage.FeatureLetters}

func peopleLinkOut(l storage.PeopleLinkRow, ok bool, sources []storage.PeopleSource, inbox []storage.InboxCount) PeopleLink {
	out := PeopleLink{State: "none", Sources: []PeopleLinkSource{}, Inbox: []PeopleLinkInbox{}}
	if ok {
		linked := l.CreatedAt
		out.State, out.Epoch, out.NemuraOrganisationID, out.Version, out.LinkedAt = l.State, l.Epoch, l.NemuraOrganisationID.String(), l.Version, &linked
		if l.ClientID != uuid.Nil {
			out.ClientID = l.ClientID.String()
		}
	}
	for _, f := range linkFeatures { // every feature shows where it lives: nowhere until a source is set
		src := PeopleLinkSource{Feature: f, Source: storage.SourceNone}
		for _, s := range sources {
			if s.Feature == f {
				src = PeopleLinkSource{Feature: f, Source: s.Source, EffectiveFrom: s.EffectiveFrom.Format(time.DateOnly), SetAt: s.SetAt}
			}
		}
		out.Sources = append(out.Sources, src)
	}
	for _, c := range inbox {
		row := PeopleLinkInbox{Outcome: c.Outcome, Count: c.N}
		if !c.LastAt.IsZero() {
			t := c.LastAt
			row.LastAt = &t
		}
		out.Inbox = append(out.Inbox, row)
	}
	return out
}

// PeopleProjectionPerson is a person of the employee projection (FR-PPL-06): what accounting needs of them, no pay, bank number or NRIC.
type PeopleProjectionPerson struct {
	PersonID    string `json:"person_id" format:"uuid" doc:"The person's ID in Nemura." example:"0192a000-0000-7000-8000-0000000000e1"`
	DisplayName string `json:"display_name,omitempty" doc:"Their name." example:"Wei Jie Ong"`
	Status      string `json:"status,omitempty" doc:"Their employment status." example:"active"`
	Department  string `json:"department_id,omitempty" format:"uuid" doc:"Their department: a dimension value of Fyniq's." example:"0192a000-0000-7000-8000-0000000000d1"`
	LineManager string `json:"line_manager_person_id,omitempty" format:"uuid" doc:"Their line manager's person ID, for claim routing (FR-AP-17)." example:"0192a000-0000-7000-8000-0000000000e2"`
	Held        bool   `json:"held" doc:"Whether a tax-clearance hold is open on them (FR-PPL-13)." example:"false"`
	Removed     bool   `json:"removed" doc:"Whether Nemura removed their record." example:"false"`
	UserID      string `json:"user_id,omitempty" format:"uuid" doc:"The Fyniq user they were mapped to, explicitly and never by email (FR-PPL-18)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
}

// PeopleProjectionPage is a page of the projection's people.
type PeopleProjectionPage struct {
	Items      []PeopleProjectionPerson `json:"items" nullable:"false" doc:"The people, by name."`
	NextCursor string                   `json:"next_cursor,omitempty" doc:"The cursor of the next page; none on the last." example:"eyJhZnRlciI6IjEwMDAifQ"`
}

// PersonPath names a person of the projection.
type PersonPath struct {
	PersonID string `path:"person_id" doc:"The person's ID in Nemura." example:"0192a000-0000-7000-8000-0000000000e1"`
}

// PersonUserMap maps a person to a Fyniq user.
type PersonUserMap struct {
	UserID string `json:"user_id" format:"uuid" doc:"The member of this organisation the person is: no Fyniq role is granted by it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
}

type (
	peopleProjectionOutput struct{ Body PeopleProjectionPage }
	personUserInput        struct {
		PersonPath
		KeyHeader
		PreferHeader
		Body PersonUserMap
	}
	personUserOutput struct {
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     PeopleProjectionPerson
	}
)

func projectionOut(p peoplelink.Person) PeopleProjectionPerson {
	out := PeopleProjectionPerson{PersonID: p.ID.String(), DisplayName: p.DisplayName, Status: p.Status, Held: p.Held, Removed: p.Removed}
	if p.DepartmentValue != uuid.Nil {
		out.Department = p.DepartmentValue.String()
	}
	if p.LineManager != uuid.Nil {
		out.LineManager = p.LineManager.String()
	}
	if p.UserID != uuid.Nil {
		out.UserID = p.UserID.String()
	}
	return out
}

func registerPeopleLink(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.ListPeopleProjection, huma.Operation{
		OperationID: "listPeopleProjection", Method: http.MethodGet, Path: "/v1/people-link/people", Tags: []string{"People"},
		Summary: "List the people of the employee projection",
		Description: "The people Nemura told Fyniq about (FR-PPL-06), by name, a page at a time: their name, status, department, line manager's person ID, whether " +
			"a hold is open, and the Fyniq user each was mapped to. Never pay, a bank number or an NRIC. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *PageQuery) (*peopleProjectionOutput, error) {
		p, _ := authn.From(ctx)
		out := PeopleProjectionPage{Items: []PeopleProjectionPerson{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.PeopleLinks().People(ctx)
			if err != nil {
				return err
			}
			page, next, err := pageOf(all, func(x peoplelink.Person) string { return x.ID.String() }, *in)
			for _, x := range page {
				out.Items = append(out.Items, projectionOut(x))
			}
			out.NextCursor = next
			return err
		})
		return &peopleProjectionOutput{Body: out}, err
	})

	register(a, authz.MapPersonToUser, huma.Operation{
		OperationID: "mapPersonToUser", Method: http.MethodPost, Path: "/v1/people-link/people/{person_id}/user", Tags: []string{"People"},
		Summary: "Map a person of the projection to a Fyniq user",
		Description: "Maps a person Nemura told Fyniq about to the member of this organisation they are, explicitly: never by email (FR-PPL-18, NM-INT-12). " +
			"The mapping grants no Fyniq role. It is what a line manager's claim routing and an approver's own department read (FR-AP-17), and what " +
			"names a payroll approver's identity. One user maps to one person. Side effects: the projection's change and its audit event by field " +
			"name. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing. On the never-list for the AI and MCP (§9.6).",
	}, append([]string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid, storage.CodePeopleLink}, locks...), func(ctx context.Context, in *personUserInput) (*personUserOutput, error) {
		person, err := parseID("person_id", in.PersonID)
		if err != nil {
			return nil, err
		}
		user, err := parseID("body.user_id", in.Body.UserID)
		if err != nil {
			return nil, err
		}
		resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PeopleProjectionPerson, error) {
			cur, err := u.PeopleLinks().Person(ctx, person)
			if err != nil {
				return PeopleProjectionPerson{}, err
			}
			if !cur.Known() {
				return PeopleProjectionPerson{}, problem(CodeNotFound, "No such person in the projection.", "Choose a person from listPeopleProjection.")
			}
			roles, err := u.Members().Roles(ctx, user, a.deps.Clock.Now())
			if err != nil && !errors.Is(err, ports.ErrNotFound) {
				return PeopleProjectionPerson{}, err
			}
			if len(roles) == 0 {
				return PeopleProjectionPerson{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.user_id", Message: "is not an active member of this organisation", Fix: "Choose a member from the members list"}})
			}
			cur.UserID, cur.UpdatedAt = user, a.deps.Clock.Now()
			if err := u.PeopleLinks().SavePerson(ctx, cur); err != nil {
				return PeopleProjectionPerson{}, err
			}
			return projectionOut(cur), nil
		})
		if err != nil {
			return nil, err
		}
		return &personUserOutput{Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
	})

	register(a, authz.GetPeopleLink, huma.Operation{
		OperationID: "getPeopleLink", Method: http.MethodGet, Path: "/v1/people-link", Tags: []string{"People"},
		Summary: "Show the link to the people platform and where each feature lives",
		Description: "Settings → People and HR (FR-PPL-01, FR-PPL-08): where each feature (people records, payroll, leave, letters) lives, whether the " +
			"organisation is linked to a Nemura organisation, the link's state and epoch, and how many events the link received by outcome " +
			"(applied, ignored as older, refused). Never a key or a token. Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*peopleLinkOutput, error) {
		p, _ := authn.From(ctx)
		var out PeopleLink
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			sources, err := u.PeopleLinks().Sources(ctx)
			if err != nil {
				return err
			}
			l, err := u.PeopleLinks().Live(ctx)
			if errors.Is(err, ports.ErrNotFound) {
				out = peopleLinkOut(storage.PeopleLinkRow{}, false, sources, nil)
				return nil
			}
			if err != nil {
				return err
			}
			inbox, err := u.PeopleLinks().InboxCounts(ctx, l.ID)
			if err != nil {
				return err
			}
			out = peopleLinkOut(l, true, sources, inbox)
			// What Nemura needs to complete its half of the link: Fyniq's public keys and the client's ID at Fyniq's OAuth server. Never a private key.
			if keys, err := u.PeopleLinks().Keys(ctx, l.ID); err == nil {
				out.FyniqLinkKey, out.NemuraClientID = base64.RawURLEncoding.EncodeToString(keys.LinkPublic), keys.NemuraClientID
				_ = json.Unmarshal(keys.ClientPublic, &out.FyniqClientKey)
			} else if !errors.Is(err, ports.ErrNotFound) {
				return err
			}
			if ask, err := u.PeopleLinks().AskKey(ctx, l.ID); err == nil {
				out.NemuraAskClientID = ask.ClientID
				_ = json.Unmarshal(ask.Public, &out.FyniqAskKey)
			} else if !errors.Is(err, ports.ErrNotFound) {
				return err
			}
			if l.ClientID != uuid.Nil {
				c, err := u.APIClients().Get(ctx, l.ClientID)
				if err != nil {
					return err
				}
				out.OAuthClientID = c.ClientID
			}
			return nil
		})
		return &peopleLinkOutput{Body: out}, err
	})

	register(a, authz.RequestPeopleLink, huma.Operation{
		OperationID: "requestPeopleLink", Method: http.MethodPost, Path: "/v1/people-link", Tags: []string{"People"}, DefaultStatus: http.StatusAccepted,
		Summary: "Ask to link the organisation to a Nemura organisation",
		Description: "Asks to link the organisation to a Nemura organisation (FR-PPL-01, FR-PPL-16, PRD-002 NM-ARC-04): you give Nemura's link key (the " +
			"public key every event is checked against), the public key of the client Nemura calls with, its expiry within a year, and the " +
			"features that live in Nemura. It is dual controlled (golden rule 6): the answer is the approval request, which the Owner approves " +
			"with a second approver; only then is the client registered with the OAuth server, holding Appendix E's scopes and no other, and the " +
			"link recorded at the next epoch with each feature's source. Fyniq never loads a feature that lives in Nemura. Side effects: the " +
			"approval request and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing. Needs a " +
			"person signed in with step-up. On the never-list for the AI and MCP (§9.6).",
	}, append(append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired, CodeUnavailable,
		approval.CodePending, CodeLinkExists, CodeInvalid, apiclient.CodeName, apiclient.CodeExpiry, apiclient.CodeKey}, locks...), storage.CodePeopleLink), func(ctx context.Context, in *peopleLinkRequestInput) (*peopleLinkAskOutput, error) {
		if a.deps.OAuthClients == nil || a.deps.APIAudience == "" {
			return nil, problem(CodeUnavailable, "This installation has no OAuth server configured, so it cannot link to the people platform.",
				"Ask the operator to configure the OAuth server (FYNIQ_OAUTH_ADMIN_URL and FYNIQ_API_AUDIENCE).")
		}
		org, err := parseID("body.nemura_organisation_id", in.Body.NemuraOrganisationID)
		if err != nil {
			return nil, err
		}
		key, err := linkKey(in.Body.LinkKey)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.link_key", Message: err.Error(), Fix: "Copy the link key from Nemura's link page: 32 bytes as base64url, 43 characters"}})
		}
		for _, f := range in.Body.Features {
			if !slices.Contains(linkFeatures, string(f)) {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.features", Message: fmt.Sprintf("%q is not a feature", f), Fix: "Choose records, payroll, leave or letters"}})
			}
		}
		clientKey, err := json.Marshal(in.Body.ClientPublicKey)
		if err != nil || len(in.Body.ClientPublicKey) == 0 {
			clientKey = nil
		}
		now := a.deps.Clock.Now()
		checked, err := apiclient.CheckLink(apiclient.Link{Name: in.Body.ClientName, ExpiresAt: in.Body.ExpiresAt, Key: clientKey}, now)
		if err != nil {
			return nil, clientProblem(err)
		}
		public, err := apiclient.PublicJWK(clientKey, checked.Alg)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		features := make([]string, len(in.Body.Features))
		for i, f := range in.Body.Features {
			features[i] = string(f)
		}
		slices.Sort(features)
		resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			if _, err := u.PeopleLinks().Live(ctx); err == nil {
				return Approval{}, problem(CodeLinkExists, "The organisation is linked already.", "Unlink first, then link again: a link is never changed in place.")
			} else if !errors.Is(err, ports.ErrNotFound) {
				return Approval{}, err
			}
			change := map[string]any{"id": uuid.NewString(), "nemura_organisation_id": org.String(), "nemura_origin": in.Body.NemuraOrigin, "link_key": base64.RawURLEncoding.EncodeToString(key),
				"nemura_client_id": in.Body.NemuraClientID, "nemura_ask_client_id": in.Body.NemuraAskClientID, "client_name": checked.Name, "client_public_key": string(public), "alg": checked.Alg, "key_thumbprint": checked.Thumbprint,
				"expires_at": checked.ExpiresAt.UTC().Format(time.RFC3339), "features": anyList(features)}
			diff := []ApprovalDiff{{Field: "nemura_organisation_id", After: org.String()}, {Field: "features in Nemura", After: fmt.Sprint(features)},
				{Field: "scopes", After: fmt.Sprint(apiclient.LinkScopes)}, {Field: "client key thumbprint", After: checked.Thumbprint},
				{Field: "expires_at", After: checked.ExpiresAt.UTC().Format(time.RFC3339)}}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectPeopleLink, ObjectKey: p.Organisation.String(), Operation: approval.OpPeopleLink,
				Summary: "Link the organisation to the Nemura organisation " + org.String(), Execute: change, Diff: diff})
		})
		if err != nil {
			return nil, err
		}
		return &peopleLinkAskOutput{Status: resp.Status, Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
	})

	register(a, authz.RequestPeopleUnlink, huma.Operation{
		OperationID: "requestPeopleUnlink", Method: http.MethodPost, Path: "/v1/people-link/unlink", Tags: []string{"People"}, DefaultStatus: http.StatusAccepted,
		Summary: "Ask to end the link to the people platform",
		Description: "Asks to end the link (FR-PPL-16, PRD-002 NM-INT-11). It is dual controlled like linking: the Owner and a second approver approve. " +
			"Unlinking first settles work in flight (runs approved and not posted, salary payments with an unknown outcome, open tax-clearance " +
			"holds, events waiting) and only then revokes the link's client; events and credentials of the ended epoch are refused from then. " +
			"Both sides keep their data. Side effects: the approval request and its audit event. Idempotent with Idempotency-Key. Needs a " +
			"person signed in with step-up. On the never-list for the AI and MCP (§9.6).",
	}, append(append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired, CodeUnavailable,
		approval.CodePending, CodeNoLink}, locks...), storage.CodePeopleLink), func(ctx context.Context, in *peopleUnlinkRequestInput) (*peopleLinkAskOutput, error) {
		p, _ := authn.From(ctx)
		resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			l, err := u.PeopleLinks().Live(ctx)
			if errors.Is(err, ports.ErrNotFound) {
				return Approval{}, problem(CodeNoLink, "The organisation has no link to end.", "Nothing to do.")
			}
			if err != nil {
				return Approval{}, err
			}
			change := map[string]any{"link_id": l.ID.String(), "epoch": l.Epoch, "reason": in.Body.Reason}
			diff := []ApprovalDiff{{Field: "state", Before: l.State, After: "unlinked"}, {Field: "reason", After: in.Body.Reason}}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectPeopleLink, ObjectKey: p.Organisation.String(), Operation: approval.OpPeopleUnlink,
				Summary: "End the link to the Nemura organisation " + l.NemuraOrganisationID.String(), Execute: change, Diff: diff})
		})
		if err != nil {
			return nil, err
		}
		return &peopleLinkAskOutput{Status: resp.Status, Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
	})

	eventCodes := append(append([]string{CodeNoLink, CodeLinkClient, CodeLinkMinimum, peoplelink.CodeMalformed, peoplelink.CodeSignature, peoplelink.CodeDigest, peoplelink.CodeReplay,
		peoplelink.CodeEpoch, peoplelink.CodeOrg}, locks...), storage.CodePeopleLink)
	// An event carries its own ID, which the inbox dedupes (FR-PPL-15), so a repeat does nothing without an Idempotency-Key; and a preview
	// would apply nothing the sender could rely on, since only the signed envelope is evidence.
	byID := func(op huma.Operation) huma.Operation {
		op = exempt(op, "Idempotency-Key", "The event's ID is its idempotency key: the inbox receives each ID once and a repeat answers duplicate (FR-PPL-15, NM-INT-10).")
		return exempt(op, "Prefer", "An event is a signed message between the apps, not a person's command: a preview of it would prove nothing (FR-INT-14).")
	}
	register(a, authz.ReceivePeopleProjectionEvent, byID(huma.Operation{
		OperationID: "receivePeopleProjectionEvent", Method: http.MethodPost, Path: "/v1/people-link/projection-events", Tags: []string{"People"},
		Summary: "Receive a person, bank-details or identity event from Nemura",
		Description: "Nemura's `people.person.upserted`, `people.bank_details.changed` and `identity.access.changed` (PRD-002 Appendix E; the last says whether a sign-in identity is an active member there and approves pay runs, by issuer and subject and two yes or no answers: a door and a hint, never a grant, with the version the sending event's sequence): an envelope (NM-INT-10) whose header the link " +
			"key signed, with the payload's canonical SHA-256 in it. It needs the link's client and the scope people.projection.write, and the " +
			"envelope is checked against the link as Fyniq holds it: its organisations, its epoch, a signing time within 5 minutes, its signature " +
			"and its digest; nothing the message carries decides who may send it. An event received before does nothing (duplicate); a version " +
			"no newer than the one applied is ignored and recorded (a late version 1 never sets the manager back); a payload with a field " +
			"Appendix E does not list is refused (NM-INT-05). Side effects: the projection's change and its audit event, by field name and " +
			"never by value, and the inbox row (identities and a digest, never the payload). Pay and bank details are never logged. Only the " +
			"link's client may call it; on the never-list for the AI and MCP (§9.6).",
	}), eventCodes, func(ctx context.Context, in *peopleEventInput) (*peopleEventOutput, error) {
		return a.receivePeopleEvent(ctx, in, peoplelink.TypePersonUpserted, peoplelink.TypeBankDetailsChange, peoplelink.TypeAccessChanged)
	})
	register(a, authz.ReceivePeopleHoldEvent, byID(huma.Operation{
		OperationID: "receivePeopleHoldEvent", Method: http.MethodPost, Path: "/v1/people-link/hold-events", Tags: []string{"People"},
		Summary: "Receive a tax-clearance hold event from Nemura",
		Description: "Nemura's `people.tax_hold.changed` (PRD-002 NM-INT-08, Appendix E): a person held from a date, or released after IRAS's notice or " +
			"directive recorded with evidence. It needs the link's client and the scope people.holds.write and is checked, applied once and " +
			"versioned exactly as receivePeopleProjectionEvent is. A held person's lines are not released (FR-PPL-13); only Nemura's release " +
			"clears a hold. Only the link's client may call it; on the never-list for the AI and MCP (§9.6).",
	}), eventCodes, func(ctx context.Context, in *peopleEventInput) (*peopleEventOutput, error) {
		return a.receivePeopleEvent(ctx, in, peoplelink.TypeTaxHoldChanged)
	})
	register(a, authz.ReceivePeopleSessionEvent, byID(huma.Operation{
		OperationID: "receivePeopleSessionEvent", Method: http.MethodPost, Path: "/v1/people-link/session-events", Tags: []string{"People"},
		Summary: "Receive a sign-out or removal from Nemura",
		Description: "Nemura's `identity.session.revoked` (PRD-002 NM-INT-12, Appendix E): a person was signed out of, or removed from, Nemura, named by sign-in issuer and subject and never an email, with a reason " +
			"(signed_out or removed). Fyniq ends that person's sessions, and only when the identity is a user mapped to a person of the link (mapPersonToUser); it changes no membership, role or passkey, and it answers " +
			"the same whether or not it ended any, so the call tells Nemura nothing of who has an account here. It needs the link's client and the scope identity.sessions.write (a link made before the scope existed " +
			"is approved again to hold it), and is checked, applied once by event ID and never ordered by version, as receivePeopleProjectionEvent is. Side effects: the sessions ended, the audit event (the reason and how " +
			"many, never the identity), and the inbox row. Only the link's client may call it; on the never-list for the AI and MCP (§9.6).",
	}), eventCodes, func(ctx context.Context, in *peopleEventInput) (*peopleEventOutput, error) {
		return a.receivePeopleEvent(ctx, in, peoplelink.TypeSessionRevoked)
	})
}

// linkKey reads Nemura's Ed25519 public key: 32 bytes, base64url without padding.
func linkKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("is not an Ed25519 public key: 32 bytes as base64url without padding")
	}
	return ed25519.PublicKey(b), nil
}

// receivePeopleEvent checks and applies one event of the types it names.
func (a *API) receivePeopleEvent(ctx context.Context, in *peopleEventInput, types ...string) (*peopleEventOutput, error) {
	p, _ := authn.From(ctx)
	// The envelope as it came, its payload as raw bytes: the digest is over those bytes, never over a re-encoding.
	var env peoplelink.Envelope
	dec := json.NewDecoder(bytes.NewReader(in.RawBody))
	if err := dec.Decode(&env); err != nil {
		return nil, problem(peoplelink.CodeMalformed, "The envelope is not well formed.", "Send the envelope of PRD-002 NM-INT-10.")
	}
	if !slices.Contains(types, env.Header.Type) {
		return nil, problem(peoplelink.CodeMalformed, fmt.Sprintf("This operation takes %v, not %q.", types, env.Header.Type), "Send the event to the operation that takes its type.")
	}
	now := a.deps.Clock.Now()
	var receipt PeopleLinkReceipt
	var refused error
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		l, refusal, err := a.linkedEnvelope(ctx, u, p, env, now)
		if err != nil {
			return err
		}
		if refusal != nil {
			refused = refusal
			return nil
		}
		eventID, err := uuid.Parse(env.Header.EventID)
		if err != nil {
			refused = linkRefusal(peoplelink.CodeMalformed, "The event ID is not an ID.")
			return nil
		}
		entry := storage.InboxEntry{EventID: eventID, LinkID: l.ID, Epoch: env.Header.Epoch, Sequence: env.Header.Sequence, Type: env.Header.Type,
			ObjectType: env.Header.ObjectType, ObjectID: env.Header.ObjectID, ObjectVersion: env.Header.ObjectVersion, PayloadSHA256: env.Header.PayloadSHA256, ReceivedAt: now}
		var next peoplelink.Person
		var access *storage.PeopleAccess
		var revoke *peoplelink.SessionRevoked
		var ignored bool
		var perr error
		switch env.Header.Type {
		case peoplelink.TypeAccessChanged:
			access, ignored, perr = a.accessOf(ctx, u, env, now)
		case peoplelink.TypeSessionRevoked:
			revoke, perr = revokedOf(env)
		default:
			next, ignored, perr = a.applyPeopleEvent(ctx, u, env, now)
		}
		switch {
		case perr != nil:
			var r *peoplelink.Refusal
			if !errors.As(perr, &r) {
				return perr
			}
			entry.Outcome, entry.Code = peoplelink.OutcomeRefused, r.Code
			if _, err := u.PeopleLinks().Receive(ctx, entry); err != nil { // the signature held, so the refusal is kept
				return err
			}
			refused = linkRefusal(r.Code, r.Message+".")
			return nil
		case ignored:
			entry.Outcome = peoplelink.OutcomeIgnoredOlder
		default:
			entry.Outcome = peoplelink.OutcomeApplied
		}
		fresh, err := u.PeopleLinks().Receive(ctx, entry)
		if err != nil {
			return err
		}
		receipt = PeopleLinkReceipt{EventID: env.Header.EventID, Outcome: entry.Outcome}
		if !fresh {
			receipt.Outcome = "duplicate" // nothing was saved yet: the person is written only below
			return nil
		}
		if ignored {
			return nil
		}
		if access != nil {
			_, err := u.PeopleLinks().SaveAccess(ctx, *access)
			return err
		}
		if revoke != nil {
			return a.endSessionsOf(ctx, u, l, *revoke, now)
		}
		return u.PeopleLinks().SavePerson(ctx, next)
	})
	if err != nil {
		return nil, err
	}
	if refused != nil {
		return nil, refused
	}
	return &peopleEventOutput{Body: receipt}, nil
}

// accessOf reads Nemura's word on an identity (identity.access.changed) strictly, and says whether it is older than the one held: its version is the sending event's sequence. Nothing but the issuer, the subject
// and the two answers is accepted, so no role name and no email can arrive in it.
func (a *API) accessOf(ctx context.Context, u storage.UnitOfWork, env peoplelink.Envelope, now time.Time) (*storage.PeopleAccess, bool, error) {
	var ev peoplelink.AccessChanged
	d := json.NewDecoder(bytes.NewReader(env.Payload))
	d.DisallowUnknownFields()
	if err := d.Decode(&ev); err != nil || ev.Issuer == "" || ev.Subject == "" || len(ev.Issuer) > 500 || len(ev.Subject) > 500 || env.Header.ObjectVersion < 1 {
		return nil, false, &peoplelink.Refusal{Code: CodeLinkMinimum, Message: "the payload has a field, or a value, that PRD-002 Appendix E does not list for " + env.Header.Type}
	}
	held, err := u.PeopleLinks().Access(ctx, ev.Issuer, ev.Subject)
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return nil, false, err
	}
	version, epoch := int64(env.Header.ObjectVersion), env.Header.Epoch // a sequence starts again with each link: the answer is kept with its epoch, and the newest (epoch, version) wins
	older := err == nil && (held.Epoch > epoch || (held.Epoch == epoch && held.Version >= version))
	return &storage.PeopleAccess{Issuer: ev.Issuer, Subject: ev.Subject, PeopleAccess: ev.PeopleAccess, PayrollApprover: ev.PayrollApprover, Version: version, Epoch: epoch, ReceivedAt: now}, older, nil
}

// linkedEnvelope checks an event against the link as Fyniq holds it: the link is active, the call came through the link's own client, and the envelope's
// organisations, epoch, signing time, signature and digest hold. A failure is a refusal (never the payload); the error is for what is not the sender's fault.
func (a *API) linkedEnvelope(ctx context.Context, u storage.UnitOfWork, p authn.Principal, env peoplelink.Envelope, now time.Time) (storage.PeopleLinkRow, *Problem, error) {
	l, err := u.PeopleLinks().Live(ctx)
	if errors.Is(err, ports.ErrNotFound) || (err == nil && l.State != storage.LinkActive) {
		return l, problem(CodeNoLink, "The organisation has no active link to the people platform.", "Ask the Owner to link Nemura in Settings, People and HR."), nil
	}
	if err != nil {
		return l, nil, err
	}
	client, err := u.APIClients().Get(ctx, l.ClientID)
	if err != nil {
		return l, nil, err
	}
	if client.ClientID != p.Client {
		return l, problem(CodeLinkClient, "The event came through a client that is not the link's.", "Call with the client the link registered."), nil
	}
	link := peoplelink.Link{Key: ed25519.PublicKey(l.LinkKey), Epoch: l.Epoch, SourceOrganisation: l.NemuraOrganisationID.String(),
		TargetOrganisation: p.Organisation.String(), SourceSystem: "nemura"}
	if err := env.Verify(link, now); err != nil {
		var r *peoplelink.Refusal
		if errors.As(err, &r) {
			a.deps.Log.WarnContext(ctx, "people_link.refused", "code", r.Code, "type", env.Header.Type, "organisation", p.Organisation.String()) // never the payload
			return l, linkRefusal(r.Code, r.Message+"."), nil
		}
		return l, nil, err
	}
	return l, nil, nil
}

// applyPeopleEvent applies a verified event of one of the projection's types to the person it is about, with the payload read strictly:
// a field Appendix E does not list is refused (NM-INT-05).
func (a *API) applyPeopleEvent(ctx context.Context, u storage.UnitOfWork, env peoplelink.Envelope, now time.Time) (peoplelink.Person, bool, error) {
	strict := func(v any) error {
		d := json.NewDecoder(bytes.NewReader(env.Payload))
		d.DisallowUnknownFields()
		if err := d.Decode(v); err != nil {
			return &peoplelink.Refusal{Code: CodeLinkMinimum, Message: "the payload has a field, or a value, that PRD-002 Appendix E does not list for " + env.Header.Type}
		}
		return nil
	}
	var personID uuid.UUID
	switch env.Header.Type {
	case peoplelink.TypePersonUpserted:
		var ev peoplelink.PersonUpserted
		if err := strict(&ev); err != nil {
			return peoplelink.Person{}, false, err
		}
		personID = ev.PersonID
		cur, err := u.PeopleLinks().Person(ctx, personID)
		if err != nil {
			return peoplelink.Person{}, false, err
		}
		return peoplelink.ApplyPerson(cur, ev, env.Header.ObjectVersion, now)
	case peoplelink.TypeBankDetailsChange:
		var ev peoplelink.BankDetailsChanged
		if err := strict(&ev); err != nil {
			return peoplelink.Person{}, false, err
		}
		cur, err := u.PeopleLinks().Person(ctx, ev.PersonID)
		if err != nil {
			return peoplelink.Person{}, false, err
		}
		return peoplelink.ApplyBankDetails(cur, ev, env.Header.ObjectVersion, now)
	case peoplelink.TypeTaxHoldChanged:
		var ev peoplelink.TaxHoldChanged
		if err := strict(&ev); err != nil {
			return peoplelink.Person{}, false, err
		}
		cur, err := u.PeopleLinks().Person(ctx, ev.PersonID)
		if err != nil {
			return peoplelink.Person{}, false, err
		}
		return peoplelink.ApplyTaxHold(cur, ev, env.Header.ObjectVersion, now)
	}
	return peoplelink.Person{}, false, &peoplelink.Refusal{Code: peoplelink.CodeMalformed, Message: "the event type is not one the projection applies"}
}

// executePeopleLink runs an approved link or unlink as its requester asked (FR-PPL-01, FR-PPL-16): to link, the client is registered with the
// OAuth server and recorded with the link's scopes and no others, the link is recorded at the next epoch and each feature's source is set;
// to unlink, work in flight settles first (nothing is in flight until payroll arrives), then the link ends and its client is revoked.
func (a *API) executePeopleLink(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	now := a.deps.Clock.Now()
	str := func(k string) string { v, _ := s.Execute[k].(string); return v }
	switch r.Operation {
	case approval.OpPeopleLink:
		id, err := uuid.Parse(str("id"))
		if err != nil {
			return err
		}
		org, err := uuid.Parse(str("nemura_organisation_id"))
		if err != nil {
			return err
		}
		key, err := linkKey(str("link_key"))
		if err != nil {
			return err
		}
		expires, err := time.Parse(time.RFC3339, str("expires_at"))
		if err != nil {
			return err
		}
		checked, err := apiclient.CheckLink(apiclient.Link{Name: str("client_name"), ExpiresAt: expires, Key: json.RawMessage(str("client_public_key"))}, now)
		if err != nil {
			return clientProblem(err)
		}
		if checked.Thumbprint != str("key_thumbprint") {
			return errors.New("people link: the key is not the one asked for")
		}
		if a.deps.OAuthClients == nil || a.deps.APIAudience == "" {
			return problem(CodeUnavailable, "This installation has no OAuth server configured, so it cannot link to the people platform.",
				"Ask the operator to configure the OAuth server (FYNIQ_OAUTH_ADMIN_URL and FYNIQ_API_AUDIENCE).")
		}
		organisation, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		clientID := "preview-" + id.String()
		if !storage.IsPreview(ctx) {
			clientID, err = a.deps.OAuthClients.RegisterIntegration(ctx, ports.IntegrationRegistration{Name: checked.Name, Class: authn.ClassIntegration,
				Audience: a.deps.APIAudience, Scopes: checked.Scopes, Key: json.RawMessage(str("client_public_key")), Alg: checked.Alg,
				TokenLifespan: IntegrationClientTokenLifespan, Organisation: organisation.ID.String()})
			if err != nil {
				return fmt.Errorf("register the link's client with the OAuth server: %w", err)
			}
			registered := clientID
			u.AfterRollback(func(ctx context.Context) { _ = a.deps.OAuthClients.RemoveClient(ctx, registered) })
		}
		client, err := u.APIClients().Create(ctx, storage.APIClient{ID: uuid.Must(uuid.NewV7()), Kind: storage.APIClientLink, ClientID: clientID, Name: checked.Name,
			Class: authn.ClassIntegration, OwnerID: r.RequestedBy, Scopes: checked.Scopes, IPAllow: []string{}, KeyThumbprint: checked.Thumbprint,
			ExpiresAt: expires, ApprovalID: &r.ID, CreatedBy: "user:" + r.RequestedBy.String(), CreatedAt: now})
		if err != nil {
			return err
		}
		linked, err := u.PeopleLinks().Link(ctx, storage.PeopleLinkRow{NemuraOrganisationID: org, NemuraOrigin: str("nemura_origin"), LinkKey: key, ClientID: client.ID, ApprovalID: r.ID,
			CreatedBy: "user:" + r.RequestedBy.String(), CreatedAt: now})
		if err != nil {
			return err
		}
		// Fyniq's own two keys, made now and never again (design D10): the envelope key signs what Fyniq sends, the client key signs its token request to Nemura's OAuth server.
		keys, secrets, err := newLinkKeys(linked.ID, str("nemura_client_id"))
		if err != nil {
			return err
		}
		if err := u.PeopleLinks().SaveKeys(ctx, keys, secrets); err != nil {
			return err
		}
		ask, err := newAskKey(linked.ID, str("nemura_ask_client_id")) // and the read-only client's, for the ask at release (design D10)
		if err != nil {
			return err
		}
		if err := u.PeopleLinks().SaveAskKey(ctx, ask); err != nil {
			return err
		}
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		features, _ := s.Execute["features"].([]any)
		for _, f := range features {
			if err := u.PeopleLinks().SetSource(ctx, storage.PeopleSource{Feature: fmt.Sprint(f), Source: storage.SourceNemura, EffectiveFrom: today, ApprovalID: r.ID, SetAt: now}); err != nil {
				return err
			}
		}
		// The mapping Fyniq already holds goes to Nemura with the link, so a run is calculated against it from the first (NM-INT-01); nothing is queued while Fyniq has none.
		if m, err := u.PayrollRuns().Mapping(ctx); err == nil {
			if err := a.tellNemuraOfTheMapping(ctx, u, organisation.ID, m.Version, m.Entries); err != nil {
				return err
			}
		} else if !errors.Is(err, ports.ErrNotFound) {
			return err
		}
		return a.tellNemuraEveryone(ctx, u) // what each identity may enter, so the switch and the passkey warning are right from the first day
	case approval.OpPeopleUnlink:
		l, err := u.PeopleLinks().Live(ctx)
		if err != nil {
			return err
		}
		if l.State == storage.LinkActive || l.State == storage.LinkSuspended {
			if l, err = u.PeopleLinks().SetState(ctx, l.ID, l.Version, storage.LinkUnlinking, l.ClientID, now); err != nil {
				return err
			}
		}
		// Settle first: runs approved and not posted, salary payments with an unknown outcome, open holds and events waiting hold the link in
		// unlinking (FR-PPL-16). None exists until payroll arrives; each will count itself here.
		if inFlight := a.peopleLinkInFlight(ctx, u); inFlight > 0 {
			return nil
		}
		client, err := u.APIClients().Get(ctx, l.ClientID)
		if err != nil {
			return err
		}
		if _, err := u.APIClients().Revoke(ctx, client.ID, client.Version, "user:"+r.RequestedBy.String(), "The link ended: "+str("reason"), now); err != nil {
			return err
		}
		if _, err := u.PeopleLinks().SetState(ctx, l.ID, l.Version, storage.LinkUnlinked, uuid.Nil, now); err != nil {
			return err
		}
		return a.removeOAuthClient(ctx, u, client.ClientID)
	}
	return fmt.Errorf("people link: no operation %q", r.Operation)
}

// newLinkKeys makes Fyniq's credentials for a link: an Ed25519 key for its events and an ES256 key for its token request, as the keys the link stores and the secrets only the worker reads.
func newLinkKeys(link uuid.UUID, nemuraClient string) (storage.PeopleLinkKeys, storage.PeopleLinkSecrets, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return storage.PeopleLinkKeys{}, storage.PeopleLinkSecrets{}, err
	}
	ck, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return storage.PeopleLinkKeys{}, storage.PeopleLinkSecrets{}, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(ck)
	if err != nil {
		return storage.PeopleLinkKeys{}, storage.PeopleLinkSecrets{}, err
	}
	jwk, err := (&jose.JSONWebKey{Key: &ck.PublicKey, Algorithm: string(jose.ES256), Use: "sig"}).MarshalJSON()
	if err != nil {
		return storage.PeopleLinkKeys{}, storage.PeopleLinkSecrets{}, err
	}
	return storage.PeopleLinkKeys{LinkID: link, NemuraClientID: nemuraClient, LinkPublic: pub, ClientPublic: jwk},
		storage.PeopleLinkSecrets{LinkID: link, LinkPrivate: priv, ClientPrivate: der}, nil
}

// newAskKey makes the ES256 key of the read-only client for the ask at release.
func newAskKey(link uuid.UUID, nemuraClient string) (storage.PeopleLinkAskKey, error) {
	ck, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return storage.PeopleLinkAskKey{}, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(ck)
	if err != nil {
		return storage.PeopleLinkAskKey{}, err
	}
	jwk, err := (&jose.JSONWebKey{Key: &ck.PublicKey, Algorithm: string(jose.ES256), Use: "sig"}).MarshalJSON()
	if err != nil {
		return storage.PeopleLinkAskKey{}, err
	}
	return storage.PeopleLinkAskKey{LinkID: link, ClientID: nemuraClient, Public: jwk, PrivateDER: der}, nil
}

// peopleLinkInFlight is the work a link must settle before it ends (FR-PPL-16).
func (a *API) peopleLinkInFlight(context.Context, storage.UnitOfWork) int { return 0 }

// peopleLinkDependencies are the link's state and epoch: what a request to link or unlink depends on, "none" before a link exists.
func peopleLinkDependencies(ctx context.Context, u storage.UnitOfWork) (int, map[string]any, error) {
	l, err := u.PeopleLinks().Live(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return 0, map[string]any{"state": "none", "epoch": 0}, nil
	}
	if err != nil {
		return 0, nil, err
	}
	return l.Version, map[string]any{"state": l.State, "epoch": l.Epoch}, nil
}

// lineManagerOf is the Fyniq user who is the line manager of a user, by the people projection: the user's person (mapped explicitly, never by email: FR-PPL-18), their line manager's person, and the
// user that person was mapped to. uuid.Nil when any link of that chain is missing, or when the manager is the user themselves. Line manager is a relationship and grants no role (§6.2).
func lineManagerOf(ctx context.Context, u storage.UnitOfWork, user uuid.UUID) (uuid.UUID, error) {
	person, err := u.PeopleLinks().PersonOfUser(ctx, user)
	if errors.Is(err, ports.ErrNotFound) || err == nil && (person.LineManager == uuid.Nil || person.Removed) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	mgr, err := u.PeopleLinks().Person(ctx, person.LineManager)
	if err != nil {
		return uuid.Nil, err
	}
	if !mgr.Known() || mgr.Removed || mgr.UserID == uuid.Nil || mgr.UserID == user || mgr.Status != "" && mgr.Status != "active" {
		return uuid.Nil, nil
	}
	return mgr.UserID, nil
}

// revokedOf reads Nemura's identity.session.revoked strictly: an identity, and a reason of two.
func revokedOf(env peoplelink.Envelope) (*peoplelink.SessionRevoked, error) {
	var ev peoplelink.SessionRevoked
	d := json.NewDecoder(bytes.NewReader(env.Payload))
	d.DisallowUnknownFields()
	if err := d.Decode(&ev); err != nil || ev.Issuer == "" || ev.Subject == "" || len(ev.Issuer) > 500 || len(ev.Subject) > 500 || (ev.Reason != "signed_out" && ev.Reason != "removed") {
		return nil, &peoplelink.Refusal{Code: CodeLinkMinimum, Message: "the payload has a field, or a value, that PRD-002 Appendix E does not list for " + env.Header.Type}
	}
	return &ev, nil
}

// endSessionsOf ends the sessions of the user behind an identity Nemura says was signed out or removed, and only when that user is mapped to a person of the link: an identity with no user here, or a user no
// person was mapped to, is left exactly as it is, and the caller cannot tell which it was (FR-PPL-18). Nothing but sessions ends: no membership, role or passkey changes. The audit event holds the reason and the count.
func (a *API) endSessionsOf(ctx context.Context, u storage.UnitOfWork, l storage.PeopleLinkRow, ev peoplelink.SessionRevoked, now time.Time) error {
	sessions, ok := a.deps.Installation.(storage.Sessions)
	if !ok || a.deps.Installation == nil {
		return nil
	}
	user, err := a.deps.Installation.UserForIdentity(ctx, ev.Issuer, ev.Subject)
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := u.PeopleLinks().PersonOfUser(ctx, user); errors.Is(err, ports.ErrNotFound) {
		return nil // not mapped to a person of the link: not Nemura's to sign out
	} else if err != nil {
		return err
	}
	n, err := sessions.EndSessionsOfUser(ctx, user, now)
	if err != nil {
		return err
	}
	return u.Audit().Record(ctx, storage.AuditRecord{Action: "people_link.sessions_ended", ObjectType: "people_link", ObjectID: l.ID,
		Changes: map[string][2]any{"reason": {nil, ev.Reason}, "sessions_ended": {nil, n}}})
}
