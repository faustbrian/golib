package internationalvalidation_test

import (
	"errors"
	"strings"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/internationalvalidation"
	"github.com/faustbrian/golib/pkg/international/phone"
	validation "github.com/faustbrian/golib/pkg/validation"
)

func TestRulesDelegateToDistinctStrictParsers(t *testing.T) {
	t.Parallel()
	finland, err := country.Parse("FI")
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, valid, invalid string
		rule                 validation.Validator[string]
	}{
		{"country", "FI", "fin", internationalvalidation.Country()},
		{"country alpha-3", "FIN", "fin", internationalvalidation.CountryAlpha3()},
		{"country numeric", "246", "24", internationalvalidation.CountryNumeric()},
		{"subdivision", "FI-18", "FI", internationalvalidation.Subdivision()},
		{"language", "fi", "fin", internationalvalidation.Language()},
		{"locale", "fi-FI", "fi_ FI", internationalvalidation.Locale()},
		{"currency", "EUR", "eur", internationalvalidation.Currency()},
		{"currency numeric", "978", "97", internationalvalidation.CurrencyNumeric()},
		{"calling code", "+358", "358", internationalvalidation.CallingCode()},
		{"phone", "040 123 4567", "not a number", internationalvalidation.Phone(phone.ParseOptions{RegionHint: finland})},
		{"valid phone", "+16502530000", "+12001230101", internationalvalidation.ValidPhone(phone.ParseOptions{})},
		{"postal", "00100", "", internationalvalidation.Postal(finland)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if report := test.rule.Validate(ctx, test.valid); report.Err() != nil {
				t.Fatalf("valid report = %v", report)
			}
			if report := test.rule.Validate(ctx, test.invalid); report.Err() == nil {
				t.Fatal("invalid value passed")
			}
		})
	}
}

func TestRulesPreserveResourceLimitAsSafeCause(t *testing.T) {
	t.Parallel()
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	report := internationalvalidation.Locale().Validate(ctx, strings.Repeat("X", 600))
	violations := report.Violations()
	if len(violations) != 1 || !errors.Is(violations[0].Cause(), international.ErrResourceLimit) {
		t.Fatalf("violations = %#v", violations)
	}
}
