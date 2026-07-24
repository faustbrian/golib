package jsonschema_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

type fixtureGroup struct {
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
	Tests       []fixtureCase   `json:"tests"`
}

type fixtureCase struct {
	Description string          `json:"description"`
	Data        json.RawMessage `json:"data"`
	Valid       bool            `json:"valid"`
}

func TestOfficialTypeFixtures(t *testing.T) {
	t.Parallel()

	dialects := []struct {
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

	for _, item := range dialects {
		item := item
		t.Run(item.directory, func(t *testing.T) {
			t.Parallel()
			runOfficialFixture(t, item.directory, "type.json", item.dialect)
		})
	}
}

func runOfficialFixture(
	t *testing.T,
	directory string,
	filename string,
	dialect jsonschema.Dialect,
) {
	runOfficialFixtureWithOptions(t, directory, filename, dialect)
}

func runOfficialFixtureWithOptions(
	t *testing.T,
	directory string,
	filename string,
	dialect jsonschema.Dialect,
	options ...jsonschema.Option,
) {
	t.Helper()

	path := filepath.Join(
		"testdata",
		"official",
		"JSON-Schema-Test-Suite",
		"tests",
		directory,
		filename,
	)
	// #nosec G304 -- path is confined to the pinned fixture tree.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var groups []fixtureGroup
	if err := json.Unmarshal(raw, &groups); err != nil {
		t.Fatal(err)
	}

	compilerOptions := append([]jsonschema.Option{jsonschema.WithDialect(dialect)}, options...)
	compiler, err := jsonschema.NewCompiler(compilerOptions...)
	if err != nil {
		t.Fatal(err)
	}

	for _, group := range groups {
		schema, err := compiler.Compile(context.Background(), group.Schema)
		if err != nil {
			t.Fatalf("%s: compile: %v", group.Description, err)
		}

		for _, test := range group.Tests {
			result, err := schema.Validate(context.Background(), test.Data)
			if err != nil {
				t.Fatalf(
					"%s/%s: validate: %v",
					group.Description,
					test.Description,
					err,
				)
			}
			if result.Valid != test.Valid {
				t.Errorf(
					"%s/%s: got valid=%t, want %t",
					group.Description,
					test.Description,
					result.Valid,
					test.Valid,
				)
			}
		}
	}
}
