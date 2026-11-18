package knowledge

import (
	"strings"
	"unicode"
)

// MaxTerms and MaxQuery bound a search (AI-04): a question's words past them are left out.
const (
	MaxTerms = 12
	MaxQuery = 200
)

// stopwords are the English words a question wastes its terms on. Full-text search here is not stemmed and ORs its terms,
// so these would match every page.
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an and are as at be but by can could did do does for from had has have how i if in into is it
	its me my of on or our should so than that the their them then there these they this to us was we were what when where which
	who whom why will with would you your`) {
		stopwords[w] = true
	}
}

// QueryTerms are the words of a question that search matches: lower-cased, split at anything that is not a letter or a
// digit, with the stopwords and one-letter words left out, each once, at most MaxTerms. Words of any script stay whole, so
// a page's aliases in another language match as typed (AI-58, QA-AI-06).
func QueryTerms(q string) []string {
	if r := []rune(q); len(r) > MaxQuery {
		q = string(r[:MaxQuery])
	}
	words := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var out []string
	seen := map[string]bool{}
	for _, w := range words {
		if len([]rune(w)) < 2 || stopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		if out = append(out, w); len(out) == MaxTerms {
			break
		}
	}
	return out
}
