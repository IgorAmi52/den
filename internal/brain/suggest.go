// Package brain is what den thinks with: free local rules over what it observes, and the prompts and checks
// for its one-shot model calls. Everything it finds becomes a suggestion the user confirms.
package brain

import (
	"slices"
	"strings"

	"github.com/IgorAmi52/den/internal/agent"
	"github.com/IgorAmi52/den/internal/store"
)

// Observation is everything the rules look at. All of it is local and free to read.
type Observation struct {
	Repository *store.Repository
	Digests    map[string]agent.Digest
	// ContextTokens has an entry for each folder with context, the repo's under "".
	ContextTokens map[string]int
}

// Suggest runs the local rules. The store drops the suggestions the user already rejected or is still looking at.
func Suggest(observation Observation) []store.Suggestion {
	return append(folderNotes(observation), skillFolders(observation)...)
}

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
