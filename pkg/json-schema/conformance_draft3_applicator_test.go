package jsonschema_test

import (
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialDraft3ApplicatorFixtures(t *testing.T) {
	t.Parallel()

	for _, filename := range []string{"extends.json", "disallow.json"} {
		filename := filename
		t.Run(filename, func(t *testing.T) {
			t.Parallel()
			runOfficialFixture(t, "draft3", filename, jsonschema.Draft3)
		})
	}
}
