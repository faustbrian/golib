package prompts

import (
	"strings"
	"testing"
)

func TestSearchExactLimitsAndTokenMatching(t *testing.T) {
	t.Parallel()

	policy := SearchPolicy{MaxOptions: 1, MaxResults: 1, MaxQueryRunes: 1}
	if normalized, err := normalizeSearchPolicy(policy); err != nil || normalized != policy {
		t.Fatalf("exact search policy = %#v, %v", normalized, err)
	}
	option := Option[int]{id: "a", label: "A", value: 1}
	results, err := Search([]Option[int]{option}, "a", policy)
	if err != nil || len(results) != 1 || results[0].id != "a" {
		t.Fatalf("exact Search() = %#v, %v", results, err)
	}
	if !tokensMatch([]string{"a"}, []string{"a", "b"}, strings.HasPrefix) {
		t.Fatal("tokensMatch() rejected an exact token")
	}
	if tokensMatch([]string{"z"}, []string{"a", "b"}, strings.HasPrefix) {
		t.Fatal("tokensMatch() accepted a missing token")
	}
	for name, test := range map[string]struct {
		options []Option[int]
		query   string
		policy  SearchPolicy
	}{
		"options": {[]Option[int]{option, option}, "a", policy},
		"query":   {[]Option[int]{option}, "aa", policy},
		"policy":  {[]Option[int]{option}, "a", SearchPolicy{}},
	} {
		if name == "policy" {
			test.policy = SearchPolicy{MaxOptions: 0, MaxResults: 1, MaxQueryRunes: 1}
		}
		if _, err := Search(test.options, test.query, test.policy); err == nil {
			t.Fatalf("%s Search() returned nil error", name)
		}
	}
}
