package jsonschema_test

import (
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialArrayApplicatorFixtures(t *testing.T) {
	t.Parallel()

	type dialectFixture struct {
		directory string
		dialect   jsonschema.Dialect
		files     []string
	}

	legacy := []string{"items.json", "additionalItems.json"}
	fixtures := []dialectFixture{
		{directory: "draft3", dialect: jsonschema.Draft3, files: legacy},
		{directory: "draft4", dialect: jsonschema.Draft4, files: legacy},
		{directory: "draft6", dialect: jsonschema.Draft6, files: legacy},
		{directory: "draft7", dialect: jsonschema.Draft7, files: legacy},
		{directory: "draft2019-09", dialect: jsonschema.Draft201909, files: legacy},
		{
			directory: "draft2020-12",
			dialect:   jsonschema.Draft202012,
			files:     []string{"prefixItems.json", "items.json"},
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
