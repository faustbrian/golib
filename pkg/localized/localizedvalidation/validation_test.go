package localizedvalidation_test

import (
	"errors"
	"strings"
	"testing"

	localized "github.com/faustbrian/golib/pkg/localized"
	validation "github.com/faustbrian/golib/pkg/localized/localizedvalidation"
	validationcore "github.com/faustbrian/golib/pkg/validation"
)

func value(t *testing.T, text string) localized.Text {
	t.Helper()
	value, err := localized.NewText(localized.Entry{Locale: mustLocale(t, "en"), Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestGoValidationAdapterReportsSafeCanonicalLocalePaths(t *testing.T) {
	t.Parallel()

	localizedValue, err := localized.TextFromMap(map[string]string{
		"EN-us": "secret-customer-content",
		"fi":    "",
	})
	if err != nil {
		t.Fatal(err)
	}
	report := validation.Validator(validation.MaxBytes(3), validation.RequireNonEmpty()).Validate(
		validationcore.Context{}, localizedValue,
	)
	if !report.HasErrors() || report.Len() != 2 {
		t.Fatalf("report = %s, violations = %+v", report.String(), report.Violations())
	}
	violations := report.Violations()
	if violations[0].Path().String() != "[en-US]" || violations[0].Code() != "localized_max_bytes" {
		t.Fatalf("first violation = %s %s", violations[0].Path(), violations[0].Code())
	}
	if violations[1].Path().String() != "[fi]" || violations[1].Code() != "localized_required" {
		t.Fatalf("second violation = %s %s", violations[1].Path(), violations[1].Code())
	}
	if strings.Contains(report.String(), "secret-customer-content") {
		t.Fatalf("report disclosed content: %s", report.String())
	}
}

type customRule func(string) error

func (rule customRule) ValidateText(value string) error { return rule(value) }

func TestGoValidationAdapterCodeMatrix(t *testing.T) {
	t.Parallel()

	customFailure := errors.New("custom failure")
	tests := []struct {
		name string
		text string
		rule validation.Rule
		code string
	}{
		{"whitespace", " ", validation.RequireNonWhitespace(), "localized_non_whitespace"},
		{"runes", "åä", validation.MaxRunes(1), "localized_max_runes"},
		{"lines", "a\nb", validation.MaxLines(1), "localized_max_lines"},
		{"control", "a\x00", validation.NoControlCharacters(), "localized_control_character"},
		{"invalid rule", "a", validation.MaxBytes(-1), "localized_invalid_rule"},
		{"custom", "a", customRule(func(string) error { return customFailure }), "localized_text"},
		{"nil", "a", nil, "localized_invalid_rule"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := validation.Validator(test.rule).Validate(validationcore.Context{}, value(t, test.text))
			violations := report.Violations()
			if len(violations) != 1 || violations[0].Code() != test.code {
				t.Fatalf("violations = %+v, want code %s", violations, test.code)
			}
		})
	}
}

func TestValidateComposesTextPolicies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		text  string
		rules []validation.Rule
		want  error
	}{
		{"valid", "one\ntwo", []validation.Rule{validation.MaxBytes(8), validation.MaxRunes(8), validation.MaxLines(2), validation.NoControlCharacters()}, nil},
		{"empty", "", []validation.Rule{validation.RequireNonEmpty()}, validation.ErrEmpty},
		{"whitespace", " \t\n", []validation.Rule{validation.RequireNonWhitespace()}, validation.ErrWhitespace},
		{"bytes", "four", []validation.Rule{validation.MaxBytes(3)}, validation.ErrBytesExceeded},
		{"runes", "åäö", []validation.Rule{validation.MaxRunes(2)}, validation.ErrRunesExceeded},
		{"lines", "one\ntwo", []validation.Rule{validation.MaxLines(1)}, validation.ErrLinesExceeded},
		{"control", "unsafe\x00", []validation.Rule{validation.NoControlCharacters()}, validation.ErrControlCharacter},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validation.Validate(value(t, test.text), test.rules...)
			if !errors.Is(err, test.want) {
				t.Fatalf("Validate() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestValidationRejectsInvalidRuleLimits(t *testing.T) {
	t.Parallel()

	for _, rule := range []validation.Rule{validation.MaxBytes(-1), validation.MaxRunes(-1), validation.MaxLines(-1)} {
		if err := validation.Validate(value(t, "text"), rule); !errors.Is(err, validation.ErrInvalidRule) {
			t.Fatalf("Validate() error = %v, want ErrInvalidRule", err)
		}
	}
}

func TestNormalizeIsExplicitAndPersistent(t *testing.T) {
	t.Parallel()

	original := value(t, "A\u030A")
	normalized, err := validation.Normalize(original, validation.NFC)
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	originalText, _ := original.Get(mustLocale(t, "en"))
	normalizedText, _ := normalized.Get(mustLocale(t, "en"))
	if originalText != "A\u030A" || normalizedText != "Å" {
		t.Fatalf("original = %q, normalized = %q", originalText, normalizedText)
	}
	if _, err := validation.Normalize(original, validation.Form(255)); !errors.Is(err, validation.ErrInvalidForm) {
		t.Fatalf("Normalize(invalid) error = %v", err)
	}
}

func TestValidationBoundaryBranches(t *testing.T) {
	t.Parallel()

	if got := validation.ErrEmpty.Error(); got != "localized validation: empty text" {
		t.Fatalf("Error() = %q", got)
	}
	if err := validation.Validate(value(t, "text"), nil); !errors.Is(err, validation.ErrInvalidRule) {
		t.Fatalf("Validate(nil) error = %v", err)
	}
	if err := validation.Validate(value(t, "text"), validation.RequireNonEmpty(), validation.RequireNonWhitespace()); err != nil {
		t.Fatalf("valid required text error = %v", err)
	}

	for _, form := range []validation.Form{validation.NFD, validation.NFKC, validation.NFKD} {
		normalized, err := validation.Normalize(value(t, "Å①"), form)
		if err != nil || normalized.IsEmpty() {
			t.Fatalf("Normalize(%v) = %v, %v", form, normalized.Entries(), err)
		}
	}
}
