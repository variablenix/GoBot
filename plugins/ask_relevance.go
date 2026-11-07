package plugins

import (
	"strings"
	"unicode"
)

// A search excerpt about only one side must not win a comparison query over
// an in-flight Assist answer. This is a conservative lexical check for weak
// search-result fallbacks, not a filter on generated Assist paraphrases.
func askComparisonRelevant(question, excerpt string) bool {
	fields := strings.Fields(strings.ToLower(question))
	for i, field := range fields {
		if token := strings.Trim(field, ".,?!:;"); token != "vs" && token != "versus" {
			continue
		}
		left := askComparisonWords(strings.Join(fields[:i], " "))
		right := askComparisonWords(strings.Join(fields[i+1:], " "))
		for word := range left {
			if _, shared := right[word]; shared {
				delete(left, word)
				delete(right, word)
			}
		}
		if len(left) == 0 || len(right) == 0 {
			return true // Too little subject information to reject a source.
		}
		compact := strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return unicode.ToLower(r)
			}
			return -1
		}, excerpt)
		matches := func(words map[string]struct{}) bool {
			for word := range words {
				if strings.Contains(compact, word) {
					return true
				}
			}
			return false
		}
		return matches(left) && matches(right)
	}
	return true
}

func askComparisonWords(text string) map[string]struct{} {
	words := make(map[string]struct{})
	for _, word := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		switch word {
		case "how", "much", "do", "does", "what", "which", "is", "are", "the", "a", "an", "of", "to", "in", "for", "at", "cost", "costs", "price", "prices", "compare", "comparison", "between", "new", "used":
			continue
		}
		if len(word) > 3 {
			word = strings.TrimSuffix(word, "s")
		}
		if len(word) >= 2 {
			words[word] = struct{}{}
		}
	}
	return words
}
