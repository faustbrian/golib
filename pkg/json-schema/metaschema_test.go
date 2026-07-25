package jsonschema_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestOfficialMetaSchemasCompileAgainstTheirDialect(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(filepath.Join("testdata", "official", "meta-schemas"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dialect, exists := metaSchemaDialect(entry.Name())
		if !exists {
			t.Fatalf("no dialect mapping for %q", entry.Name())
		}
		root := filepath.Join("testdata", "official", "meta-schemas", entry.Name())
		err := filepath.WalkDir(root, func(path string, item os.DirEntry, err error) error {
			if err != nil || item.IsDir() || !strings.HasSuffix(path, ".json") {
				return err
			}
			t.Run(strings.TrimPrefix(path, root+string(filepath.Separator)), func(t *testing.T) {
				t.Parallel()
				// #nosec G304 -- path comes from the pinned meta-schema manifest.
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				compiler, err := jsonschema.NewCompiler(
					jsonschema.WithDialect(dialect),
					jsonschema.WithResourceLoader(officialRemoteLoader()),
				)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := compiler.Compile(context.Background(), raw); err != nil {
					t.Fatal(err)
				}
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func metaSchemaDialect(directory string) (jsonschema.Dialect, bool) {
	switch directory {
	case "draft-03":
		return jsonschema.Draft3, true
	case "draft-04":
		return jsonschema.Draft4, true
	case "draft-06":
		return jsonschema.Draft6, true
	case "draft-07":
		return jsonschema.Draft7, true
	case "draft2019-09":
		return jsonschema.Draft201909, true
	case "draft2020-12":
		return jsonschema.Draft202012, true
	default:
		return "", false
	}
}
