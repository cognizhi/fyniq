package linksend

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// AskScope is the one scope Fyniq's read-only link client holds at Nemura's OAuth server: it reaches the read of a hold's status and nothing that writes (PRD-001 FR-PPL-13).
const AskScope = "link.holds.read"

// HoldTimeout is how long Fyniq waits for Nemura's answer at a release: no answer within it leaves the person's lines waiting (PRD-001 FR-PPL-13; p1-payroll-link design D10).
const HoldTimeout = 5 * time.Second

// HoldAnswer is Nemura's answer about a person's hold: held, released or none, the hold's ID and its latest version.
type HoldAnswer struct {
	Status  string `json:"status"`
	HoldID  string `json:"hold_id"`
	Version int    `json:"version"`
}

// HoldAsker asks Nemura, the hold's authority, whether a person is held now (FR-PPL-13). It uses the link's read-only client and key, never the worker's (the one that sends events): the API holds the
// first and not the second. Nemura's API URL, token endpoint and audience are the deployment's settings; the call is bounded by HoldTimeout and its failure is an error the caller must read as "wait".
type HoldAsker struct {
	HTTP     *http.Client // through internal/egress, to Nemura's API and token hosts only
	APIURL   string
	TokenURL string
	Audience string
	Clock    ports.Clock

	mu     sync.Mutex
	tokens map[uuid.UUID]*hydra.TokenSource
}

// Ask asks for one person, by Nemura's person ID, with the link's ask key.
func (h *HoldAsker) Ask(ctx context.Context, key storage.PeopleLinkAskKey, person uuid.UUID) (HoldAnswer, error) {
	if h == nil || h.HTTP == nil || h.APIURL == "" || h.TokenURL == "" || h.Audience == "" {
		return HoldAnswer{}, errors.New("the link's endpoints are not configured for the ask at release")
	}
	ctx, cancel := context.WithTimeout(ctx, HoldTimeout)
	defer cancel()
	token, err := h.token(ctx, key, AskScope)
	if err != nil {
		return HoldAnswer{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.APIURL+"/v1/link/people/"+url.PathEscape(person.String())+"/hold", nil)
	if err != nil {
		return HoldAnswer{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := h.HTTP.Do(req)
	if err != nil {
		return HoldAnswer{}, fmt.Errorf("Nemura did not answer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<14))
	if resp.StatusCode != http.StatusOK {
		return HoldAnswer{}, fmt.Errorf("Nemura answered %d", resp.StatusCode)
	}
	var out HoldAnswer
	if json.Unmarshal(raw, &out) != nil || (out.Status != "held" && out.Status != "released" && out.Status != "none") {
		return HoldAnswer{}, errors.New("Nemura's answer is not a hold's status")
	}
	return out, nil
}

// token is the read-only client's token for a scope: made with the link's ask key and kept by scope until it is nearly out of date.
func (h *HoldAsker) token(ctx context.Context, key storage.PeopleLinkAskKey, scope string) (string, error) {
	der, err := x509.ParsePKCS8PrivateKey(key.PrivateDER)
	if err != nil {
		return "", errors.New("the link's ask key cannot be read")
	}
	ec, ok := der.(*ecdsa.PrivateKey)
	if !ok {
		return "", errors.New("the link's ask key is not an EC key")
	}
	h.mu.Lock()
	if h.tokens == nil {
		h.tokens = map[uuid.UUID]*hydra.TokenSource{}
	}
	ts, have := h.tokens[key.LinkID]
	if !have {
		ts = &hydra.TokenSource{TokenURL: h.TokenURL, ClientID: key.ClientID, Key: ec, Audience: h.Audience, HTTP: h.HTTP, Clock: h.Clock}
		h.tokens[key.LinkID] = ts
	}
	h.mu.Unlock()
	return ts.Token(ctx, scope)
}

// SyncScope is the second scope Fyniq's read-only link client holds at Nemura's OAuth server (the owner's decision of 21 Nov 2026): it reads the link's event log by identity, for gap detection and health.
const SyncScope = "link.sync.read"

// SyncEvent is one event Nemura queued, by its identity: never a payload.
type SyncEvent struct {
	Sequence      int64  `json:"sequence"`
	EventID       string `json:"event_id"`
	Type          string `json:"type"`
	ObjectType    string `json:"object_type"`
	ObjectID      string `json:"object_id"`
	ObjectVersion int    `json:"object_version"`
	State         string `json:"state"`
}

// SyncPage is a page of the log Nemura says it queued for Fyniq, and the highest sequence it has queued.
type SyncPage struct {
	Epoch           int         `json:"epoch"`
	HighestSequence int64       `json:"highest_sequence"`
	Events          []SyncEvent `json:"events"`
	NextCursor      string      `json:"next_cursor"`
}

// Sync reads a page of the link's event log, with the ask key's other scope: what Fyniq compares with its inbox to find a gap (the replay request is the events' client's, and the worker's). It is bounded like the
// ask and its failure is an error the caller reads as "not known now".
func (h *HoldAsker) Sync(ctx context.Context, key storage.PeopleLinkAskKey, cursor string) (SyncPage, error) {
	if h == nil || h.HTTP == nil || h.APIURL == "" || h.TokenURL == "" || h.Audience == "" {
		return SyncPage{}, errors.New("the link's endpoints are not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*HoldTimeout)
	defer cancel()
	token, err := h.token(ctx, key, SyncScope)
	if err != nil {
		return SyncPage{}, err
	}
	u := h.APIURL + "/v1/link/sync?page_size=500"
	if cursor != "" {
		u += "&cursor=" + url.QueryEscape(cursor)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return SyncPage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := h.HTTP.Do(req)
	if err != nil {
		return SyncPage{}, fmt.Errorf("Nemura did not answer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return SyncPage{}, fmt.Errorf("Nemura answered %d", resp.StatusCode)
	}
	var out SyncPage
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields() // a field Nemura was never to send (a payload) is an error, not something to keep
	if err := d.Decode(&out); err != nil {
		return SyncPage{}, errors.New("Nemura's answer is not the link's event log by identity")
	}
	return out, nil
}
