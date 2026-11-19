package kbcompile

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Kind is the worker's outbox work of a person's "Recompile" (PRD-001 AI-55, AI-52; p1-knowledge-base tasks 6.3, 8.1): compile the
// organisation's changed notes, then run the deterministic lint across its knowledge base. The API only queues it; the work runs as the
// compiler, through APIClient, with the compiler's own service token (never the API's or the person's).
const Kind = "kb.compile"

// LintJob is the platform job that runs the lint pass of every organisation's knowledge base weekly (outbox.PlatformSchedules).
const LintJob = "kb.lint"

// RequestItem is the outbox item of a recompile requested by a person. Each request is its own row, so a second one while the first runs is
// harmless: it finds nothing left to compile.
func RequestItem(requestedBy uuid.UUID, compile bool) (storage.NewOutboxItem, error) {
	payload, err := json.Marshal(map[string]any{"requested_by": requestedBy.String(), "compile": compile})
	return storage.NewOutboxItem{Class: storage.ClassWorker, Kind: Kind, Payload: payload, BusinessKey: "kb-compile:" + uuid.NewString()}, err
}

// Work is the outbox's work of Kind, run by c: the compile pass (only when the request found the AI switched on: the compile needs a model,
// the lint does not), then the lint pass. A note that could not be compiled fails the row (it is tried again, as the outbox's attempts
// allow, and a dead row tells the Owner); the lint runs either way.
func Work(c Client, log *slog.Logger) outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var in struct {
			Compile bool `json:"compile"`
		}
		if err := json.Unmarshal(row.Payload, &in); err != nil {
			return fmt.Errorf("%s: %w", Kind, err)
		}
		var rep Report
		var err error
		if in.Compile {
			rep, err = Run(ctx, c, org)
		}
		found, raised, lintErr := RunLint(ctx, c, org)
		log.InfoContext(ctx, "kb.compile", "organisation", org, "considered", rep.Considered, "drafts", rep.Drafts, "findings", rep.Findings,
			"failed", len(rep.Failed), "lint_found", found, "lint_raised", raised)
		if err == nil && len(rep.Failed) > 0 {
			err = fmt.Errorf("%d note(s) could not be compiled: %s", len(rep.Failed), rep.Failed[0])
		}
		if err != nil {
			return err
		}
		return lintErr
	}
}
