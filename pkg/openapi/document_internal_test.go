package openapi

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

func TestWrapDocumentResultRetainsRootClassification(t *testing.T) {
	t.Parallel()

	injected := errors.New("injected decoder failure")
	if _, err := wrapDocumentResult(testDocument{}, injected); !errors.Is(err, ErrInvalidDocument) || !errors.Is(err, injected) {
		t.Fatalf("wrapDocumentResult() error = %v", err)
	}
}

type testDocument struct{}

func (testDocument) Raw() jsonvalue.Value {
	return jsonvalue.Null()
}

func (testDocument) SpecificationVersion() Version {
	version, _ := ParseVersion("3.2.0")
	return version
}
