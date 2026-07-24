// Package http adapts bounded Accept-Language preferences to localized matching.
package http

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
	localizedmatch "github.com/faustbrian/golib/pkg/localized/match"
)

// Error is a stable privacy-safe HTTP adapter error identity.
type Error string

// Error implements error.
func (e Error) Error() string { return string(e) }

const (
	ErrCandidateLimit   Error = "localized http: candidate limit exceeded"
	ErrDuplicateRange   Error = "localized http: duplicate range"
	ErrHeaderLimit      Error = "localized http: header limit exceeded"
	ErrInvalidParameter Error = "localized http: invalid parameter"
	ErrInvalidRange     Error = "localized http: invalid range"
	ErrInvalidWeight    Error = "localized http: invalid weight"
)

const (
	defaultMaxHeaderBytes = 8 << 10
	defaultMaxCandidates  = 64
)

// ParseOptions bounds Accept-Language parser work.
type ParseOptions struct {
	MaxBytes      int
	MaxCandidates int
}

// ParseAcceptLanguage parses ordered RFC-style preferences without applying a
// default locale. Wildcard uses the zero locale only inside this adapter.
func ParseAcceptLanguage(header string, options ParseOptions) ([]localizedmatch.Preference, error) {
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = defaultMaxHeaderBytes
	}
	maxCandidates := options.MaxCandidates
	if maxCandidates == 0 {
		maxCandidates = defaultMaxCandidates
	}
	if maxBytes < 0 || len(header) > maxBytes {
		return nil, ErrHeaderLimit
	}
	if maxCandidates < 0 {
		return nil, ErrCandidateLimit
	}
	if !utf8.ValidString(header) {
		return nil, ErrInvalidRange
	}
	if strings.TrimSpace(header) == "" {
		return nil, nil
	}

	items := strings.Split(header, ",")
	if len(items) > maxCandidates {
		return nil, ErrCandidateLimit
	}
	preferences := make([]localizedmatch.Preference, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		parts := strings.Split(item, ";")
		if len(parts) > 2 {
			return nil, ErrInvalidParameter
		}
		rawRange := strings.TrimSpace(parts[0])
		if rawRange == "" {
			return nil, ErrInvalidRange
		}
		weight := 1.0
		if len(parts) == 2 {
			parameter := strings.SplitN(strings.TrimSpace(parts[1]), "=", 2)
			if len(parameter) != 2 || !strings.EqualFold(strings.TrimSpace(parameter[0]), "q") {
				return nil, ErrInvalidParameter
			}
			var err error
			weight, err = parseWeight(strings.TrimSpace(parameter[1]))
			if err != nil {
				return nil, err
			}
		}

		var tag locale.Tag
		var key string
		if rawRange == "*" {
			tag = locale.Tag{}
			key = "*"
		} else {
			if strings.ContainsAny(rawRange, "_ \t\r\n") {
				return nil, ErrInvalidRange
			}
			parsed, err := locale.Parse(rawRange)
			if err != nil {
				return nil, ErrInvalidRange
			}
			canonical, _ := parsed.Canonical()
			tag = canonical
			key = canonical.String()
		}
		if _, ok := seen[key]; ok {
			return nil, ErrDuplicateRange
		}
		seen[key] = struct{}{}
		preferences = append(preferences, localizedmatch.Preference{Locale: tag, Weight: weight})
	}
	return preferences, nil
}

func parseWeight(raw string) (float64, error) {
	valid := raw == "0" || raw == "1"
	if strings.HasPrefix(raw, "0.") && len(raw) <= 5 {
		valid = digitsOnly(raw[2:])
	}
	if strings.HasPrefix(raw, "1.") && len(raw) <= 5 {
		valid = digitsOnly(raw[2:]) && strings.Trim(raw[2:], "0") == ""
	}
	if !valid {
		return 0, ErrInvalidWeight
	}
	// The complete syntax check above makes ParseFloat infallible here.
	weight, _ := strconv.ParseFloat(raw, 64)
	return weight, nil
}

func digitsOnly(value string) bool {
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

// Select parses preferences and performs matching without configured fallback.
func Select(value localized.Text, header string, options ParseOptions) (localizedmatch.Result, error) {
	preferences, err := ParseAcceptLanguage(header, options)
	if err != nil {
		return localizedmatch.Result{}, err
	}
	items := strings.Split(header, ",")
	type candidate struct {
		preference localizedmatch.Preference
		wildcard   bool
	}
	candidates := make([]candidate, len(preferences))
	for i, preference := range preferences {
		candidates[i] = candidate{
			preference: preference,
			wildcard:   strings.TrimSpace(strings.SplitN(items[i], ";", 2)[0]) == "*",
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].preference.Weight > candidates[j].preference.Weight
	})
	for _, candidate := range candidates {
		if candidate.preference.Weight == 0 {
			continue
		}
		if candidate.wildcard {
			locales := value.Locales()
			if len(locales) == 0 {
				continue
			}
			text, _ := value.Get(locales[0])
			return localizedmatch.Result{
				Kind: localizedmatch.Matched, Requested: locale.Tag{},
				Locale: locales[0], Text: text, Present: true, Empty: text == "",
			}, nil
		}
		// The parser has already guaranteed one bounded preference with a
		// weight in [0,1], so Best cannot fail for this internal call.
		result, _ := localizedmatch.Best(value, candidate.preference)
		if result.Present {
			return result, nil
		}
	}
	return localizedmatch.Result{Kind: localizedmatch.Missing}, nil
}
