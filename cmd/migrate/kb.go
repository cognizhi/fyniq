package main

import (
	"context"
	"fmt"
	"os"

	userguide "github.com/cognizhi/fyniq/docs/user-guide"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/domain/knowledge"
	"github.com/cognizhi/fyniq/internal/version"
	"github.com/cognizhi/fyniq/kb"
)

// loadKnowledge copies the release's compiled knowledge base and the guide it is compiled from into the global tables, after
// the migrations, as the statutory packs are (PRD-001 AI-50, AI-56; p1-knowledge-base task 1.2). Only this binary embeds
// them: the services read them through the reader filter and carry no copy. A knowledge base that does not pass its lint
// is refused, so the release does not start with pages `make kb-lint` would have refused.
func loadKnowledge(ctx context.Context, url string) error {
	c, err := knowledge.Load(kb.FS, userguide.FS)
	if err != nil {
		return err
	}
	rel, err := knowledge.NewRelease(version.Version, c)
	if err != nil {
		return err
	}
	if err := postgres.LoadPlatformKB(ctx, url, rel, os.Stderr); err != nil {
		return fmt.Errorf("load the knowledge base: %w", err)
	}
	return nil
}
