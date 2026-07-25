package jsonschema_test

import (
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialVocabularyFixtures(t *testing.T) {
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
			runOfficialFixtureWithOptions(
				t,
				fixture.directory,
				"vocabulary.json",
				fixture.dialect,
				jsonschema.WithResourceLoader(officialRemoteLoader()),
			)
		})
	}
}
