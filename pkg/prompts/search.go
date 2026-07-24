package prompts

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// SearchPolicy bounds deterministic static option search.
type SearchPolicy struct {
	MaxOptions    int
	MaxResults    int
	MaxQueryRunes int
}

// SearchSelectConfig defines a searchable single selection prompt.
type SearchSelectConfig[T any] struct {
	Select SelectConfig[T]
	Search SearchPolicy
}

// NewSearchSelect creates a searchable single selection prompt.
func NewSearchSelect[T any](config SearchSelectConfig[T]) (Prompt[T], error) {
	policy, err := normalizeSearchPolicy(config.Search)
	if err != nil {
		return Prompt[T]{}, invalidBehaviorDefinition("define search-select prompt", config.Select.ID, err)
	}
	prompt, err := newSelect(KindSearchSelect, config.Select)
	if err != nil {
		return Prompt[T]{}, err
	}
	details := *prompt.definition.selection
	details.searchPolicy = policy
	prompt.definition.selection = &details

	return prompt, nil
}

// Search returns ranked option copies with stable declaration-order ties.
func Search[T any](options []Option[T], query string, policy SearchPolicy) ([]Option[T], error) {
	normalizedPolicy, err := normalizeSearchPolicy(policy)
	if err != nil || len(options) > normalizedPolicy.MaxOptions || utf8.RuneCountInString(query) > normalizedPolicy.MaxQueryRunes {
		return nil, &Error{Kind: ErrorUnsupported, Operation: "search options", Cause: ErrUnsupported}
	}
	if len(options) == 0 {
		return []Option[T]{}, nil
	}
	owned, _, err := ownOptions(options, normalizedPolicy.MaxOptions)
	if err != nil {
		return nil, &Error{Kind: ErrorUnsupported, Operation: "search options", Cause: ErrUnsupported}
	}
	options = owned
	normalizedQuery := normalizeSearchText(query)
	queryTokens := strings.Fields(normalizedQuery)
	type match struct {
		option Option[T]
		rank   int
		index  int
	}
	matches := make([]match, 0, min(len(options), normalizedPolicy.MaxResults))
	for index, option := range options {
		rank, matched := searchRank(option, normalizedQuery, queryTokens)
		if matched {
			matches = append(matches, match{option: option, rank: rank, index: index})
		}
	}
	sort.SliceStable(matches, func(left, right int) bool {
		if matches[left].rank != matches[right].rank {
			return matches[left].rank < matches[right].rank
		}

		return matches[left].index < matches[right].index
	})
	if len(matches) > normalizedPolicy.MaxResults {
		matches = matches[:normalizedPolicy.MaxResults]
	}
	results := make([]Option[T], len(matches))
	for index, matched := range matches {
		results[index] = matched.option
	}

	return results, nil
}

func normalizeSearchPolicy(policy SearchPolicy) (SearchPolicy, error) {
	if policy == (SearchPolicy{}) {
		return SearchPolicy{MaxOptions: 10_000, MaxResults: 50, MaxQueryRunes: 256}, nil
	}
	if policy.MaxOptions < 1 || policy.MaxResults < 1 || policy.MaxQueryRunes < 1 {
		return SearchPolicy{}, fmt.Errorf("%w: invalid search bounds", ErrUnsupported)
	}

	return policy, nil
}

func normalizeSearchText(value string) string {
	return cases.Fold().String(norm.NFKC.String(value))
}

func searchRank[T any](option Option[T], query string, queryTokens []string) (int, bool) {
	label := normalizeSearchText(option.label)
	searchable := strings.TrimSpace(label + " " + normalizeSearchText(option.description))
	if query == "" {
		return 4, true
	}
	if label == query {
		return 0, true
	}
	if strings.HasPrefix(label, query) {
		return 1, true
	}
	candidateTokens := strings.Fields(searchable)
	if tokensMatch(queryTokens, candidateTokens, strings.HasPrefix) {
		return 2, true
	}
	if tokensMatch(queryTokens, candidateTokens, strings.Contains) {
		return 3, true
	}

	return 0, false
}

func tokensMatch(queries []string, candidates []string, matches func(string, string) bool) bool {
	for _, query := range queries {
		found := false
		for _, candidate := range candidates {
			if matches(candidate, query) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}
