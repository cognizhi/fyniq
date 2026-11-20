// Package webhooks signs and sends Fyniq's outbound webhooks (PRD-001 FR-INT-12; p2-api-third-parties design D1 to D4). A delivery is a CloudEvents 1.0 structured
// event signed as Standard Webhooks (standardwebhooks.com, retrieved 21 Nov 2026): the headers webhook-id (the same on every attempt), webhook-timestamp (seconds) and
// webhook-signature ("v1,<base64 HMAC-SHA256 of id.timestamp.body>", one signature for each secret in force, space separated), the secret "whsec_" and the base64 of its
// key. The signature is made at each attempt with a fresh timestamp, so a retry is inside the receiver's five-minute window. Nothing here reaches the network except
// through the egress guard's client the caller passes in.
package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// SecretPrefix begins every signing secret.
const SecretPrefix = "whsec_"

// ReplayWindow is how far a receiver should let a timestamp be from its own clock.
const ReplayWindow = 5 * time.Minute

// NewSecret makes a signing secret: 32 random bytes, base64, after the prefix.
func NewSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return SecretPrefix + base64.StdEncoding.EncodeToString(b), nil
}

// Hint is a secret's last four characters, to tell two apart without showing it.
func Hint(secret string) string {
	if len(secret) < 4 {
		return "????"
	}
	return secret[len(secret)-4:]
}

func key(secret string) ([]byte, error) {
	raw, ok := strings.CutPrefix(secret, SecretPrefix)
	if !ok {
		return nil, errors.New("webhooks: a signing secret begins with whsec_")
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(b) < 16 {
		return nil, errors.New("webhooks: the signing secret is not base64 of a key")
	}
	return b, nil
}

// Sign is the v1 signature of a message: HMAC-SHA256 of "id.timestamp.body" under the secret's key.
func Sign(secret, id string, at time.Time, body []byte) (string, error) {
	k, err := key(secret)
	if err != nil {
		return "", err
	}
	h := hmac.New(sha256.New, k)
	_, _ = fmt.Fprintf(h, "%s.%d.", id, at.Unix())
	h.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

// Header is the webhook-signature value for the secrets in force: one signature each, space separated.
func Header(secrets []string, id string, at time.Time, body []byte) (string, error) {
	var out []string
	for _, s := range secrets {
		sig, err := Sign(s, id, at, body)
		if err != nil {
			return "", err
		}
		out = append(out, sig)
	}
	return strings.Join(out, " "), nil
}

// Verify is what a receiver does: the timestamp within the window of now, and one of the header's signatures equal to the message's under the secret. It is here so the
// tests, the sandbox and the SDKs' documentation use one reference implementation.
func Verify(secret, id, timestamp, header string, body []byte, now time.Time) error {
	sec, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errors.New("webhooks: the timestamp is not a number")
	}
	at := time.Unix(sec, 0)
	if d := now.Sub(at); d > ReplayWindow || d < -ReplayWindow {
		return errors.New("webhooks: the timestamp is outside the replay window")
	}
	want, err := Sign(secret, id, at, body)
	if err != nil {
		return err
	}
	for _, got := range strings.Fields(header) {
		if hmac.Equal([]byte(got), []byte(want)) {
			return nil
		}
	}
	return errors.New("webhooks: no signature matches")
}

// CloudEvent is the structured CloudEvents 1.0 envelope of a delivery (cloudevents.io spec 1.0.2).
type CloudEvent struct {
	SpecVersion     string          `json:"specversion"`
	ID              string          `json:"id"`
	Source          string          `json:"source"`
	Type            string          `json:"type"`
	Subject         string          `json:"subject,omitempty"`
	Time            string          `json:"time"`
	DataContentType string          `json:"datacontenttype"`
	Data            json.RawMessage `json:"data"`
}

// Event is the body of a delivery: the type, the data (the event's IDs and times), the source (the installation's origin) and the subject (the record's ID).
func Event(id, eventType, source, subject string, at time.Time, data json.RawMessage) ([]byte, error) {
	return json.Marshal(CloudEvent{SpecVersion: "1.0", ID: id, Source: source, Type: eventType, Subject: subject, Time: at.UTC().Format(time.RFC3339), DataContentType: "application/json", Data: data})
}

// VerifyType is the event an endpoint is sent to prove it is the organisation's: its data holds a challenge the endpoint echoes in its answer.
const VerifyType = "fyniq.webhook.verify.v1"

// deliverable are the event types an endpoint may ask for: operational and approval events, whose data is IDs and times. The security, personal-data and payee events,
// a person's own (a mention, a missing timesheet or receipt), the investors' and the AI's are never delivered outside (design D4).
var deliverable = []string{
	"fyniq.approval.decided.v1", "fyniq.approval.requested.v1", "fyniq.bills.due.v1", "fyniq.cash.below_buffer.v1", "fyniq.cash.buffer_recovered.v1",
	"fyniq.deadline.overdue.v1", "fyniq.deadline.reminder.v1", "fyniq.jobs.failed.v1", "fyniq.kpi.anomaly.v1", "fyniq.ledger.posted.v1", "fyniq.period.closed.v1",
	"fyniq.period.reopened.v1", "fyniq.recurring.made.v1", "fyniq.reports.export_ready.v1", "fyniq.reports.scheduled.v1",
}

// Deliverable says whether an event type may be asked for by an endpoint.
func Deliverable(eventType string) bool { return slices.Contains(deliverable, eventType) }

// DeliverableTypes are the types an endpoint may ask for, sorted.
func DeliverableTypes() []string { return slices.Sorted(slices.Values(deliverable)) }

// The retry schedule (design D3): the delay after the nth failed attempt, the last repeated, until three days from the first attempt.
var schedule = []time.Duration{5 * time.Second, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 5 * time.Hour, 10 * time.Hour, 12 * time.Hour}

// Lifetime is how long a delivery is retried from its first attempt.
const Lifetime = 72 * time.Hour

// Next is when the delivery tried n times (n >= 1) whose first attempt was at first is tried again after failing at now; false when its three days are used up.
func Next(n int, first, now time.Time) (time.Time, bool) {
	d := schedule[min(max(n, 1), len(schedule))-1]
	at := now.Add(d)
	if at.After(first.Add(Lifetime)) {
		return time.Time{}, false
	}
	return at, true
}

// Result is the answer to an attempt.
type Result struct {
	Status   int
	Body     []byte // up to the first KiB of the answer
	Duration time.Duration
}

// Send posts a delivery to its endpoint through client (the egress guard's, reaching that endpoint's host alone): the structured CloudEvent, signed afresh at now for every
// secret in force. A connection that fails is an error with no Result; any answer, whatever its status, is a Result.
func Send(ctx context.Context, client *http.Client, clock ports.Clock, target string, id string, now time.Time, body []byte, secrets []string) (Result, error) {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return Result{}, errors.New("webhooks: the endpoint's address is not a web address")
	}
	sig, err := Header(secrets, id, now, body)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/cloudevents+json; charset=utf-8")
	req.Header.Set("User-Agent", "Fyniq-Webhooks/1")
	req.Header.Set("webhook-id", id)
	req.Header.Set("webhook-timestamp", strconv.FormatInt(now.Unix(), 10))
	req.Header.Set("webhook-signature", sig)
	start := clock.Now()
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return Result{Status: resp.StatusCode, Body: b, Duration: clock.Now().Sub(start)}, nil
}
