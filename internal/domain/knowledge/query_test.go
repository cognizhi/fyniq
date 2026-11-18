package knowledge_test

import (
	"slices"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/knowledge"
)

// req: AI-58
func TestAQuestionsSearchTermsAreItsWordsWithoutTheFiller(t *testing.T) {
	for q, want := range map[string][]string{
		"How do I approve a team claim?":          {"approve", "team", "claim"},
		"  What is the GST rate, the GST rate!  ": {"gst", "rate"},
		"我欠股东的钱 director loan":                    {"我欠股东的钱", "director", "loan"},
		"":                                        nil,
		"the of and":                              nil,
	} {
		if got := knowledge.QueryTerms(q); !slices.Equal(got, want) {
			t.Errorf("%q: %v; want %v", q, got, want)
		}
	}
	long := ""
	for i := 0; i < 40; i++ {
		long += "word" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + " "
	}
	if got := knowledge.QueryTerms(long); len(got) != knowledge.MaxTerms {
		t.Errorf("%d terms; want at most %d", len(got), knowledge.MaxTerms)
	}
}
