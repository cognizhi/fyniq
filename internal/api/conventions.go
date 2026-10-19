package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ Idempotency-Key (FR-INT-08)

// IdempotencyTTL is how long a key is kept.
const IdempotencyTTL = 24 * time.Hour

// retries is how many times a write runs when the database asks for it again (ENG-47).
const retries = 3

// KeyHeader is the Idempotency-Key header of a write.
type KeyHeader struct {
	IdempotencyKey string `header:"Idempotency-Key" minLength:"1" maxLength:"255" pattern:"^[\\x21-\\x7E]+$" doc:"A key you choose (a UUID is best) that makes the request safe to send again for 24 hours: a repeat returns the original response, and the same key with a different request returns 422 (FR-INT-08)." example:"5b1a6f0e-8d0c-4c1e-9b7a-2f6d3c8e1a90"`
}

// written is the outcome of a write: its body, the status it was first answered with, and whether
// this answer is a replay.
type written[B any] struct {
	Body     B
	Status   int
	Replayed bool
}

// write runs a command as one unit of work for the caller's organisation, with its Idempotency-Key:
// the key is claimed, the command runs, and the response is saved, all in the same unit of work
// (the command's own unit of work joins it), so they commit together or not at all. A repeat with the
// same key and request gets the saved response; nothing runs again. A failed command saves nothing,
// so the key can be used again. request is what the key's hash covers.
func write[B any](ctx context.Context, a *API, key string, request any, status int,
	run func(ctx context.Context, u storage.UnitOfWork) (B, error)) (written[B], error) {
	p, _ := authn.From(ctx)
	var out written[B]
	hash, err := requestHash(ctx, request)
	if err != nil {
		return out, err
	}
	for attempt := 1; ; attempt++ {
		out = written[B]{Status: status}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if key != "" {
				now := a.deps.Clock.Now()
				earlier, err := u.Idempotency().Claim(ctx, storage.IdempotencyClaim{Client: p.Client + " " + p.Subject, Key: key,
					RequestHash: hash, CreatedAt: now, ExpiresAt: now.Add(IdempotencyTTL)})
				if err != nil {
					return err
				}
				if earlier != nil {
					return replay(a, earlier, hash, &out)
				}
			}
			body, err := run(ctx, u)
			if err != nil {
				return err
			}
			out.Body = body
			if key == "" {
				return nil
			}
			saved, err := json.Marshal(body)
			if err != nil {
				return err
			}
			return u.Idempotency().Complete(ctx, p.Client+" "+p.Subject, key, status, saved)
		})
		var se *storage.Error
		if err != nil && errors.As(err, &se) && se.Retryable() && attempt < retries {
			continue
		}
		return out, err
	}
}

func replay[B any](a *API, earlier *storage.StoredResponse, hash []byte, out *written[B]) error {
	switch {
	case !bytes.Equal(earlier.RequestHash, hash):
		return problem(CodeIdempotencyReused, "This Idempotency-Key was sent before with a different request.",
			"Use a new key for a new request; send the same request to repeat it.")
	case !earlier.ExpiresAt.After(a.deps.Clock.Now()):
		return problem(CodeIdempotencyOld, fmt.Sprintf("This Idempotency-Key expired at %s.", earlier.ExpiresAt.UTC().Format(time.RFC3339)),
			"Use a new key: keys are kept for 24 hours.")
	case earlier.Status == 0:
		return problem(CodeIdempotencyBusy, "The first request with this Idempotency-Key has not finished.",
			"Wait a moment and send the same request again.").withHeader("Retry-After", "1")
	}
	out.Status, out.Replayed = earlier.Status, true
	return json.Unmarshal(earlier.Body, &out.Body)
}

// requestHash is SHA-256 over the operation and the request (its path parameters, body and
// preconditions), so the same key with anything else is refused.
func requestHash(ctx context.Context, request any) ([]byte, error) {
	b, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	op, _ := ctx.Value(operationKey{}).(string)
	h := sha256.New()
	h.Write([]byte(op + "\n"))
	h.Write(b)
	return h.Sum(nil), nil
}

