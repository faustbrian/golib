package jsonschema_test

import (
	"context"
	"errors"
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestCompileRejectsMalformedResourceIdentifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dialect    jsonschema.Dialect
		identifier string
	}{
		{name: "draft3", dialect: jsonschema.Draft3, identifier: "id"},
		{name: "draft4", dialect: jsonschema.Draft4, identifier: "id"},
		{name: "draft6", dialect: jsonschema.Draft6, identifier: "$id"},
		{name: "draft7", dialect: jsonschema.Draft7, identifier: "$id"},
		{name: "draft2019-09", dialect: jsonschema.Draft201909, identifier: "$id"},
		{name: "draft2020-12", dialect: jsonschema.Draft202012, identifier: "$id"},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			compiler, err := jsonschema.NewCompiler(
				jsonschema.WithDialect(testCase.dialect),
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = compiler.Compile(
				context.Background(),
				[]byte(`{"`+testCase.identifier+`":"%"}`),
			)
			if !errors.Is(err, jsonschema.ErrInvalidSchema) {
				t.Fatalf("got %v, want ErrInvalidSchema", err)
			}
		})
	}
}

func TestCompileRejectsUnnormalizableResourceIdentifier(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(
		context.Background(),
		[]byte(`{"$id":"https://example.test/?%"}`),
	)
	if !errors.Is(err, jsonschema.ErrInvalidSchema) {
		t.Fatalf("got %v, want ErrInvalidSchema", err)
	}
}

func TestCompileRejectsDuplicateResourceIdentifiers(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(context.Background(), []byte(`{
		"$id":"https://example.test/root",
		"$defs":{
			"first":{"$id":"duplicate"},
			"second":{"$id":"duplicate"}
		}
	}`))
	if !errors.Is(err, jsonschema.ErrInvalidSchema) {
		t.Fatalf("got %v, want ErrInvalidSchema", err)
	}
}

func TestCompileResolvesEquivalentResourceIdentifiers(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{
		"$defs":{
			"target":{
				"$id":"HTTPS://EXAMPLE.TEST:443/a/../%7Eschema",
				"type":"integer"
			}
		},
		"$ref":"https://example.test/~schema"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := schema.Validate(context.Background(), []byte(`7`))
	if err != nil || !result.Valid {
		t.Fatalf("got valid=%t err=%v", result.Valid, err)
	}
}

func TestCompileRejectsEquivalentDuplicateResourceIdentifiers(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(context.Background(), []byte(`{
		"$defs":{
			"first":{"$id":"HTTPS://EXAMPLE.TEST:443/%7Eschema"},
			"second":{"$id":"https://example.test/~schema"}
		}
	}`))
	if !errors.Is(err, jsonschema.ErrInvalidSchema) {
		t.Fatalf("got %v, want ErrInvalidSchema", err)
	}
}

func TestCompileRejectsModernIdentifiersWithFragments(t *testing.T) {
	t.Parallel()

	for _, dialect := range []jsonschema.Dialect{
		jsonschema.Draft201909,
		jsonschema.Draft202012,
	} {
		dialect := dialect
		t.Run(string(dialect), func(t *testing.T) {
			t.Parallel()

			compiler, err := jsonschema.NewCompiler(
				jsonschema.WithDialect(dialect),
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = compiler.Compile(
				context.Background(),
				[]byte(`{"$id":"https://example.test/schema#fragment"}`),
			)
			if !errors.Is(err, jsonschema.ErrInvalidSchema) {
				t.Fatalf("got %v, want ErrInvalidSchema", err)
			}
		})
	}
}

func TestCompileRejectsDuplicateAnchors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		first  string
		second string
		schema string
	}{
		{name: "anchor", first: `"$anchor":"node"`, second: `"$anchor":"node"`},
		{
			name:   "anchor and dynamic anchor",
			first:  `"$anchor":"node"`,
			second: `"$dynamicAnchor":"node"`,
		},
		{
			name:   "dynamic anchor",
			first:  `"$dynamicAnchor":"node"`,
			second: `"$dynamicAnchor":"node"`,
		},
		{
			name:   "same schema",
			schema: `{"$anchor":"node","$dynamicAnchor":"node"}`,
		},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			compiler, err := jsonschema.NewCompiler()
			if err != nil {
				t.Fatal(err)
			}
			schema := testCase.schema
			if schema == "" {
				schema = `{
				"$id":"https://example.test/root",
				"$defs":{
					"first":{` + testCase.first + `},
					"second":{` + testCase.second + `}
				}
			}`
			}
			_, err = compiler.Compile(context.Background(), []byte(schema))
			if !errors.Is(err, jsonschema.ErrInvalidSchema) {
				t.Fatalf("got %v, want ErrInvalidSchema", err)
			}
		})
	}
}
