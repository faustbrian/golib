package jsonschema_test

import (
	"context"
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialFormatAnnotationFixtures(t *testing.T) {
	t.Parallel()

	fixtures := []struct {
		directory string
		dialect   jsonschema.Dialect
	}{
		{directory: "draft3", dialect: jsonschema.Draft3},
		{directory: "draft4", dialect: jsonschema.Draft4},
		{directory: "draft6", dialect: jsonschema.Draft6},
		{directory: "draft7", dialect: jsonschema.Draft7},
		{directory: "draft2019-09", dialect: jsonschema.Draft201909},
		{directory: "draft2020-12", dialect: jsonschema.Draft202012},
	}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.directory, func(t *testing.T) {
			t.Parallel()
			runOfficialFixture(t, fixture.directory, "format.json", fixture.dialect)
		})
	}
}

func TestStandardFormatsDoNotLeakAcrossDialects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dialect jsonschema.Dialect
		format  string
		value   string
	}{
		{name: "draft3 modern hostname", dialect: jsonschema.Draft3, format: "hostname", value: "not a hostname"},
		{name: "draft4 historical host-name", dialect: jsonschema.Draft4, format: "host-name", value: "not a hostname"},
		{name: "draft4 date", dialect: jsonschema.Draft4, format: "date", value: "not a date"},
		{name: "draft6 regex", dialect: jsonschema.Draft6, format: "regex", value: "("},
		{name: "draft7 duration", dialect: jsonschema.Draft7, format: "duration", value: "not a duration"},
		{name: "draft7 uuid", dialect: jsonschema.Draft7, format: "uuid", value: "not a uuid"},
		{name: "draft2020-12 color", dialect: jsonschema.Draft202012, format: "color", value: "not a color"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			compiler, err := jsonschema.NewCompiler(
				jsonschema.WithDialect(test.dialect),
				jsonschema.WithFormatAssertion(),
			)
			if err != nil {
				t.Fatal(err)
			}
			schema, err := compiler.Compile(
				context.Background(),
				[]byte(`{"format":"`+test.format+`"}`),
			)
			if err != nil {
				t.Fatal(err)
			}
			result, err := schema.Validate(
				context.Background(),
				[]byte(`"`+test.value+`"`),
			)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Valid {
				t.Fatal("format from another dialect unexpectedly asserted")
			}
		})
	}
}

func TestCustomFormatsCanExtendHistoricalDialects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		format  string
		valid   bool
		checker jsonschema.FormatChecker
	}{
		{
			name:   "newer standard name",
			format: "hostname",
			valid:  false,
			checker: jsonschema.FormatFunc(func(context.Context, string) (bool, error) {
				return false, nil
			}),
		},
		{
			name:   "historical standard replacement",
			format: "time",
			valid:  true,
			checker: jsonschema.FormatFunc(func(context.Context, string) (bool, error) {
				return true, nil
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			compiler, err := jsonschema.NewCompiler(
				jsonschema.WithDialect(jsonschema.Draft3),
				jsonschema.WithFormatAssertion(),
				jsonschema.WithFormat(test.format, test.checker),
			)
			if err != nil {
				t.Fatal(err)
			}
			schema, err := compiler.Compile(
				context.Background(),
				[]byte(`{"format":"`+test.format+`"}`),
			)
			if err != nil {
				t.Fatal(err)
			}
			result, err := schema.Validate(context.Background(), []byte(`"invalid"`))
			if err != nil {
				t.Fatal(err)
			}
			if result.Valid != test.valid {
				t.Fatalf("got valid=%t, want %t", result.Valid, test.valid)
			}
		})
	}
}
