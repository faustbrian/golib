package specversion_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestParseCoversEveryPinnedVersionAndSyntaxClass(t *testing.T) {
	t.Parallel()

	for value, dialect := range map[string]specversion.Dialect{
		"2.0":   specversion.DialectSwagger20,
		"3.0.0": specversion.DialectOAS30,
		"3.0.1": specversion.DialectOAS30,
		"3.0.2": specversion.DialectOAS30,
		"3.0.3": specversion.DialectOAS30,
		"3.0.4": specversion.DialectOAS30,
		"3.1.0": specversion.DialectOAS31,
		"3.1.1": specversion.DialectOAS31,
		"3.1.2": specversion.DialectOAS31,
		"3.2.0": specversion.DialectOAS32,
	} {
		version, err := specversion.Parse(value)
		if err != nil {
			t.Fatal(err)
		}
		if version.String() != value || version.Dialect() != dialect {
			t.Fatalf("Parse(%q) = %#v", value, version)
		}
		if version.IsLegacy() != (dialect == specversion.DialectSwagger20) {
			t.Fatalf("Parse(%q).IsLegacy() mismatch", value)
		}
	}
	for _, value := range []string{"", "1", "1.", ".1", "01.2", "1.02", "1.a", "1.2.3.4"} {
		if _, err := specversion.Parse(value); !errors.Is(err, specversion.ErrMalformedVersion) {
			t.Fatalf("Parse(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{"1.2", "2.1", "3.3.0"} {
		if _, err := specversion.Parse(value); !errors.Is(err, specversion.ErrUnsupportedVersion) {
			t.Fatalf("Parse(%q) error = %v", value, err)
		}
	}
}
