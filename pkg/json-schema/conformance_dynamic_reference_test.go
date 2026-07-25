package jsonschema_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialDynamicReferenceFixtures(t *testing.T) {
	t.Parallel()

	t.Run("draft2019-09", func(t *testing.T) {
		t.Parallel()
		runOfficialFixtureWithOptions(
			t,
			"draft2019-09",
			"recursiveRef.json",
			jsonschema.Draft201909,
		)
	})
	t.Run("draft2020-12", func(t *testing.T) {
		t.Parallel()
		runOfficialFixtureWithOptions(
			t,
			"draft2020-12",
			"dynamicRef.json",
			jsonschema.Draft202012,
			jsonschema.WithResourceLoader(officialRemoteLoader()),
		)
	})
}

func officialRemoteLoader() jsonschema.ResourceLoader {
	metaSchemas := make(map[string]string)
	manifest, manifestErr := os.ReadFile(filepath.Join(
		"specification",
		"official-meta-schemas.sources.tsv",
	))
	if manifestErr == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(manifest)), "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) == 2 {
				metaSchemas[fields[0]] = fields[1]
			}
		}
	}
	return jsonschema.ResourceLoaderFunc(func(ctx context.Context, identifier string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parsed, err := url.Parse(identifier)
		if err != nil {
			return nil, fmt.Errorf("unauthorized test resource %q", identifier)
		}
		if parsed.Host == "json-schema.org" &&
			(parsed.Scheme == "http" || parsed.Scheme == "https") {
			if manifestErr != nil {
				return nil, manifestErr
			}
			parsed.Scheme = "https"
			path, exists := metaSchemas[parsed.String()]
			if !exists {
				return nil, fmt.Errorf("unpinned meta-schema %q", identifier)
			}
			// #nosec G304 -- path comes from the pinned meta-schema manifest.
			return os.ReadFile(path)
		}
		if parsed.Scheme != "http" || parsed.Host != "localhost:1234" {
			return nil, fmt.Errorf("unauthorized test resource %q", identifier)
		}
		relative := strings.TrimPrefix(filepath.Clean(parsed.Path), string(filepath.Separator))
		if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("unsafe test resource path %q", parsed.Path)
		}

		// #nosec G304 -- relative is confined to the pinned remotes tree.
		return os.ReadFile(filepath.Join(
			"testdata",
			"official",
			"JSON-Schema-Test-Suite",
			"remotes",
			relative,
		))
	})
}
