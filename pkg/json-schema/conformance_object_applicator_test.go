package jsonschema_test

import (
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialObjectApplicatorFixtures(t *testing.T) {
	t.Parallel()

	type dialectFixture struct {
		directory string
		dialect   jsonschema.Dialect
		files     []string
	}

	legacy := []string{"properties.json", "patternProperties.json", "additionalProperties.json"}
	modern := append(append([]string(nil), legacy...), "propertyNames.json")
	fixtures := []dialectFixture{
		{directory: "draft3", dialect: jsonschema.Draft3, files: legacy},
		{directory: "draft4", dialect: jsonschema.Draft4, files: legacy},
		{directory: "draft6", dialect: jsonschema.Draft6, files: modern},
		{directory: "draft7", dialect: jsonschema.Draft7, files: modern},
		{directory: "draft2019-09", dialect: jsonschema.Draft201909, files: modern},
		{directory: "draft2020-12", dialect: jsonschema.Draft202012, files: modern},
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
