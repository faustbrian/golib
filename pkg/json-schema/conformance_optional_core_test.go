package jsonschema_test

import (
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialOptionalCoreFixtures(t *testing.T) {
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
			files:     []string{"bignum.json", "zeroTerminatedFloats.json"},
		},
		{
			directory: "draft4",
			dialect:   jsonschema.Draft4,
			files:     []string{"bignum.json", "float-overflow.json", "id.json", "zeroTerminatedFloats.json"},
		},
		{
			directory: "draft6",
			dialect:   jsonschema.Draft6,
			files: []string{
				"bignum.json",
				"float-overflow.json",
				"id.json",
				"unknownKeyword.json",
			},
		},
		{
			directory: "draft7",
			dialect:   jsonschema.Draft7,
			files: []string{
				"bignum.json",
				"cross-draft.json",
				"float-overflow.json",
				"id.json",
				"unknownKeyword.json",
			},
		},
		{
			directory: "draft2019-09",
			dialect:   jsonschema.Draft201909,
			files: []string{
				"anchor.json",
				"bignum.json",
				"cross-draft.json",
				"dependencies-compatibility.json",
				"float-overflow.json",
				"id.json",
				"no-schema.json",
				"refOfUnknownKeyword.json",
				"unknownKeyword.json",
			},
		},
		{
			directory: "draft2020-12",
			dialect:   jsonschema.Draft202012,
			files: []string{
				"anchor.json",
				"bignum.json",
				"cross-draft.json",
				"dependencies-compatibility.json",
				"dynamicRef.json",
				"float-overflow.json",
				"id.json",
				"no-schema.json",
				"refOfUnknownKeyword.json",
				"unknownKeyword.json",
			},
		},
	}

	for _, fixture := range fixtures {
		fixture := fixture
		for _, filename := range fixture.files {
			filename := filename
			t.Run(fixture.directory+"/"+filename, func(t *testing.T) {
				t.Parallel()
				runOfficialFixtureWithOptions(
					t,
					fixture.directory+"/optional",
					filename,
					fixture.dialect,
					jsonschema.WithResourceLoader(officialRemoteLoader()),
				)
			})
		}
	}
}
