package openapi_test

import (
	"errors"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
)

func TestParseVersionPreservesSupportedPatchVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    string
		dialect openapi.Dialect
		legacy  bool
	}{
		{input: "2.0", want: "2.0", dialect: openapi.DialectSwagger20, legacy: true},
		{input: "3.0.0", want: "3.0.0", dialect: openapi.DialectOAS30},
		{input: "3.0.1", want: "3.0.1", dialect: openapi.DialectOAS30},
		{input: "3.0.2", want: "3.0.2", dialect: openapi.DialectOAS30},
		{input: "3.0.3", want: "3.0.3", dialect: openapi.DialectOAS30},
		{input: "3.0.4", want: "3.0.4", dialect: openapi.DialectOAS30},
		{input: "3.1.0", want: "3.1.0", dialect: openapi.DialectOAS31},
		{input: "3.1.1", want: "3.1.1", dialect: openapi.DialectOAS31},
		{input: "3.1.2", want: "3.1.2", dialect: openapi.DialectOAS31},
		{input: "3.2.0", want: "3.2.0", dialect: openapi.DialectOAS32},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()

			version, err := openapi.ParseVersion(test.input)
			if err != nil {
				t.Fatalf("ParseVersion() error = %v", err)
			}
			if got := version.String(); got != test.want {
				t.Fatalf("String() = %q, want %q", got, test.want)
			}
			if got := version.Dialect(); got != test.dialect {
				t.Fatalf("Dialect() = %q, want %q", got, test.dialect)
			}
			if got := version.IsLegacy(); got != test.legacy {
				t.Fatalf("IsLegacy() = %t, want %t", got, test.legacy)
			}
		})
	}
}

func TestParseVersionRejectsMalformedAndUnsupportedVersions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  error
	}{
		{input: "", want: openapi.ErrMalformedVersion},
		{input: "3.1", want: openapi.ErrMalformedVersion},
		{input: "03.1.2", want: openapi.ErrMalformedVersion},
		{input: "3.1.2-alpha", want: openapi.ErrMalformedVersion},
		{input: "3.0.5", want: openapi.ErrUnsupportedVersion},
		{input: "3.1.3", want: openapi.ErrUnsupportedVersion},
		{input: "3.2.1", want: openapi.ErrUnsupportedVersion},
		{input: "4.0.0", want: openapi.ErrUnsupportedVersion},
		{input: "1.2", want: openapi.ErrUnsupportedVersion},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()

			_, err := openapi.ParseVersion(test.input)
			if !errors.Is(err, test.want) {
				t.Fatalf("ParseVersion() error = %v, want %v", err, test.want)
			}
		})
	}
}
