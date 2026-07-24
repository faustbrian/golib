package validate

import (
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestResponseCodeGrammarExactBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		code    string
		dialect specversion.Dialect
		valid   bool
	}{
		{code: "100", dialect: specversion.DialectSwagger20, valid: true},
		{code: "599", dialect: specversion.DialectSwagger20, valid: true},
		{code: "099", dialect: specversion.DialectOAS32},
		{code: "600", dialect: specversion.DialectOAS32},
		{code: "20", dialect: specversion.DialectOAS32},
		{code: "2000", dialect: specversion.DialectOAS32},
		{code: "2a0", dialect: specversion.DialectOAS32},
		{code: "2XX", dialect: specversion.DialectSwagger20},
		{code: "2XX", dialect: specversion.DialectOAS30, valid: true},
		{code: "2XX", dialect: specversion.DialectOAS31, valid: true},
		{code: "2XX", dialect: specversion.DialectOAS32, valid: true},
		{code: "2xx", dialect: specversion.DialectOAS32},
	} {
		if got := validResponseCode(test.code, test.dialect); got != test.valid {
			t.Errorf("validResponseCode(%q, %q) = %t", test.code, test.dialect, got)
		}
	}
}

func TestSuccessfulResponseGrammarExactBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		code       string
		dialect    specversion.Dialect
		successful bool
	}{
		{code: "199", dialect: specversion.DialectOAS32},
		{code: "200", dialect: specversion.DialectSwagger20, successful: true},
		{code: "299", dialect: specversion.DialectOAS32, successful: true},
		{code: "300", dialect: specversion.DialectOAS32},
		{code: "2XX", dialect: specversion.DialectSwagger20},
		{code: "2XX", dialect: specversion.DialectOAS30, successful: true},
		{code: "2XX", dialect: specversion.DialectOAS31, successful: true},
		{code: "2XX", dialect: specversion.DialectOAS32, successful: true},
		{code: "20X", dialect: specversion.DialectOAS32},
	} {
		responses := testValidationValue(t, `{"`+test.code+`":{}}`)
		if got := hasSuccessfulResponse(responses, test.dialect); got != test.successful {
			t.Errorf("hasSuccessfulResponse(%q, %q) = %t", test.code, test.dialect, got)
		}
	}
}

func TestResponseContainerRecognizesOnlyCodesAndDefault(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		raw     string
		dialect specversion.Dialect
		valid   bool
	}{
		{raw: `{"default":{}}`, dialect: specversion.DialectSwagger20, valid: true},
		{raw: `{"200":{}}`, dialect: specversion.DialectSwagger20, valid: true},
		{raw: `{"2XX":{}}`, dialect: specversion.DialectOAS32, valid: true},
		{raw: `{"x-note":true}`, dialect: specversion.DialectOAS32},
		{raw: `{"600":{}}`, dialect: specversion.DialectOAS32},
	} {
		if got := hasResponseCode(
			testValidationValue(t, test.raw), test.dialect,
		); got != test.valid {
			t.Errorf("hasResponseCode(%s, %q) = %t", test.raw, test.dialect, got)
		}
	}
}
