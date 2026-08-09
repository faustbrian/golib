package jsonschema

import "testing"

func TestFilesystemResourcePathPolicy(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"schema.json", "nested/schema.json"} {
		if !validFilesystemResourcePath(name) {
			t.Fatalf("valid resource path %q was rejected", name)
		}
	}
	for _, name := range []string{"", ".", "../schema.json", "/schema.json"} {
		if validFilesystemResourcePath(name) {
			t.Fatalf("unsafe resource path %q was accepted", name)
		}
	}
}
