package jsonschema_test

import (
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialOptionalCoreFormatFixtures(t *testing.T) {
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
			files: []string{
				"color.json",
				"date-time.json",
				"date.json",
				"email.json",
				"host-name.json",
				"ip-address.json",
				"ipv6.json",
				"regex.json",
				"time.json",
				"uri.json",
			},
		},
		{
			directory: "draft4",
			dialect:   jsonschema.Draft4,
			files: []string{
				"date-time.json",
				"email.json",
				"hostname.json",
				"ipv4.json",
				"ipv6.json",
				"unknown.json",
				"uri.json",
			},
		},
		{
			directory: "draft6",
			dialect:   jsonschema.Draft6,
			files: []string{
				"date-time.json",
				"email.json",
				"hostname.json",
				"ipv4.json",
				"ipv6.json",
				"json-pointer.json",
				"unknown.json",
				"uri-reference.json",
				"uri-template.json",
				"uri.json",
			},
		},
		{
			directory: "draft7",
			dialect:   jsonschema.Draft7,
			files: []string{
				"date-time.json",
				"date.json",
				"email.json",
				"hostname.json",
				"idn-email.json",
				"idn-hostname.json",
				"iri-reference.json",
				"iri.json",
				"ipv4.json",
				"ipv6.json",
				"json-pointer.json",
				"regex.json",
				"relative-json-pointer.json",
				"time.json",
				"unknown.json",
				"uri-reference.json",
				"uri-template.json",
				"uri.json",
			},
		},
		{
			directory: "draft2019-09",
			dialect:   jsonschema.Draft201909,
			files: []string{
				"date-time.json",
				"date.json",
				"duration.json",
				"email.json",
				"hostname.json",
				"idn-email.json",
				"idn-hostname.json",
				"iri-reference.json",
				"iri.json",
				"ipv4.json",
				"ipv6.json",
				"json-pointer.json",
				"regex.json",
				"relative-json-pointer.json",
				"time.json",
				"uuid.json",
				"unknown.json",
				"uri-reference.json",
				"uri-template.json",
				"uri.json",
			},
		},
		{
			directory: "draft2020-12",
			dialect:   jsonschema.Draft202012,
			files: []string{
				"date-time.json",
				"date.json",
				"duration.json",
				"ecmascript-regex.json",
				"email.json",
				"hostname.json",
				"idn-email.json",
				"idn-hostname.json",
				"iri-reference.json",
				"iri.json",
				"ipv4.json",
				"ipv6.json",
				"json-pointer.json",
				"regex.json",
				"relative-json-pointer.json",
				"time.json",
				"uuid.json",
				"unknown.json",
				"uri-reference.json",
				"uri-template.json",
				"uri.json",
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
					fixture.directory+"/optional/format",
					filename,
					fixture.dialect,
					jsonschema.WithFormatAssertion(),
				)
			})
		}
	}
}

func TestOfficialFormatAssertionVocabularyFixtures(t *testing.T) {
	t.Parallel()

	runOfficialFixtureWithOptions(
		t,
		"draft2020-12/optional",
		"format-assertion.json",
		jsonschema.Draft202012,
		jsonschema.WithResourceLoader(officialRemoteLoader()),
	)
}

func TestOfficialDraft7ContentAssertionFixtures(t *testing.T) {
	t.Parallel()

	runOfficialFixtureWithOptions(
		t,
		"draft7/optional",
		"content.json",
		jsonschema.Draft7,
		jsonschema.WithContentAssertion(),
	)
}
