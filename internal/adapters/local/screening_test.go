//go:build local

package local

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
)

// The local screening stub flags every case of the red-team corpus with the filters it names, in prompts and answers
// alike, flags none of the clean cases, and never repeats the matched text in a finding (PRD-001 §12.8, "Local Model
// Armor stub"; QA-AI-03).
// req: platform/local-mode Local AI in replay and live mode
func TestLocalScreeningFlagsTheRedTeamCorpus(t *testing.T) {
	b, err := os.ReadFile("testdata/screening-corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Flagged []struct {
			Source, Text string
			Filters      []string
		}
		Clean []struct{ Source, Text string }
	}
	if err := json.Unmarshal(b, &corpus); err != nil {
		t.Fatal(err)
	}
	// What the logs may never hold is screened too; built from parts, so no scanner mistakes the fixture for a secret.
	corpus.Flagged = append(corpus.Flagged,
		struct {
			Source, Text string
			Filters      []string
		}{"tool result", "use Bearer " + "abcDEF0123456789" + "ghij for the bank", []string{ports.ScreenSensitive}},
		struct {
			Source, Text string
			Filters      []string
		}{"answer", "the key is sk-" + "live0123456789abcdefXYZ", []string{ports.ScreenSensitive}})

	ctx := context.Background()
	s := Screen{}
	for _, c := range corpus.Flagged {
		for name, screen := range map[string]func(context.Context, string) (ports.Screened, error){"prompt": s.ScreenPrompt, "answer": s.ScreenResponse} {
			got, err := screen(ctx, c.Text)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Flagged() {
				t.Errorf("%s, screened as a %s: not flagged: %q", c.Source, name, c.Text)
			}
			var filters []string
			for _, f := range got.Findings {
				if f.Detail == "" || f.Confidence == "" {
					t.Errorf("%s: a finding must name what matched and how sure it is: %+v", c.Source, f)
				}
				if !slices.Contains(filters, f.Filter) {
					filters = append(filters, f.Filter)
				}
			}
			found, _ := json.Marshal(got)
			for _, value := range []string{"S1234567D", "012-345678-9", "0123456789", "501 123456 001", "evil.example", "abcDEF"} {
				if strings.Contains(string(found), value) {
					t.Errorf("%s: a finding repeats the matched text %q: %s", c.Source, value, found)
				}
			}
			slices.Sort(filters)
			want := slices.Clone(c.Filters)
			slices.Sort(want)
			if !slices.Equal(filters, want) {
				t.Errorf("%s %q: filters %v, want %v", c.Source, c.Text, filters, want)
			}
		}
	}
	for _, c := range corpus.Clean {
		got, err := s.ScreenPrompt(ctx, c.Text)
		if err != nil || got.Flagged() {
			t.Errorf("%s %q: flagged %+v %v", c.Source, c.Text, got.Findings, err)
		}
	}
}