type operationKey struct{}

// replayedHeader is the value of Idempotent-Replayed on a replayed answer.
func replayedHeader(replayed bool) string {
	if replayed {
		return "true"
	}
	return ""
}

// ------------------------------------------------------------------ ETag and If-Match (FR-INT-09)

// IfMatchHeader is the precondition of a command that changes a versioned record.
type IfMatchHeader struct {
	IfMatch string `header:"If-Match" doc:"The record's ETag from your last read; the command runs only if the record has not changed since (412 otherwise; 428 without it)." example:"\"1-draft\""`
}

// etag is a strong entity tag over a record's version and, for documents, its status.
func etag(parts ...any) string {
	s := make([]string, len(parts))
	for i, p := range parts {
		s[i] = fmt.Sprint(p)
	}
	return `"` + strings.Join(s, "-") + `"`
}

// checkIfMatch compares an If-Match header with the record's current ETag (RFC 9110 §13.1.1: strong
// comparison; "*" matches any current record).
func checkIfMatch(header, current string) error {
	if strings.TrimSpace(header) == "" {
		return problem(CodeIfMatchRequired, "This command changes a versioned record, so it needs If-Match.",
			"Read the record and send its ETag in If-Match.")
	}
	for _, tag := range strings.Split(header, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" || tag == current {
			return nil
		}
	}
	return problem(CodeStale, fmt.Sprintf("The record changed since you read it: it is now %s.", current),
		"Read the record again, check the change, and retry with its new ETag.",
		FieldProblem{Code: CodeStale, Field: "header.If-Match", Message: "names an older version", Fix: "Send the current ETag"})
}

// ------------------------------------------------------------------ cursor lists (FR-INT-10)

// PageQuery is the paging of a list.
type PageQuery struct {
	PageSize int    `query:"page_size" minimum:"1" maximum:"500" default:"100" doc:"How many items per page, up to 500." example:"100"`
	Cursor   string `query:"cursor" maxLength:"1000" doc:"The next_cursor of the previous page; leave it out for the first page." example:"eyJhZnRlciI6IjEwMDAifQ"`
}

type cursor struct {
	After string `json:"after"`
}

func encodeCursor(after string) string {
	b, _ := json.Marshal(cursor{After: after})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	var c cursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	if err != nil || c.After == "" {
		return "", badCursor()
	}
	return c.After, nil
}

func badCursor() error {
	return problem(CodeCursor, "The cursor is not one this list gave.",
		"Send the next_cursor of the previous page exactly as it came, or leave cursor out for the first page.",
		FieldProblem{Code: CodeCursor, Field: "query.cursor", Message: "is not a cursor of this list", Fix: "Send next_cursor as it came"})
}

// pageOf returns one page of a list held in memory, in the list's own order, and the cursor of the
// next page, which names the page's last item by its key (FR-INT-10).
func pageOf[T any](items []T, key func(T) string, q PageQuery) ([]T, string, error) {
	after, err := decodeCursor(q.Cursor)
	if err != nil {
		return nil, "", err
	}
	if after != "" {
		i := slices.IndexFunc(items, func(t T) bool { return key(t) == after })
		if i < 0 {
			return nil, "", badCursor()
		}
		items = items[i+1:]
	}
	size := q.PageSize
	if size <= 0 {
		size = 100
	}
	if len(items) > size {
		return items[:size], encodeCursor(key(items[size-1])), nil
	}
	return items, "", nil
}

// ------------------------------------------------------------------ exemptions

// exempt opts op out of one convention of §8.18.1 (Idempotency-Key, If-Match or cursor), with the
// reason, as x-fyniq-exempt. The contract lint (contracts/openapi/spectral.yaml) accepts no other
// way out, and TestConventionExemptions lists every exemption, so a new one is reviewed.
func exempt(op huma.Operation, convention, reason string) huma.Operation {
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	ex, _ := op.Extensions["x-fyniq-exempt"].(map[string]string)
	if ex == nil {
		ex = map[string]string{}
	}
	ex[convention] = reason
	op.Extensions["x-fyniq-exempt"] = ex
	return op
}

