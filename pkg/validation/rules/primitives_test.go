package rules_test

import (
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/rules"
)

func TestStandardIdentifierPrimitives(t *testing.T) {
	ctx := contextFor(t)
	tests := []struct {
		name, value, code string
		validator         validation.Validator[string]
		valid             bool
	}{
		{"url", "https://example.com/a", "url", rules.URL(), true},
		{"url relative", "/a", "url", rules.URL(), false},
		{"hostname", "api.example.com", "hostname", rules.Hostname(), true},
		{"hostname bad", "bad_name", "hostname", rules.Hostname(), false},
		{"ip", "2001:db8::1", "ip", rules.IP(), true},
		{"ip bad", "300.1.1.1", "ip", rules.IP(), false},
		{"cidr", "10.0.0.0/8", "cidr", rules.CIDR(), true},
		{"cidr bad", "10.0.0.0", "cidr", rules.CIDR(), false},
		{"email", "a@example.com", "email", rules.Email(), true},
		{"email display", "A <a@example.com>", "email", rules.Email(), false},
		{"uuid", "550e8400-e29b-41d4-a716-446655440000", "uuid", rules.UUID(), true},
		{"uuid bad", "550e8400", "uuid", rules.UUID(), false},
		{"identifier", "customer_42", "identifier", rules.Identifier(), true},
		{"identifier bad", "customer/42", "identifier", rules.Identifier(), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := tt.validator.Validate(ctx, tt.value)
			if report.Empty() != tt.valid {
				t.Fatalf("report = %v, valid=%v", report, tt.valid)
			}
			if !tt.valid && !report.HasCode(tt.code) {
				t.Fatalf("missing code %q", tt.code)
			}
		})
	}
}
