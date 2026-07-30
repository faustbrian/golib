package defaults

import (
	"errors"
	"reflect"
	"testing"

	config "github.com/faustbrian/golib/pkg/config"
)

func TestCollectRejectsSchemaBeyondDepthLimitInternally(t *testing.T) {
	t.Parallel()

	type settings struct{ Value string }
	err := collect(
		reflect.TypeFor[settings](),
		make(map[string]any),
		make(map[string]config.Origin),
		"nested",
		maxSchemaDepth+1,
		make(map[reflect.Type]bool),
	)
	if err == nil {
		t.Fatal("collect() error = nil")
	}

	if err := collect(
		reflect.TypeFor[struct{}](),
		make(map[string]any),
		make(map[string]config.Origin),
		"allowed",
		maxSchemaDepth,
		make(map[reflect.Type]bool),
	); err != nil {
		t.Fatalf("collect(max depth) error = %v", err)
	}

	type nestedAtLimit struct {
		Nested struct {
			Value string `config:"value" default:"loaded"`
		} `config:"nested"`
	}
	err = collect(
		reflect.TypeFor[nestedAtLimit](),
		make(map[string]any),
		make(map[string]config.Origin),
		"",
		maxSchemaDepth,
		make(map[reflect.Type]bool),
	)
	var schemaErr *SchemaError
	if !errors.As(err, &schemaErr) || schemaErr.Path != "nested" {
		t.Fatalf("collect(nested at limit) error = %#v", err)
	}
}

func TestCollectContinuesAcrossNonDefaultFieldsInternally(t *testing.T) {
	t.Parallel()

	type settings struct {
		_      string
		Nested struct {
			Value string `config:"value" default:"nested"`
		} `config:"nested"`
		WithoutDefault string `config:"without_default"`
		Final          string `config:"final" default:"loaded"`
	}
	tree := make(map[string]any)
	if err := collect(
		reflect.TypeFor[settings](),
		tree,
		make(map[string]config.Origin),
		"",
		1,
		make(map[reflect.Type]bool),
	); err != nil {
		t.Fatalf("collect() error = %v", err)
	}
	nested, ok := tree["nested"].(map[string]any)
	if !ok || nested["value"] != "nested" || tree["final"] != "loaded" {
		t.Fatalf("collect() tree = %#v", tree)
	}
}
