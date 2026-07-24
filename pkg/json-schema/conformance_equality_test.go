package jsonschema_test

import (
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialEqualityFixtures(t *testing.T) {
	t.Parallel()

	fixtures := []struct {
		directory string
		filename  string
		dialect   jsonschema.Dialect
	}{
		{directory: "draft3", filename: "enum.json", dialect: jsonschema.Draft3},
		{directory: "draft4", filename: "enum.json", dialect: jsonschema.Draft4},
		{directory: "draft6", filename: "enum.json", dialect: jsonschema.Draft6},
		{directory: "draft6", filename: "const.json", dialect: jsonschema.Draft6},
		{directory: "draft7", filename: "enum.json", dialect: jsonschema.Draft7},
		{directory: "draft7", filename: "const.json", dialect: jsonschema.Draft7},
		{directory: "draft2019-09", filename: "enum.json", dialect: jsonschema.Draft201909},
		{directory: "draft2019-09", filename: "const.json", dialect: jsonschema.Draft201909},
		{directory: "draft2020-12", filename: "enum.json", dialect: jsonschema.Draft202012},
		{directory: "draft2020-12", filename: "const.json", dialect: jsonschema.Draft202012},
	}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.directory+"/"+fixture.filename, func(t *testing.T) {
			t.Parallel()
			runOfficialFixture(
				t,
				fixture.directory,
				fixture.filename,
				fixture.dialect,
			)
		})
	}
}
