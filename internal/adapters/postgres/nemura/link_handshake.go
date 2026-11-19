package nemura

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/apiclient"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/link"
	"github.com/cognizhi/fyniq/internal/nemura/linkauth"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
	"github.com/cognizhi/fyniq/internal/ports"
)

// ------------------------------------------------------------------ the link's handshake and its inbox door (NM-ARC-04, NM-INT-10, NM-INT-11)

// The refusals of the link's ceremony, as plain Go errors until the page that shows them arrives.
const (
	CodeLinkApprover = "NM_LINK_SECOND_APPROVER" // the link needs an Owner to ask and another Owner or Administrator to approve
	CodeLinkStepUp   = "NM_LINK_STEP_UP"         // a passkey confirmed in the last five minutes is needed
	CodeLinkState    = "NM_LINK_STATE"           // there is no proposed link to approve, or its state does not allow it
)

// LinkOffer is what Nemura gives Fyniq to link with: its organisation, the envelope key and the client key as public halves, and the ID Fyniq's client will have at Nemura's own
// OAuth server. Nothing in it is secret.
type LinkOffer struct {
	Link               uuid.UUID
	Epoch              int
	NemuraOrganisation uuid.UUID
	LinkKey            ed25519.PublicKey
	ClientKey          json.RawMessage // the JWK Fyniq registers for Nemura's client at Fyniq's OAuth server
	PeerClientID       string
	PeerAskClientID    string // the ID of Fyniq's read-only client for the ask at release
}

// ProposeLink starts a link to a Fyniq organisation: it makes Nemura's two keys (the envelope key, which signs what Nemura sends, and the ES256 client key, which signs the assertion that gets
// Nemura's token from Fyniq's OAuth server) and the ID Fyniq's client will have here, and keeps them as a link that is only proposed until Fyniq's half is known (ActivateLink). The private halves go
// to the secret table, which only the worker reads.
func ProposeLink(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, actor audit.Actor, proposer, fyniqOrg uuid.UUID) (LinkOffer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return LinkOffer{}, err
	}
	ck, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return LinkOffer{}, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(ck)
	if err != nil {
		return LinkOffer{}, err
	}
	jwk, err := (&jose.JSONWebKey{Key: &ck.PublicKey, Algorithm: string(jose.ES256), Use: "sig"}).MarshalJSON()
	if err != nil {
		return LinkOffer{}, err
	}
	var epoch int
	if err := tx.QueryRow(ctx, "SELECT coalesce(max(epoch), 0) + 1 FROM nemura.fyniq_link").Scan(&epoch); err != nil {
		return LinkOffer{}, err
	}
	id, peer, ask := uuid.Must(uuid.NewV7()), "fyniq-link-"+uuid.NewString(), "fyniq-ask-"+uuid.NewString()
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.fyniq_link (id, state, epoch, fyniq_organisation_id, fyniq_key, link_public, peer_client_id, peer_ask_client_id, proposed_by, created_at, changed_at) VALUES ($1, 'proposed', $2, $3, NULL, $4, $5, $6, $7, $8, $8)`,
		id, epoch, fyniqOrg, []byte(pub), peer, ask, proposer, at); err != nil {
		return LinkOffer{}, linkError(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.fyniq_link_secret (link_id, link_private, client_private, client_public) VALUES ($1, $2, $3, $4)`, id, []byte(priv), der, jwk); err != nil {
		return LinkOffer{}, linkError(err)
	}
	// The audit trail names the link, its epoch and that keys exist, never a key.
	return LinkOffer{Link: id, Epoch: epoch, NemuraOrganisation: org, LinkKey: pub, ClientKey: jwk, PeerClientID: peer, PeerAskClientID: ask}, Audit(ctx, tx, org, at, actor, "nemura.fyniq_link.proposed",
		audit.Object{Type: "fyniq_link", ID: id}, map[string]audit.Change{"fyniq_organisation": {nil, fyniqOrg.String()}, "epoch": {nil, epoch}, "state": {nil, "proposed"}}, "")
}

