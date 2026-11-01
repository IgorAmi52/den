// Package brain is what den thinks with: free local rules over what it observes, and the prompts and checks
// for its one-shot model calls. Everything it finds becomes a suggestion the user confirms.
package brain

import (
	"slices"
	"strings"
)

// words splits text into lowercase words, breaking on anything that isn't a letter or digit.
func words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(character rune) bool {
		return !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9')
	})
}

func containsAll(haystack []string, needles []string) bool {
	for _, needle := range needles {
		if !slices.Contains(haystack, needle) {
			return false
		}
	}
	return true
}

func toSet(spaceSeparated string) map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.Fields(spaceSeparated) {
		set[word] = true
	}
	return set
}
