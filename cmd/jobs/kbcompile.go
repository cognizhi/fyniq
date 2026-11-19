package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/kbcompile"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/wiring"
)

// kbCompilerTokens is how the compiler gets its service token for the API, set by the build that has one: the local build signs it
// with the local key (kbcompile_local.go); the release build asks Google for an identity token as the compiler's own service account
// [live-verify], and until it is wired the job refuses to run rather than run as anything else.
var kbCompilerTokens func(clock ports.Clock) func(ctx context.Context) (string, error)

// runKBCompile is `jobs kb-compile <organisation id>` (PRD-001 AI-52, AI-57; p1-knowledge-base design D7): the knowledge-base
// compiler, run to completion. It opens no database and holds no provider key: it reaches the API's compiler operations with its
// own service token, which open nothing else, so it can make drafts and findings and cannot publish, read the ledger or run a command.
func runKBCompile(ctx context.Context, clock ports.Clock, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: jobs kb-compile <organisation id>")
	}
	org, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("%q is not an organisation's ID", args[0])
	}
	if kbCompilerTokens == nil {
		return errors.New("the knowledge-base compiler has no service identity in this build: it runs under its own, never another's [live-verify]")
	}
	url := os.Getenv("FYNIQ_API_INTERNAL_URL")
	if url == "" {
		url = "http://localhost:8081/internal/v1"
	}
	client := kbcompile.APIClient{URL: url, Token: kbCompilerTokens(clock), HTTP: wiring.PlatformEgress().Bounded(4<<20, 10*time.Minute).For(egress.HostOf(url))}
	rep, err := kbcompile.Run(ctx, client, org)
	found, raised, lintErr := kbcompile.RunLint(ctx, client, org)
	fmt.Fprintf(os.Stderr, "jobs: knowledge-base lint for %s: %d finding(s) in the lint, %d newly raised\n", org, found, raised)
	err = errors.Join(err, lintErr)
	fmt.Fprintf(os.Stderr, "jobs: knowledge-base compiler for %s: %d note(s) considered, %d draft(s), %d finding(s), %d failed\n", org, rep.Considered, rep.Drafts, rep.Findings, len(rep.Failed))
	for _, f := range rep.Failed {
		fmt.Fprintln(os.Stderr, "jobs:   failed:", f)
	}
	if err == nil && len(rep.Failed) > 0 {
		err = fmt.Errorf("%d note(s) could not be compiled", len(rep.Failed))
	}
	return err
}