// ActivateLink completes a proposed link with Fyniq's half: its envelope key and the ID of Nemura's client at Fyniq's OAuth server. The link is then active at its epoch.
func ActivateLink(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, actor audit.Actor, fyniqKey ed25519.PublicKey, fyniqClientID string) (LinkRow, error) {
	l, err := LiveLink(ctx, tx)
	if err != nil {
		return LinkRow{}, err
	}
	if l.State != "proposed" {
		return LinkRow{}, &payroll.Refusal{Code: CodeLinkState, Message: "There is no proposed link to complete."}
	}
	if len(fyniqKey) != ed25519.PublicKeySize || fyniqClientID == "" || len(fyniqClientID) > 200 {
		return LinkRow{}, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "Fyniq's half of the link is its Ed25519 public key (32 bytes) and the ID of the client Nemura calls with."}
	}
	if _, err := tx.Exec(ctx, `UPDATE nemura.fyniq_link SET state = 'active', fyniq_key = $2, fyniq_client_id = $3, version = version + 1, changed_at = $4 WHERE id = $1`, l.ID, []byte(fyniqKey), fyniqClientID, at); err != nil {
		return LinkRow{}, linkError(err)
	}
	l.State, l.FyniqKey, l.FyniqClientID, l.Version = "active", fyniqKey, fyniqClientID, l.Version+1
	return l, Audit(ctx, tx, org, at, actor, "nemura.fyniq_link.linked", audit.Object{Type: "fyniq_link", ID: l.ID},
		map[string]audit.Change{"state": {"proposed", "active"}, "epoch": {nil, l.Epoch}}, "")
}

// LinkSecrets are the private halves the worker signs with. Only the worker's role can read them.
type LinkSecrets struct {
	Envelope ed25519.PrivateKey
	Client   *ecdsa.PrivateKey
}

// SecretsOf reads the link's private keys. It needs the worker's grant.
func SecretsOf(ctx context.Context, tx pgx.Tx, linkID uuid.UUID) (LinkSecrets, error) {
	var env, client []byte
	if err := tx.QueryRow(ctx, `SELECT link_private, client_private FROM nemura.fyniq_link_secret WHERE link_id = $1`, linkID).Scan(&env, &client); err != nil {
		return LinkSecrets{}, err
	}
	out := LinkSecrets{Envelope: ed25519.PrivateKey(env)}
	if client != nil {
		k, err := x509.ParsePKCS8PrivateKey(client)
		if err != nil {
			return LinkSecrets{}, errors.New("the link's client key cannot be read")
		}
		ec, ok := k.(*ecdsa.PrivateKey)
		if !ok {
			return LinkSecrets{}, errors.New("the link's client key is not an EC key")
		}
		out.Client = ec
	}
	return out, nil
}

