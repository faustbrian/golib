package rules_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/validation/rules"
)

func FuzzUnicodeAndMalformedPrimitives(f *testing.F) {
	f.Add("é", "https://example.com", "a@example.com")
	f.Add(string([]byte{0xff, 0xfe}), "://", "not-mail")
	f.Fuzz(func(t *testing.T, text, rawURL, email string) {
		ctx := contextFor(t)
		_ = rules.ByteLength(0, 1024).Validate(ctx, text)
		_ = rules.RuneLength(0, 1024).Validate(ctx, text)
		_ = rules.URL().Validate(ctx, rawURL)
		_ = rules.Hostname().Validate(ctx, text)
		_ = rules.IP().Validate(ctx, text)
		_ = rules.CIDR().Validate(ctx, text)
		_ = rules.Email().Validate(ctx, email)
		_ = rules.UUID().Validate(ctx, text)
		_ = rules.Identifier().Validate(ctx, text)
	})
}
