package jsonschema_test

import (
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialPatternFixtures(t *testing.T) {
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
			runOfficialFixture(t, fixture.directory, "pattern.json", fixture.dialect)
		})
	}
}