// LinkClientRoute implements linkauth.Routes: the link a client of the OAuth server belongs to. The organisation comes from the one global route the database keeps (a token names a client, not an
// organisation); the link's state, epoch and Fyniq organisation are then read inside that organisation, with row-level security on, and only for the link the client was made for.
func (s *Store) LinkClientRoute(ctx context.Context, client string) (linkauth.Route, error) {
	var org, id *uuid.UUID
	var purpose *string
	if err := s.pool.QueryRow(ctx, `SELECT organisation_id, link_id, purpose FROM nemura.link_client_route($1)`, client).Scan(&org, &id, &purpose); err != nil {
		return linkauth.Route{}, err
	}
	if org == nil || id == nil || purpose == nil {
		return linkauth.Route{}, ports.ErrNotFound
	}
	out := linkauth.Route{Organisation: *org, Link: *id, Purpose: *purpose}
	err := s.InOrg(ctx, *org, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT state, epoch, fyniq_organisation_id FROM nemura.fyniq_link WHERE id = $1 AND (peer_client_id = $2 OR peer_ask_client_id = $2)`, *id, client).Scan(&out.State, &out.Epoch, &out.FyniqOrganisation)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return linkauth.Route{}, ports.ErrNotFound
	}
	return out, err
}

// LinkService is the people team's door to the link and Fyniq's door to the inbox (NM-ARC-04, NM-INT-10).
type LinkService struct {
	Store *Store
	OAuth ports.OAuthClients // Nemura's own OAuth server; nil when the deployment has none
	Clock ports.Clock
}

var (
	_ api.LinkInbox = LinkService{}
	_ api.LinkHolds = LinkService{}
)

func (c LinkService) holds(ctx context.Context, s api.Session, now time.Time, roles ...authz.Role) (bool, error) {
	var ok bool
	err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		held, err := ActiveRoles(ctx, tx, s.Member, now)
		for _, r := range roles {
			ok = ok || slices.Contains(held, r)
		}
		return err
	})
	return ok, err
}

// Propose starts the link, as the Owner who asks, with a passkey confirmed in the last five minutes. It returns what Fyniq needs to link.
func (c LinkService) Propose(ctx context.Context, s api.Session, fyniqOrg uuid.UUID) (LinkOffer, error) {
	now := c.Clock.Now()
	if !s.Fresh(now) {
		return LinkOffer{}, &payroll.Refusal{Code: CodeLinkStepUp, Message: "Confirm with your passkey first: linking to Fyniq needs a passkey confirmed in the last five minutes."}
	}
	if ok, err := c.holds(ctx, s, now, authz.Owner); err != nil {
		return LinkOffer{}, err
	} else if !ok {
		return LinkOffer{}, &payroll.Refusal{Code: CodeLinkApprover, Message: "An Owner starts the link, and an Owner or Administrator other than them approves it."}
	}
	var offer LinkOffer
	err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) (err error) {
		offer, err = ProposeLink(ctx, tx, s.Organisation, now, user(s.Member), s.Member, fyniqOrg)
		return err
	})
	return offer, err
}

// Activate approves the proposed link with Fyniq's half, as an Owner or Administrator other than the one who proposed, with a fresh passkey. It registers Fyniq's client at Nemura's own
// OAuth server, with the link scope alone, under the ID the proposal chose, and then the link is active.
func (c LinkService) Activate(ctx context.Context, s api.Session, fyniqKey ed25519.PublicKey, fyniqClientID string, clientKey, askKey json.RawMessage) error {
	if c.OAuth == nil {
		return api.ErrUnavailable
	}
	now := c.Clock.Now()
	if !s.Fresh(now) {
		return &payroll.Refusal{Code: CodeLinkStepUp, Message: "Confirm with your passkey first: approving the link needs a passkey confirmed in the last five minutes."}
	}
	if err := apiclient.CheckKey(clientKey); err != nil {
		return err
	}
	if err := apiclient.CheckKey(askKey); err != nil {
		return err
	}
	var peer, askPeer string
	if err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		l, err := LiveLink(ctx, tx)
		if err != nil {
			return err
		}
		held, err := ActiveRoles(ctx, tx, s.Member, now)
		if err != nil {
			return err
		}
		if l.State != "proposed" {
			return &payroll.Refusal{Code: CodeLinkState, Message: "There is no proposed link to approve."}
		}
		if s.Member == l.ProposedBy || !(slices.Contains(held, authz.Owner) || slices.Contains(held, authz.Administrator)) {
			return &payroll.Refusal{Code: CodeLinkApprover, Message: "A second approver decides: an Owner or Administrator other than the one who proposed the link."}
		}
		peer, askPeer = l.PeerClientID, l.PeerAskClientID
		return nil
	}); err != nil {
		return err
	}
	hydraID, err := c.OAuth.RegisterIntegration(ctx, ports.IntegrationRegistration{ClientID: peer, Name: "Fyniq link", Class: "integration", Audience: apiclient.Audience,
		Scopes: []string{apiclient.LinkScope}, Key: clientKey, Alg: "ES256", TokenLifespan: 15 * time.Minute, Organisation: s.Organisation.String()})
	if err != nil {
		return err
	}
	// The second client, for the ask at release: one scope that only reads a hold's status, its own key, its own ID.
	askID, err := c.OAuth.RegisterIntegration(ctx, ports.IntegrationRegistration{ClientID: askPeer, Name: "Fyniq link (ask)", Class: "integration", Audience: apiclient.Audience,
		Scopes: []string{apiclient.AskScope}, Key: askKey, Alg: "ES256", TokenLifespan: 15 * time.Minute, Organisation: s.Organisation.String()})
	if err != nil {
		_ = c.OAuth.RemoveClient(context.WithoutCancel(ctx), hydraID)
		return err
	}
	if err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		_, err := ActivateLink(ctx, tx, s.Organisation, now, user(s.Member), fyniqKey, fyniqClientID)
		return err
	}); err != nil {
		_ = c.OAuth.RemoveClient(context.WithoutCancel(ctx), hydraID) // no client the OAuth server holds without a link that uses it
		_ = c.OAuth.RemoveClient(context.WithoutCancel(ctx), askID)
		return err
	}
	return nil
}

// Receive is the inbox's door: Fyniq's event, from the link's client, checked against the live link (the same link, epoch and client the token was accepted for) and then received once.
// A refusal after Fyniq's signature held is kept in the inbox, so the transaction commits and the refusal is returned after it.
func (c LinkService) Receive(ctx context.Context, caller linkauth.Caller, raw []byte) (string, error) {
	var env link.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", &link.Refusal{Code: link.CodeMalformed, Message: "the envelope is not well formed"}
	}
	var outcome string
	var refused error
	err := c.Store.InOrg(ctx, caller.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		l, err := LiveLink(ctx, tx)
		if err != nil {
			return err
		}
		if l.ID != caller.Link || l.Epoch != caller.Epoch || l.State != "active" || l.PeerClientID != caller.ClientID {
			return &payroll.Refusal{Code: CodeLinkNone, Message: "The call is not from the link's client at the link's epoch."}
		}
		outcome, err = ReceiveLinkEvent(ctx, tx, caller.Organisation, c.Clock.Now(), env)
		if outcome == "refused" {
			refused = err
			return nil
		}
		return err
	})
	if err != nil {
		return "", err
	}
	return outcome, refused
}

// LinkAdminService is the people team's door to the link, as the API's pages call it: it shows the link, proposes it and approves it, over LinkService's own steps.
type LinkAdminService struct{ LinkService }

var _ api.LinkAdmin = LinkAdminService{}

func (c LinkAdminService) view(ctx context.Context, org uuid.UUID) (api.LinkView, error) {
	var out api.LinkView
	err := c.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		l, err := LiveLink(ctx, tx)
		if noLink(err) {
			return nil
		}
		if err != nil {
			return err
		}
		out = api.LinkView{State: l.State, Epoch: l.Epoch, FyniqOrganisation: l.FyniqOrganisation, NemuraOrganisation: org, LinkKey: l.Public, PeerClientID: l.PeerClientID, PeerAskClientID: l.PeerAskClientID,
			FyniqClientID: l.FyniqClientID, ProposedBy: l.ProposedBy}
		return nil
	})
	return out, err
}

// Current is the live link, or one with no state when there is none. The public half of the client key is shown by the proposal only: it sits in the secret table, whose columns the API cannot read, and
// widening that grant, even to the one public column, is the owners' decision (p1-nemura-fyniq-link, open).
func (c LinkAdminService) Current(ctx context.Context, s api.Session) (api.LinkView, error) {
	return c.view(ctx, s.Organisation)
}

// Propose starts the link and shows it.
func (c LinkAdminService) Propose(ctx context.Context, s api.Session, fyniqOrg uuid.UUID) (api.LinkView, error) {
	offer, err := c.LinkService.Propose(ctx, s, fyniqOrg)
	if err != nil {
		return api.LinkView{}, err
	}
	v, err := c.view(ctx, s.Organisation)
	v.ClientKey = offer.ClientKey // shown once, with the proposal
	return v, err
}

// Approve completes the link with Fyniq's half and shows it.
func (c LinkAdminService) Approve(ctx context.Context, s api.Session, fyniqKey []byte, fyniqClientID string, clientKey, askKey json.RawMessage) (api.LinkView, error) {
	if err := c.Activate(ctx, s, ed25519.PublicKey(fyniqKey), fyniqClientID, clientKey, askKey); err != nil {
		return api.LinkView{}, err
	}
	return c.view(ctx, s.Organisation)
}

// Hold is the answer to Fyniq's ask at release: the hold's latest version for the person, for the asking client of the live link at its epoch (the client that sends events is refused by the token's check and
// here). It reads and changes nothing.
func (c LinkService) Hold(ctx context.Context, caller linkauth.Caller, person uuid.UUID) (api.LinkHold, error) {
	var out api.LinkHold
	err := c.Store.InOrg(ctx, caller.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		l, err := LiveLink(ctx, tx)
		if err != nil {
			return err
		}
		if l.ID != caller.Link || l.Epoch != caller.Epoch || l.State != "active" || l.PeerAskClientID != caller.ClientID || caller.Scope != apiclient.AskScope {
			return &payroll.Refusal{Code: CodeLinkNone, Message: "The call is not from the link's asking client at the link's epoch."}
		}
		h, ok, err := TaxHoldOf(ctx, tx, person)
		if err != nil {
			return err
		}
		out = api.LinkHold{Status: "none"}
		if ok {
			out = api.LinkHold{Status: "released", HoldID: h.HoldID, Version: h.Version}
			if h.Held {
				out.Status = "held"
			}
		}
		return nil
	})
	return out, err
}
