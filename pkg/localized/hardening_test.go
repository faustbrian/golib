package localized_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	language "github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
	localizedmatch "github.com/faustbrian/golib/pkg/localized/match"
)

func TestStandardsCanonicalizationMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		class string
		raw   string
		want  string
	}{
		{"language", "FI", "fi"},
		{"script", "zh-hant", "zh-Hant"},
		{"region", "en-us", "en-US"},
		{"variant", "sl-rozaj-biske", "sl-rozaj-biske"},
		{"extension", "en-u-ca-gregory", "en-u-ca-gregory"},
		{"private use", "x-tenant", "x-tenant"},
		{"grandfathered preferred value", "i-klingon", "tlh"},
		{"deprecated preferred value", "iw-IL", "he-IL"},
		{"undetermined", "und", "und"},
		{"multiple", "mul", "mul"},
		{"reserved unknown", "qaa", "qaa"},
	}
	for _, test := range tests {
		t.Run(test.class, func(t *testing.T) {
			tag, err := language.Parse(test.raw)
			if err != nil {
				t.Fatalf("language.Parse(%q) error = %v", test.raw, err)
			}
			value, err := localized.NewText(localized.Entry{Locale: tag, Text: test.class})
			if err != nil {
				t.Fatalf("NewText(%q) error = %v", test.raw, err)
			}
			if got := value.Pairs()[0].Locale; got != test.want {
				t.Fatalf("canonical locale = %q, want %q", got, test.want)
			}
		})
	}

	for _, raw := range []string{"", "en_", "en-", "en us", "-fi"} {
		if _, err := localized.TextFromMap(map[string]string{raw: "value"}); !errors.Is(err, localized.ErrInvalidLocale) {
			t.Fatalf("TextFromMap(%q) error = %v", raw, err)
		}
	}
}

func TestStandardsRegistryProvenance(t *testing.T) {
	t.Parallel()

	provenance := language.DatasetProvenance()
	if provenance.Dataset != "iana-language-subtag-registry" ||
		provenance.UpstreamVersion != "IANA registry 2026-06-14; x/text v0.40.0" ||
		provenance.SHA256 != "be1fad86a99e3a932d07b80c9b3c271ec2381a5909ce22420144e5077ab0a43a" {
		t.Fatalf("locale provenance drifted: %+v", provenance)
	}
}

func TestStandardsMatchingMatrix(t *testing.T) {
	t.Parallel()

	value, err := localized.TextFromMap(map[string]string{
		"de": "Deutsch", "en-GB": "Hallo", "en-US": "Hello", "mul": "Many",
		"und": "Unknown", "x-tenant": "Private", "zh-Hant": "您好",
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		requested string
		kind      localizedmatch.Kind
		selected  string
	}{
		{"script parent", "zh-Hant-TW", localizedmatch.Matched, "zh-Hant"},
		{"region choice", "en-CA", localizedmatch.Matched, "en-GB"},
		{"variant parent", "de-1996", localizedmatch.Matched, "de"},
		{"private exact", "x-tenant", localizedmatch.Exact, "x-tenant"},
		{"und exact", "und", localizedmatch.Exact, "und"},
		{"mul exact", "mul", localizedmatch.Exact, "mul"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, matchErr := localizedmatch.Best(value, localizedmatch.Preference{
				Locale: mustLocale(t, test.requested), Weight: 1,
			})
			if matchErr != nil || result.Kind != test.kind || result.Locale.String() != test.selected {
				t.Fatalf("Best(%s) = %+v, %v", test.requested, result, matchErr)
			}
		})
	}
}

