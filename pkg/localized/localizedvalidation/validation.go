// Package localizedvalidation provides composable, content-safe text policies.
package localizedvalidation

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	localized "github.com/faustbrian/golib/pkg/localized"
	validationcore "github.com/faustbrian/golib/pkg/validation"
	"golang.org/x/text/unicode/norm"
)

// Error is a stable privacy-safe validation error identity.
type Error string

// Error implements error.
func (e Error) Error() string { return string(e) }

const (
	ErrBytesExceeded    Error = "localized validation: byte limit exceeded"
	ErrControlCharacter Error = "localized validation: control character"
	ErrEmpty            Error = "localized validation: empty text"
	ErrInvalidForm      Error = "localized validation: invalid normalization form"
	ErrInvalidRule      Error = "localized validation: invalid rule"
	ErrLinesExceeded    Error = "localized validation: line limit exceeded"
	ErrRunesExceeded    Error = "localized validation: rune limit exceeded"
	ErrWhitespace       Error = "localized validation: whitespace-only text"
)

// Rule validates one localized string without receiving its locale identity.
type Rule interface {
	ValidateText(string) error
}

type ruleFunc func(string) error

func (rule ruleFunc) ValidateText(value string) error { return rule(value) }

// Validate applies every rule to every present value in deterministic order.
func Validate(value localized.Text, rules ...Rule) error {
	for _, entry := range value.Entries() {
		for _, rule := range rules {
			if rule == nil {
				return ErrInvalidRule
			}
			if err := rule.ValidateText(entry.Text); err != nil {
				return err
			}
		}
	}
	return nil
}

// Validator adapts immutable localized text rules to validation. The
// returned validator snapshots the rule slice and emits content-free findings
// at canonical locale-key paths. Built-in rules are immutable; callers remain
// responsible for the concurrency safety of custom Rule implementations.
func Validator(rules ...Rule) validationcore.Validator[localized.Text] {
	owned := append([]Rule(nil), rules...)
	return validationcore.ValidatorFunc[localized.Text](func(
		ctx validationcore.Context, value localized.Text,
	) validationcore.Report {
		report := validationcore.NewReport(ctx.Limits())
		for _, entry := range value.Entries() {
			path := ctx.Path().Append(validationcore.Key(entry.Locale.String()))
			for _, rule := range owned {
				if rule == nil {
					report = report.Add(validationcore.NewViolation(
						path, "localized_invalid_rule", validationcore.Error, nil, nil,
					))
					continue
				}
				if err := rule.ValidateText(entry.Text); err != nil {
					report = report.Add(validationcore.NewViolation(
						path, validationCode(err), validationcore.Error, nil, nil,
					))
				}
			}
		}
		return report
	})
}

func validationCode(err error) string {
	switch {
	case errors.Is(err, ErrEmpty):
		return "localized_required"
	case errors.Is(err, ErrWhitespace):
		return "localized_non_whitespace"
	case errors.Is(err, ErrBytesExceeded):
		return "localized_max_bytes"
	case errors.Is(err, ErrRunesExceeded):
		return "localized_max_runes"
	case errors.Is(err, ErrLinesExceeded):
		return "localized_max_lines"
	case errors.Is(err, ErrControlCharacter):
		return "localized_control_character"
	case errors.Is(err, ErrInvalidRule):
		return "localized_invalid_rule"
	default:
		return "localized_text"
	}
}

// RequireNonEmpty rejects present-empty strings.
func RequireNonEmpty() Rule {
	return ruleFunc(func(value string) error {
		if value == "" {
			return ErrEmpty
		}
		return nil
	})
}

// RequireNonWhitespace rejects empty and Unicode whitespace-only strings.
func RequireNonWhitespace() Rule {
	return ruleFunc(func(value string) error {
		if strings.TrimSpace(value) == "" {
			return ErrWhitespace
		}
		return nil
	})
}

// MaxBytes bounds UTF-8 encoded bytes.
func MaxBytes(max int) Rule {
	return ruleFunc(func(value string) error {
		if max < 0 {
			return ErrInvalidRule
		}
		if len(value) > max {
			return ErrBytesExceeded
		}
		return nil
	})
}

// MaxRunes bounds Unicode code points without claiming grapheme semantics.
func MaxRunes(max int) Rule {
	return ruleFunc(func(value string) error {
		if max < 0 {
			return ErrInvalidRule
		}
		if utf8.RuneCountInString(value) > max {
			return ErrRunesExceeded
		}
		return nil
	})
}

// MaxLines bounds newline-delimited logical lines. Empty text has zero lines.
func MaxLines(max int) Rule {
	return ruleFunc(func(value string) error {
		if max < 0 {
			return ErrInvalidRule
		}
		lines := 0
		if value != "" {
			lines = strings.Count(value, "\n") + 1
		}
		if lines > max {
			return ErrLinesExceeded
		}
		return nil
	})
}

// NoControlCharacters rejects Unicode controls except tab and line endings.
func NoControlCharacters() Rule {
	return ruleFunc(func(value string) error {
		for _, character := range value {
			if unicode.IsControl(character) && character != '\t' &&
				character != '\n' && character != '\r' {
				return ErrControlCharacter
			}
		}
		return nil
	})
}

// Form selects an explicit Unicode normalization transform.
type Form uint8

const (
	// NFC applies canonical composition.
	NFC Form = iota
	// NFD applies canonical decomposition.
	NFD
	// NFKC applies compatibility composition.
	NFKC
	// NFKD applies compatibility decomposition.
	NFKD
)

// Normalize returns a new value with the selected transform applied to text
// only. Locale identifiers are never normalized or rewritten here.
func Normalize(value localized.Text, form Form) (localized.Text, error) {
	var normalizer norm.Form
	switch form {
	case NFC:
		normalizer = norm.NFC
	case NFD:
		normalizer = norm.NFD
	case NFKC:
		normalizer = norm.NFKC
	case NFKD:
		normalizer = norm.NFKD
	default:
		return localized.Text{}, ErrInvalidForm
	}
	entries := value.Entries()
	for i := range entries {
		entries[i].Text = normalizer.String(entries[i].Text)
	}
	return localized.NewText(entries...)
}
