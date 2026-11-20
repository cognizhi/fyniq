package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/google/uuid"
)

// Idempotency (PRD-001 §8.18.1, golden rule 4, NM-ARC-03): a write sent with an Idempotency-Key runs once. The same key from the same person with the same request returns the first answer, marked
// Idempotent-Replayed; the same key with a different request is 422; two identical requests in flight at once run once, the second waiting for the first. A key is kept for 24 hours. The answer of an
// operation that may carry pay, a bank account, an NRIC or the text of a question is never stored: only its status is, so a replay of such an operation that returned a body is refused in words with 409 and
// the person reads the result from the record instead.

// IdempotencyWindow is how long a key is kept.
const IdempotencyWindow = 24 * time.Hour

// Codes of the idempotency refusals.
const (
	CodeIdempotencyMismatch = "NM_IDEMPOTENCY_MISMATCH"
	CodeIdempotencyConflict = "NM_IDEMPOTENCY_CONFLICT"
)

// MaxIdempotentBody is the largest request body that is hashed and kept for comparison: a larger request runs without a key's protection and says so in the contract.
const MaxIdempotentBody = 1 << 20

// MaxStoredResponse is the largest answer that is stored for a replay.
const MaxStoredResponse = 64 << 10

// inFlightWait is how long a request waits for an identical one that is still running before it answers 409.
const inFlightWait = 15 * time.Second

// IdemRecord is what is kept of a finished answer.
type IdemRecord struct {
	Status                      int
	ContentType, Location, ETag string
	Body                        []byte
	HadBody                     bool // the answer had a body
	BodyKept                    bool // and it was stored: a replay can return it
}

// IdemClaim is the result of claiming a key.
type IdemClaim struct {
	Owned    bool // the caller owns the key and runs the request
	Mismatch bool // the key was used for a different request
	InFlight bool // an identical request is running
	Rec      IdemRecord
}

// Idempotency is Nemura's table of keys, written by the API's own database role. Keys are scoped by organisation and person; nothing sensitive is stored.
type Idempotency interface {
	Claim(ctx context.Context, org, principal uuid.UUID, key string, hash []byte, now time.Time) (IdemClaim, error)
	Finish(ctx context.Context, org, principal uuid.UUID, key string, rec IdemRecord) error
	Release(ctx context.Context, org, principal uuid.UUID, key string) error
}

// sensitiveOperation says whether an operation's answer may carry pay, a bank account, an NRIC, a file of pay, or the text of a question: its body is never stored.
func sensitiveOperation(pattern string) bool {
	p := strings.ToLower(pattern)
	for _, s := range []string{"/payroll", "/pay", "pr-election", "tax-hold", "nric", "ir8a", "bank-account", "/assistant/", "/leave/settlement", "/exports", "/me/questions", "/questions/", "/details", "/emergency"} {
		if strings.Contains(p, s) {
			return true
		}
	}
	return false
}

type idemMux struct {
	humago.Mux
	a *API
}

func (m idemMux) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	method, _, ok := strings.Cut(pattern, " ")
	if ok && method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		m.Mux.HandleFunc(pattern, m.a.idempotent(pattern, h))
		return
	}
	m.Mux.HandleFunc(pattern, h)
}

// addIdempotencyHeader documents the Idempotency-Key header on every write that takes one.
func addIdempotencyHeader(_ *huma.OpenAPI, op *huma.Operation) {
	if op.Method == http.MethodGet || op.Method == http.MethodHead || op.Method == http.MethodOptions {
		return
	}
	if _, exempt := IdempotencyExempt[op.Method+" "+op.Path]; exempt {
		return
	}
	max := 255
	op.Parameters = append(op.Parameters, &huma.Param{Name: "Idempotency-Key", In: "header", Required: false, Example: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1faa",
		Description: "A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat from the same person returns the first answer with `Idempotent-Replayed: true`, the same key with a different request returns 422, and two identical requests at once run once. An operation whose answer may carry pay, a bank account or an NRIC does not store its answer: a repeat of one that returned a body answers 409, and the result is read from the record.",
		Schema:      &huma.Schema{Type: "string", MinLength: new(int), MaxLength: &max, Examples: []any{"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1faa"}}})
}

