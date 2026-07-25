package jsonschema_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialMandatoryFixtures(t *testing.T) {
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
		entries, err := os.ReadDir(filepath.Join(
			"testdata",
			"official",
			"JSON-Schema-Test-Suite",
			"tests",
			fixture.directory,
		))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			entry := entry
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			t.Run(fixture.directory+"/"+entry.Name(), func(t *testing.T) {
				t.Parallel()
				runOfficialFixtureWithOptions(
					t,
					fixture.directory,
					entry.Name(),
					fixture.dialect,
					jsonschema.WithResourceLoader(officialRemoteLoader()),
				)
			})
		}
	}
}

func TestOfficialOptionalFixtures(t *testing.T) {
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
		root := filepath.Join(
			"testdata",
			"official",
			"JSON-Schema-Test-Suite",
			"tests",
			fixture.directory,
			"optional",
		)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			t.Run(fixture.directory+"/"+relative, func(t *testing.T) {
				t.Parallel()
				options := []jsonschema.Option{
					jsonschema.WithResourceLoader(officialRemoteLoader()),
				}
				if strings.HasPrefix(relative, "format"+string(filepath.Separator)) {
					options = append(options, jsonschema.WithFormatAssertion())
				}
				if relative == "content.json" {
					options = append(options, jsonschema.WithContentAssertion())
				}
				runOfficialFixtureWithOptions(
					t,
					fixture.directory+"/optional/"+filepath.Dir(relative),
					filepath.Base(relative),
					fixture.dialect,
					options...,
				)
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
