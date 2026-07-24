package jsonschema_test

import (
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialNumericFixtures(t *testing.T) {
	t.Parallel()

	type dialectFixture struct {
		directory string
		dialect   jsonschema.Dialect
		files     []string
	}

	fixtures := []dialectFixture{
		{
			directory: "draft3",
			dialect:   jsonschema.Draft3,
			files:     []string{"minimum.json", "maximum.json", "divisibleBy.json"},
		},
		{
			directory: "draft4",
			dialect:   jsonschema.Draft4,
			files:     []string{"minimum.json", "maximum.json", "multipleOf.json"},
		},
		{
			directory: "draft6",
			dialect:   jsonschema.Draft6,
			files: []string{
				"minimum.json",
				"maximum.json",
				"exclusiveMinimum.json",
				"exclusiveMaximum.json",
				"multipleOf.json",
			},
		},
		{
			directory: "draft7",
			dialect:   jsonschema.Draft7,
			files: []string{
				"minimum.json",
				"maximum.json",
				"exclusiveMinimum.json",
				"exclusiveMaximum.json",
				"multipleOf.json",
			},
		},
		{
			directory: "draft2019-09",
			dialect:   jsonschema.Draft201909,
			files: []string{
				"minimum.json",
				"maximum.json",
				"exclusiveMinimum.json",
				"exclusiveMaximum.json",
				"multipleOf.json",
			},
		},
		{
			directory: "draft2020-12",
			dialect:   jsonschema.Draft202012,
			files: []string{
				"minimum.json",
				"maximum.json",
				"exclusiveMinimum.json",
				"exclusiveMaximum.json",
				"multipleOf.json",
			},
		},
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
