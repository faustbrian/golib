package jsonschema_test

import (
	"context"
	"errors"
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialAnchorFixtures(t *testing.T) {
	t.Parallel()

	fixtures := []struct {
		directory string
		dialect   jsonschema.Dialect
	}{
		{directory: "draft2019-09", dialect: jsonschema.Draft201909},
		{directory: "draft2020-12", dialect: jsonschema.Draft202012},
	}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.directory, func(t *testing.T) {
			t.Parallel()
			runOfficialFixture(t, fixture.directory, "anchor.json", fixture.dialect)
		})
	}
}

func TestEscapedLocalJSONPointerReference(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{
		"$defs": {"percent%field": {"type": "integer"}},
		"$ref": "#/$defs/percent%25field"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := schema.Validate(context.Background(), []byte(`"invalid"`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("string unexpectedly satisfied referenced integer schema")
	}
}

func TestReplacingReferenceIgnoresSiblingIdentifier(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler(jsonschema.WithDialect(jsonschema.Draft7))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{
		"$id": "https://example.test/base/",
		"definitions": {
			"number": {"$id": "value", "type": "number"}
		},
		"allOf": [{
			"$id": "https://example.test/ignored/",
			"$ref": "value"
		}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := schema.Validate(context.Background(), []byte(`"invalid"`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("string unexpectedly satisfied referenced number schema")
	}
}

func TestOfficialRemoteReferenceFixtures(t *testing.T) {
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
			runOfficialFixtureWithOptions(
				t,
				fixture.directory,
				"refRemote.json",
				fixture.dialect,
				jsonschema.WithResourceLoader(officialRemoteLoader()),
			)
		})
	}
}

func TestOfficialReferenceAndDefinitionFixtures(t *testing.T) {
	t.Parallel()

	type dialectFixture struct {
		directory string
		dialect   jsonschema.Dialect
		files     []string
	}
	fixtures := []dialectFixture{
		{directory: "draft3", dialect: jsonschema.Draft3, files: []string{"ref.json"}},
		{
			directory: "draft4",
			dialect:   jsonschema.Draft4,
			files:     []string{"ref.json", "definitions.json"},
		},
		{
			directory: "draft6",
			dialect:   jsonschema.Draft6,
			files:     []string{"ref.json", "definitions.json"},
		},
		{
			directory: "draft7",
			dialect:   jsonschema.Draft7,
			files:     []string{"ref.json", "definitions.json"},
		},
		{
			directory: "draft2019-09",
			dialect:   jsonschema.Draft201909,
			files:     []string{"ref.json", "defs.json"},
		},
		{
			directory: "draft2020-12",
			dialect:   jsonschema.Draft202012,
			files:     []string{"ref.json", "defs.json"},
		},
	}

	for _, fixture := range fixtures {
		fixture := fixture
		for _, filename := range fixture.files {
			filename := filename
			t.Run(fixture.directory+"/"+filename, func(t *testing.T) {
				t.Parallel()
				runOfficialFixtureWithOptions(
					t,
					fixture.directory,
					filename,
					fixture.dialect,
					jsonschema.WithResourceLoader(officialRemoteLoader()),
				)
			})
		}
	}
}

func TestAnchorKeywordsDoNotLeakIntoEarlierDialects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dialect    jsonschema.Dialect
		definition string
		keyword    string
	}{
		{name: "draft3 anchor", dialect: jsonschema.Draft3, definition: "definitions", keyword: "$anchor"},
		{name: "draft4 anchor", dialect: jsonschema.Draft4, definition: "definitions", keyword: "$anchor"},
		{name: "draft6 anchor", dialect: jsonschema.Draft6, definition: "definitions", keyword: "$anchor"},
		{name: "draft7 anchor", dialect: jsonschema.Draft7, definition: "definitions", keyword: "$anchor"},
		{name: "draft3 dynamic anchor", dialect: jsonschema.Draft3, definition: "definitions", keyword: "$dynamicAnchor"},
		{name: "draft4 dynamic anchor", dialect: jsonschema.Draft4, definition: "definitions", keyword: "$dynamicAnchor"},
		{name: "draft6 dynamic anchor", dialect: jsonschema.Draft6, definition: "definitions", keyword: "$dynamicAnchor"},
		{name: "draft7 dynamic anchor", dialect: jsonschema.Draft7, definition: "definitions", keyword: "$dynamicAnchor"},
		{name: "draft2019-09 dynamic anchor", dialect: jsonschema.Draft201909, definition: "$defs", keyword: "$dynamicAnchor"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			compiler, err := jsonschema.NewCompiler(jsonschema.WithDialect(test.dialect))
			if err != nil {
				t.Fatal(err)
			}
			document := []byte(`{"` + test.definition + `":{"target":{"` +
				test.keyword + `":"leaked"}},"$ref":"#leaked"}`)
			_, err = compiler.Compile(context.Background(), document)
			if !errors.Is(err, jsonschema.ErrInvalidSchema) {
				t.Fatalf("got %v, want unresolved anchor error", err)
			}
		})
	}
}