func TestMergeMissingEmptyConflictMatrix(t *testing.T) {
	t.Parallel()

	type state struct {
		present bool
		text    string
	}
	states := []state{{}, {present: true}, {present: true, text: "value"}}
	policies := []localized.MergePolicy{localized.LeftWins, localized.RightWins, localized.RejectConflict}
	for leftIndex, leftState := range states {
		for rightIndex, rightState := range states {
			for _, emptyPolicy := range []localized.EmptyPolicy{localized.EmptyIsValue, localized.EmptyIsAbsent} {
				for _, policy := range policies {
					name := fmt.Sprintf("left-%d/right-%d/empty-%d/policy-%d", leftIndex, rightIndex, emptyPolicy, policy)
					t.Run(name, func(t *testing.T) {
						left := matrixText(t, leftState.present, leftState.text)
						right := matrixText(t, rightState.present, rightState.text)
						result, err := left.MergeWithOptions(right, localized.MergeOptions{Conflicts: policy, Empty: emptyPolicy})
						leftEffective := leftState.present && (emptyPolicy == localized.EmptyIsValue || leftState.text != "")
						rightEffective := rightState.present && (emptyPolicy == localized.EmptyIsValue || rightState.text != "")
						if policy == localized.RejectConflict && leftEffective && rightEffective {
							if !errors.Is(err, localized.ErrConflict) {
								t.Fatalf("error = %v, want ErrConflict", err)
							}
							return
						}
						if err != nil {
							t.Fatal(err)
						}
						got, present := result.Get(mustLocale(t, "en"))
						want, wantPresent := "", false
						switch {
						case leftEffective && rightEffective && policy == localized.RightWins:
							want, wantPresent = rightState.text, true
						case leftEffective:
							want, wantPresent = leftState.text, true
						case rightEffective:
							want, wantPresent = rightState.text, true
						}
						if got != want || present != wantPresent {
							t.Fatalf("Get(en) = %q, %v; want %q, %v", got, present, want, wantPresent)
						}
					})
				}
			}
		}
	}
}

func matrixText(t *testing.T, present bool, text string) localized.Text {
	t.Helper()
	if !present {
		return localized.Text{}
	}
	value, err := localized.NewText(localized.Entry{Locale: mustLocale(t, "en"), Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestConcurrentImmutableOperationSurface(t *testing.T) {
	t.Parallel()

	value, _ := localized.TextFromMap(map[string]string{"en-US": "Hello", "fi": "", "zh-Hant": "您好"})
	overlay, _ := localized.TextFromMap(map[string]string{"sv": "Hej"})
	requested := mustLocale(t, "zh-Hant-TW")
	plan, err := localizedmatch.NewPlan([]localizedmatch.Chain{{
		From: requested, Candidates: []localizedmatch.Candidate{{Kind: localizedmatch.ParentRange, Locale: requested}},
	}}, localizedmatch.PlanOptions{MaxDepth: 2, MaxCandidates: 1})
	if err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if text, present := value.Get(mustLocale(t, "en-US")); !present || text != "Hello" {
				t.Errorf("Get() = %q, %v", text, present)
			}
			count := 0
			for range value.All() {
				count++
			}
			if count != value.Len() {
				t.Errorf("iterator count = %d", count)
			}
			matched, matchErr := localizedmatch.Best(value, localizedmatch.Preference{Locale: mustLocale(t, "en-CA"), Weight: 1})
			if matchErr != nil || !matched.Present {
				t.Errorf("Best() = %+v, %v", matched, matchErr)
			}
			if resolved := plan.Resolve(value, requested); !resolved.Present || resolved.Text != "您好" {
				t.Errorf("Resolve() = %+v", resolved)
			}
			merged, mergeErr := value.Merge(overlay, localized.RightWins)
			if mergeErr != nil || merged.Len() != value.Len()+1 {
				t.Errorf("Merge() len = %d, error = %v", merged.Len(), mergeErr)
			}
			encoded, encodeErr := json.Marshal(value)
			if encodeErr != nil || !strings.Contains(string(encoded), `"fi":""`) {
				t.Errorf("Marshal() = %s, %v", encoded, encodeErr)
			}
		}()
	}
	wait.Wait()
}

func TestErrorsDoNotDiscloseLocalizedContent(t *testing.T) {
	t.Parallel()

	secret := "secret-customer-content"
	_, err := localized.NewTextWithLimits(
		localized.Limits{MaxLocales: 1, MaxTagBytes: 8, MaxTextBytes: 1, MaxTotalBytes: 1},
		localized.Entry{Locale: mustLocale(t, "en"), Text: secret},
	)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error = %q", err)
	}
}
