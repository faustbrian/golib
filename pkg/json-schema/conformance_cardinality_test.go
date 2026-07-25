package jsonschema_test

import (
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialCardinalityFixtures(t *testing.T) {
	t.Parallel()

	type dialectFixture struct {
		directory string
		dialect   jsonschema.Dialect
		files     []string
	}

	common := []string{
		"minLength.json",
		"maxLength.json",
		"minItems.json",
		"maxItems.json",
		"required.json",
	}
	withObjects := append(
		append([]string(nil), common...),
		"minProperties.json",
		"maxProperties.json",
	)
	fixtures := []dialectFixture{
		{directory: "draft3", dialect: jsonschema.Draft3, files: common},
		{directory: "draft4", dialect: jsonschema.Draft4, files: withObjects},
		{directory: "draft6", dialect: jsonschema.Draft6, files: withObjects},
		{directory: "draft7", dialect: jsonschema.Draft7, files: withObjects},
		{directory: "draft2019-09", dialect: jsonschema.Draft201909, files: withObjects},
		{directory: "draft2020-12", dialect: jsonschema.Draft202012, files: withObjects},
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
