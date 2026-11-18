//go:build local

package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Local mode's model providers (PRD-001 §12.8, "Local AI in replay and live mode"; p1-ai-providers design D12). By
// default a Replay answers each request with the answer recorded for it, so tests and evals are deterministic and
// offline (DEV-02). With FYNIQ_AI_LIVE=1 a live provider answers, on the synthetic sample company only (DEV-08), and a
// Recorder keeps each answer as a cassette for the next replay. Neither is in a release build (DEV-07).

// LiveEnv is the switch to live mode.
const LiveEnv = "FYNIQ_AI_LIVE"

// Cassette is one recorded answer: the request's key and model, the answer, and the events it streamed.
type Cassette struct {
	Key      string              `json:"key"`
	Model    string              `json:"model"`
	Response ports.ModelResponse `json:"response"`
	Events   []ports.ModelEvent  `json:"events,omitempty"`
}

// CassetteKey is a request's key: the SHA-256 of its canonical JSON (keys sorted, no spacing), so the model, the
// prompt, the conversation, the tools and the limits decide it, and their formatting does not.
func CassetteKey(r ports.ModelRequest) (string, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(v) // maps marshal with sorted keys
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// Replay answers from the cassettes in Dir; a request none matches is ports.ErrNoCassette, naming its key.
type Replay struct {
	Dir  string
	Caps ports.ModelCapabilities // what the recorded provider proved; Replay itself proves nothing
}

var _ ports.ModelProvider = Replay{}

// Capabilities implements ports.ModelProvider.
func (r Replay) Capabilities() ports.ModelCapabilities { return r.Caps }

func (r Replay) cassette(req ports.ModelRequest) (Cassette, error) {
	key, err := CassetteKey(req)
	if err != nil {
		return Cassette{}, err
	}
	b, err := os.ReadFile(filepath.Join(r.Dir, key+".json")) // #nosec G304 -- the configured cassette directory and a hex digest
	if errors.Is(err, fs.ErrNotExist) {
		return Cassette{}, fmt.Errorf("%w: key %s, model %s: record it with %s=1 on the synthetic sample company", ports.ErrNoCassette, key, req.Model, LiveEnv)
	}
	if err != nil {
		return Cassette{}, err
	}
	var c Cassette
	if err := json.Unmarshal(b, &c); err != nil {
		return Cassette{}, fmt.Errorf("cassette %s: %w", key, err)
	}
	return c, nil
}

// Call implements ports.ModelProvider.
func (r Replay) Call(_ context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	c, err := r.cassette(req)
	return c.Response, err
}

// Stream implements ports.ModelProvider: the recorded events, or the answer's parts one by one when none were recorded.
func (r Replay) Stream(_ context.Context, req ports.ModelRequest, each func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	c, err := r.cassette(req)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	events := c.Events
	if len(events) == 0 {
		for i := range c.Response.Parts {
			events = append(events, ports.ModelEvent{Part: &c.Response.Parts[i]})
		}
	}
	for _, e := range events {
		if err := each(e); err != nil {
			return ports.ModelResponse{}, err
		}
	}
	return c.Response, nil
}

// ListModels implements ports.ModelProvider: the list recorded in models.json, else none.
func (r Replay) ListModels(_ context.Context, _ string) (ports.ModelPage, error) {
	var page ports.ModelPage
	b, err := os.ReadFile(filepath.Join(r.Dir, "models.json")) // #nosec G304 -- the configured cassette directory
	if errors.Is(err, fs.ErrNotExist) {
		return page, nil
	}
	if err != nil {
		return page, err
	}
	return page, json.Unmarshal(b, &page)
}

// Recorder answers through Live and keeps each answer as a cassette in Dir.
type Recorder struct {
	Live ports.ModelProvider
	Dir  string
}

var _ ports.ModelProvider = Recorder{}

// Capabilities implements ports.ModelProvider.
func (r Recorder) Capabilities() ports.ModelCapabilities { return r.Live.Capabilities() }

func (r Recorder) keep(req ports.ModelRequest, resp ports.ModelResponse, events []ports.ModelEvent) error {
	key, err := CassetteKey(req)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(Cassette{Key: key, Model: req.Model, Response: resp, Events: events}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(r.Dir, 0o750); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.Dir, key+".json"), append(b, '\n'), 0o600)
}

// Call implements ports.ModelProvider.
func (r Recorder) Call(ctx context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	resp, err := r.Live.Call(ctx, req)
	if err != nil {
		return resp, err
	}
	return resp, r.keep(req, resp, nil)
}

// Stream implements ports.ModelProvider.
func (r Recorder) Stream(ctx context.Context, req ports.ModelRequest, each func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	var events []ports.ModelEvent
	resp, err := r.Live.Stream(ctx, req, func(e ports.ModelEvent) error {
		events = append(events, e)
		return each(e)
	})
	if err != nil {
		return resp, err
	}
	return resp, r.keep(req, resp, events)
}

// ListModels implements ports.ModelProvider: the live list, kept as models.json for the next replay.
func (r Recorder) ListModels(ctx context.Context, after string) (ports.ModelPage, error) {
	page, err := r.Live.ListModels(ctx, after)
	if err != nil || after != "" || page.Next != "" {
		return page, err // only a whole first page is kept
	}
	b, err := json.MarshalIndent(page, "", "  ")
	if err != nil {
		return page, err
	}
	if err := os.MkdirAll(r.Dir, 0o750); err != nil {
		return page, err
	}
	return page, os.WriteFile(filepath.Join(r.Dir, "models.json"), append(b, '\n'), 0o600)
}

// Models is local mode's model provider: a Replay of dir, or with FYNIQ_AI_LIVE=1 a Recorder over the provider live
// makes from a developer's key in the environment. Live mode without one is an error that says what to set.
func Models(dir string, live func() (ports.ModelProvider, error)) (ports.ModelProvider, error) {
	if os.Getenv(LiveEnv) != "1" {
		return Replay{Dir: dir}, nil
	}
	if live == nil {
		return nil, fmt.Errorf("%s=1 needs a live provider: set a developer's key for one, on the synthetic sample company only", LiveEnv)
	}
	p, err := live()
	if err != nil {
		return nil, err
	}
	return Recorder{Live: p, Dir: dir}, nil
}
