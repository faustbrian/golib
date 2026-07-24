// Package match provides explicit language matching and application fallback.
package match

import (
	"sort"

	"github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
	textlanguage "golang.org/x/text/language"
)

// Kind identifies how a result was resolved.
type Kind uint8

const (
	// Missing means no value was resolved.
	Missing Kind = iota
	// Exact means the requested locale was present exactly.
	Exact
	// Matched means the locale layer selected a standards-aligned alternative.
	Matched
	// Fallback means an explicit application fallback candidate was present.
	Fallback
	// Default means the plan's final default locale was present.
	Default
)

// Preference is a requested locale with an HTTP-style quality weight.
type Preference struct {
	Locale locale.Tag
	Weight float64
}

// Result describes resolution without conflating missing and present-empty.
type Result struct {
	Kind      Kind
	Requested locale.Tag
	Locale    locale.Tag
	Text      string
	Present   bool
	Empty     bool
}

const maxPreferences = 64

// Options bounds matching and attaches an optional content-free observer.
type Options struct {
	MaxCandidates int
	Observer      Observer
}

// Best performs standards-aligned language matching. It never applies an
// application fallback or changes the stored value.
func Best(value localized.Text, preferences ...Preference) (Result, error) {
	return BestWithOptions(value, Options{}, preferences...)
}

// BestWithOptions performs standards matching with explicit limits and hooks.
func BestWithOptions(value localized.Text, options Options, preferences ...Preference) (Result, error) {
	maxCandidates := options.MaxCandidates
	if maxCandidates == 0 {
		maxCandidates = maxPreferences
	}
	if maxCandidates < 0 || len(preferences) > maxCandidates {
		return Result{}, ErrCandidateLimit
	}
	ordered := append([]Preference(nil), preferences...)
	for i, preference := range ordered {
		canonical, err := preference.Locale.Canonical()
		if err != nil {
			return Result{}, ErrInvalidCandidate
		}
		ordered[i].Locale = canonical
		if preference.Weight < 0 || preference.Weight > 1 {
			return Result{}, ErrInvalidWeight
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Weight > ordered[j].Weight
	})

	accepted := ordered[:0]
	for _, preference := range ordered {
		if preference.Weight > 0 {
			accepted = append(accepted, preference)
		}
	}
	if len(accepted) == 0 || value.IsEmpty() {
		result := Result{Kind: Missing}
		notify(options.Observer, Event{Operation: OperationMatch, Kind: result.Kind, CandidateCount: len(accepted)})
		return result, nil
	}
	supported := value.Locales()
	matcherTags := make([]textlanguage.Tag, len(supported))
	for i, tag := range supported {
		matcherTags[i] = textlanguage.MustParse(tag.String())
	}
	matcher := textlanguage.NewMatcher(matcherTags)
	for _, preference := range accepted {
		if text, ok := value.Get(preference.Locale); ok {
			result := present(Exact, preference.Locale, preference.Locale, text)
			notify(options.Observer, Event{Operation: OperationMatch, Kind: result.Kind, CandidateCount: len(accepted)})
			return result, nil
		}
		_, index, confidence := matcher.Match(textlanguage.MustParse(preference.Locale.String()))
		if confidence != textlanguage.No {
			locale := supported[index]
			text, _ := value.Get(locale)
			result := present(Matched, preference.Locale, locale, text)
			notify(options.Observer, Event{Operation: OperationMatch, Kind: result.Kind, CandidateCount: len(accepted)})
			return result, nil
		}
	}
	result := Result{Kind: Missing}
	notify(options.Observer, Event{Operation: OperationMatch, Kind: result.Kind, CandidateCount: len(accepted)})
	return result, nil
}

func present(kind Kind, requested, selected locale.Tag, text string) Result {
	return Result{
		Kind: kind, Requested: requested, Locale: selected, Text: text,
		Present: true, Empty: text == "",
	}
}

// FallbackPlan is an immutable ordered application fallback chain.
type FallbackPlan struct {
	candidates []locale.Tag
	defaultTag *locale.Tag
}

// NewFallbackPlan validates and copies an ordered exact-locale chain. A
// positive max bounds both the chain and optional default candidate.
func NewFallbackPlan(candidates []locale.Tag, defaultTag *locale.Tag, max int) (FallbackPlan, error) {
	count := len(candidates)
	if defaultTag != nil {
		count++
	}
	if max < 0 || count > max {
		return FallbackPlan{}, ErrCandidateLimit
	}
	seen := make(map[string]struct{}, count)
	owned := append([]locale.Tag(nil), candidates...)
	for i, candidate := range owned {
		canonical, err := candidate.Canonical()
		if err != nil {
			return FallbackPlan{}, ErrInvalidCandidate
		}
		owned[i] = canonical
		candidate = canonical
		key := candidate.String()
		if _, ok := seen[key]; ok {
			return FallbackPlan{}, ErrDuplicateCandidate
		}
		seen[key] = struct{}{}
	}
	var ownedDefault *locale.Tag
	if defaultTag != nil {
		canonical, err := defaultTag.Canonical()
		if err != nil {
			return FallbackPlan{}, ErrInvalidCandidate
		}
		key := canonical.String()
		if _, ok := seen[key]; ok {
			return FallbackPlan{}, ErrDuplicateCandidate
		}
		copy := canonical
		ownedDefault = &copy
	}
	return FallbackPlan{candidates: owned, defaultTag: ownedDefault}, nil
}

// Resolve performs exact lookup along the configured chain, then the optional
// default. It never materializes a fallback entry.
func (p FallbackPlan) Resolve(value localized.Text) Result {
	for _, candidate := range p.candidates {
		if text, ok := value.Get(candidate); ok {
			return present(Fallback, candidate, candidate, text)
		}
	}
	if p.defaultTag != nil {
		if text, ok := value.Get(*p.defaultTag); ok {
			return present(Default, *p.defaultTag, *p.defaultTag, text)
		}
	}
	return Result{Kind: Missing}
}
