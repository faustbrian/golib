package validate

import (
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestValidSecuritySchemeTypeDistinguishesDialects(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		kind    string
		dialect specversion.Dialect
		want    bool
	}{
		{kind: "basic", dialect: specversion.DialectSwagger20, want: true},
		{kind: "basic", dialect: specversion.DialectOAS30},
		{kind: "apiKey", dialect: specversion.DialectSwagger20, want: true},
		{kind: "oauth2", dialect: specversion.DialectOAS32, want: true},
		{kind: "http", dialect: specversion.DialectSwagger20},
		{kind: "http", dialect: specversion.DialectOAS30, want: true},
		{kind: "openIdConnect", dialect: specversion.DialectSwagger20},
		{kind: "openIdConnect", dialect: specversion.DialectOAS31, want: true},
		{kind: "mutualTLS", dialect: specversion.DialectOAS30},
		{kind: "mutualTLS", dialect: specversion.DialectOAS31, want: true},
		{kind: "mutualTLS", dialect: specversion.DialectOAS32, want: true},
		{kind: "unknown", dialect: specversion.DialectOAS32},
	} {
		if actual := validSecuritySchemeType(test.kind, test.dialect); actual != test.want {
			t.Errorf("validSecuritySchemeType(%q, %v) = %t, want %t",
				test.kind, test.dialect, actual, test.want)
		}
	}
}