// oneTimeSecret exempts a command whose response carries a one-time secret from Idempotency-Key
// (FR-INT-08, rev 0.27): a stored response would keep the secret at rest, and the code already makes
// the command one-time.
func oneTimeSecret(op huma.Operation) huma.Operation {
	return exempt(op, "Idempotency-Key", "Its response carries a one-time secret, which a stored response would keep at rest; "+
		"the code already makes the command one-time (FR-INT-08, rev 0.27).")
}

// ------------------------------------------------------------------ deprecation (FR-INT-06)

// sunsetNoticeDays is the shortest six calendar months, which oasdiff enforces as the least time
// between deprecating an operation and its sunset (--deprecation-days-stable, QA-API-02). deprecate
// checks the six months themselves, from the date of the deprecation.
const sunsetNoticeDays = 181

// deprecate marks op deprecated on the date on (YYYY-MM-DD), with the date of its sunset, at least
// six months later, and the page that says what to use instead. Every response of the operation then
// carries Deprecation (RFC 9745), Sunset (RFC 8594) and a Link to the page, and the contract documents
// them, with x-sunset for oasdiff, which refuses to drop the operation before that date.
func deprecate(op huma.Operation, on, sunset, note string) huma.Operation {
	from, err1 := time.Parse(time.DateOnly, on)
	until, err2 := time.Parse(time.DateOnly, sunset)
	if err := errors.Join(err1, err2); err != nil {
		panic("api: " + op.OperationID + ": " + err.Error())
	}
	if until.Before(from.AddDate(0, 6, 0)) {
		panic(fmt.Sprintf("api: %s: its sunset %s is less than six months after %s (FR-INT-06)", op.OperationID, sunset, on))
	}
	if !strings.HasPrefix(note, "https://") {
		panic("api: " + op.OperationID + ": the deprecation note must be an https URL")
	}
	op.Deprecated = true
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions["x-fyniq-deprecated-on"] = on
	op.Extensions["x-sunset"] = sunset
	op.Extensions["x-fyniq-deprecation-note"] = note
	return op
}

// deprecationHeaders are the headers of a deprecated operation's responses, or nil.
func deprecationHeaders(op *huma.Operation) map[string]string {
	if op == nil || !op.Deprecated {
		return nil
	}
	on, _ := op.Extensions["x-fyniq-deprecated-on"].(string)
	sunset, _ := op.Extensions["x-sunset"].(string)
	note, _ := op.Extensions["x-fyniq-deprecation-note"].(string)
	from, _ := time.Parse(time.DateOnly, on)
	until, _ := time.Parse(time.DateOnly, sunset)
	return map[string]string{
		"Deprecation": fmt.Sprintf("@%d", from.Unix()),
		"Sunset":      until.UTC().Format(http.TimeFormat),
		"Link":        "<" + note + `>; rel="deprecation"; type="text/html"`,
	}
}

// describeDeprecation documents the deprecation headers on every response of a deprecated operation.
func describeDeprecation(_ *huma.OpenAPI, op *huma.Operation) {
	h := deprecationHeaders(op)
	if h == nil {
		return
	}
	docs := map[string]string{
		"Deprecation": "When this operation was deprecated, as a structured date (RFC 9745).",
		"Sunset":      "When this operation stops working (RFC 8594): at least six months after its deprecation (FR-INT-06).",
		"Link":        "The page that says what to use instead (rel=\"deprecation\").",
	}
	for _, r := range op.Responses {
		if r.Headers == nil {
			r.Headers = map[string]*huma.Param{}
		}
		for _, name := range []string{"Deprecation", "Sunset", "Link"} {
			r.Headers[name] = &huma.Param{Description: docs[name], Schema: &huma.Schema{Type: "string", Examples: []any{h[name]}}}
		}
	}
}