func validKey(k string) bool {
	if k == "" || len(k) > 255 {
		return false
	}
	for _, c := range k {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

func writeProblem(w http.ResponseWriter, p *Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// capture records an answer while the operation writes it.
type capture struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (c *capture) Header() http.Header         { return c.header }
func (c *capture) WriteHeader(code int)        { c.status = code }
func (c *capture) Write(b []byte) (int, error) { return c.body.Write(b) }

func replay(w http.ResponseWriter, rec IdemRecord, replayed bool) {
	h := w.Header()
	if rec.ContentType != "" {
		h.Set("Content-Type", rec.ContentType)
	}
	if rec.Location != "" {
		h.Set("Location", rec.Location)
	}
	if rec.ETag != "" {
		h.Set("ETag", rec.ETag)
	}
	if replayed {
		h.Set("Idempotent-Replayed", "true")
	}
	w.WriteHeader(rec.Status)
	_, _ = w.Write(rec.Body)
}

// idempotent wraps a write so that its Idempotency-Key is honoured. A request with no key, no session, or an operation that takes no key runs as it is.
func (a *API) idempotent(pattern string, h func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		_, exempt := IdempotencyExempt[pattern]
		if key == "" || exempt || a.d.Idempotency == nil {
			h(w, r)
			return
		}
		if !validKey(key) {
			writeProblem(w, Err(CodeInvalid, "Idempotency-Key: use 1 to 255 printable characters without spaces, such as a UUID."))
			return
		}
		ctx := r.Context()
		s, err := a.d.Sessions.Authenticate(ctx, a.cookie(r.Header.Get("Cookie")), a.d.Clock.Now())
		if err != nil {
			h(w, r) // no valid session: the operation answers 401 as it would without a key
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, MaxIdempotentBody+1))
		if err != nil {
			h(w, r)
			return
		}
		if len(raw) > MaxIdempotentBody {
			r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), r.Body))
			h(w, r)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		sum := sha256.New()
		sum.Write([]byte(r.Method + "\n" + r.URL.RequestURI() + "\n"))
		sum.Write(raw)
		hash := sum.Sum(nil)
		deadline := a.d.Clock.Now().Add(inFlightWait)
		for {
			claim, err := a.d.Idempotency.Claim(ctx, s.Organisation, s.Member, key, hash, a.d.Clock.Now())
			if err != nil {
				writeProblem(w, Err(CodeUnavailable, "The key could not be checked, so nothing was done: send the request again."))
				return
			}
			switch {
			case claim.Mismatch:
				writeProblem(w, Err(CodeIdempotencyMismatch, "This Idempotency-Key was used for a different request: use a new key for a new request, or send the same request again."))
				return
			case claim.InFlight:
				if !a.d.Clock.Now().Before(deadline) {
					writeProblem(w, Err(CodeIdempotencyConflict, "An identical request is still running: wait a moment and read the record before sending it again."))
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(25 * time.Millisecond):
				}
				continue
			case claim.Owned:
				a.runOwned(w, r, pattern, h, s, key)
				return
			default:
				if claim.Rec.HadBody && !claim.Rec.BodyKept {
					writeProblem(w, Err(CodeIdempotencyConflict, "This request already ran with this key, and its answer is not kept because it may carry pay or restricted data: read the result from the record."))
					return
				}
				replay(w, claim.Rec, true)
				return
			}
		}
	}
}

func (a *API) runOwned(w http.ResponseWriter, r *http.Request, pattern string, h func(http.ResponseWriter, *http.Request), s Session, key string) {
	c := &capture{header: http.Header{}, status: http.StatusOK}
	defer func() {
		if p := recover(); p != nil {
			_ = a.d.Idempotency.Release(context.WithoutCancel(r.Context()), s.Organisation, s.Member, key)
			panic(p)
		}
	}()
	h(c, r)
	rec := IdemRecord{Status: c.status, ContentType: c.header.Get("Content-Type"), Location: c.header.Get("Location"), ETag: c.header.Get("ETag"), HadBody: c.body.Len() > 0}
	if c.status >= 500 {
		// A failure is not an answer to keep: the same key may try again.
		_ = a.d.Idempotency.Release(context.WithoutCancel(r.Context()), s.Organisation, s.Member, key)
	} else {
		if rec.HadBody && !sensitiveOperation(pattern) && c.body.Len() <= MaxStoredResponse {
			rec.Body, rec.BodyKept = c.body.Bytes(), true
		}
		if err := a.d.Idempotency.Finish(context.WithoutCancel(r.Context()), s.Organisation, s.Member, key, rec); err != nil {
			_ = a.d.Idempotency.Release(context.WithoutCancel(r.Context()), s.Organisation, s.Member, key)
		}
	}
	for k, v := range c.header {
		w.Header()[k] = v
	}
	w.WriteHeader(c.status)
	_, _ = w.Write(c.body.Bytes())
}
