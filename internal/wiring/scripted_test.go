//go:build local

package wiring

import (
	"context"
	"testing"

	"github.com/cognizhi/fyniq/connectors"
	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/connector"
)

// Local mode's catalogue has the scripted model the AI journeys route to (p1-launch-readiness design D6), as a model
// provider whose manifest the catalogue accepts; a release's catalogue never has it.
// req: QA-E2E-01
func TestLocalModesCatalogueHasTheScriptedModel(t *testing.T) {
	b, err := scriptedModel()
	if err != nil {
		t.Fatal(err)
	}
	all, err := connectors.Catalogue(b)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := all.Get("scripted")
	if !ok || got.Model == nil {
		t.Fatalf("local mode's catalogue has no scripted model provider: %+v", got.Manifest)
	}
	if _, ok := got.Manifest.Capability(connector.ModelProvider); !ok {
		t.Fatal("the scripted connector is not a model provider")
	}
	p, err := got.Model(connector.Config{})
	if err != nil {
		t.Fatal(err)
	}
	page, err := p.ListModels(context.Background(), "")
	if err != nil || len(page.Models) != 1 || page.Models[0].ID != local.ScriptedModel {
		t.Fatalf("its models: %+v %v", page, err)
	}
	release, err := connectors.Catalogue()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := release.Get("scripted"); ok {
		t.Fatal("the release's catalogue has the scripted model")
	}
}
