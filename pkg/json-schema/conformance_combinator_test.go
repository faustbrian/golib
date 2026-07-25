package jsonschema_test

import (
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialCombinatorFixtures(t *testing.T) {
	t.Parallel()

	type dialectFixture struct {
		directory string
		dialect   jsonschema.Dialect
		files     []string
	}

	base := []string{"allOf.json", "anyOf.json", "oneOf.json", "not.json"}
	withConditional := append(append([]string(nil), base...), "if-then-else.json")
	fixtures := []dialectFixture{
		{directory: "draft4", dialect: jsonschema.Draft4, files: base},
		{directory: "draft6", dialect: jsonschema.Draft6, files: base},
		{directory: "draft7", dialect: jsonschema.Draft7, files: withConditional},
		{directory: "draft2019-09", dialect: jsonschema.Draft201909, files: withConditional},
		{directory: "draft2020-12", dialect: jsonschema.Draft202012, files: withConditional},
	}

	for _, fixture := range fixtures {
		fixture := fixture
		for _, filename := range fixture.files {
			filename := filename
			t.Run(fixture.directory+"/"+filename, func(t *testing.T) {
				t.Parallel()
				runOfficialFixture(
					t,
					fixture.directory,
					filename,
					fixture.dialect,
				)
			})
		}
	}
}
