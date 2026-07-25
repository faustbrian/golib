package jsonschema

import (
	"errors"
	"testing"
)

func TestConfigureVocabulariesReturnsIndexError(t *testing.T) {
	t.Parallel()

	want := errors.New("index failure")
	compiler := &schemaCompiler{indexError: want}
	if err := compiler.configureVocabularies(nil); !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}
