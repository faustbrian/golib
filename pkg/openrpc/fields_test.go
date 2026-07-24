package openrpc_test

import (
	"errors"
	"reflect"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func TestNewExtensionsRequiresUniquePrefixedNames(t *testing.T) {
	t.Parallel()

	value, err := jsonvalue.Parse([]byte(`null`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}

	_, err = openrpc.NewExtensions(openrpc.Field{Name: "future", Value: value})
	if !errors.Is(err, openrpc.ErrInvalidExtensionName) {
		t.Fatalf("error = %v, want ErrInvalidExtensionName", err)
	}

	_, err = openrpc.NewExtensions(
		openrpc.Field{Name: "x-feature", Value: value},
		openrpc.Field{Name: "x-feature", Value: value},
	)
	if !errors.Is(err, openrpc.ErrDuplicateField) {
		t.Fatalf("error = %v, want ErrDuplicateField", err)
	}
}

func TestFieldsAreDeterministicAndOwnershipSafe(t *testing.T) {
	t.Parallel()

	one, err := jsonvalue.Parse([]byte(`1`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	two, err := jsonvalue.Parse([]byte(`2`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	input := []openrpc.Field{
		{Name: "x-zeta", Value: two},
		{Name: "x-alpha", Value: one},
	}

	fields, err := openrpc.NewExtensions(input...)
	if err != nil {
		t.Fatal(err)
	}
	input[0].Name = "changed"
	if !reflect.DeepEqual(fields.Names(), []string{"x-alpha", "x-zeta"}) {
		t.Fatalf("Names() = %#v", fields.Names())
	}

	names := fields.Names()
	names[0] = "changed"
	if fields.Names()[0] != "x-alpha" {
		t.Fatal("Names exposed mutable internal storage")
	}
	value, ok := fields.Get("x-zeta")
	if !ok || string(value.Bytes()) != "2" {
		t.Fatalf("Get(x-zeta) = (%q, %t)", value.Bytes(), ok)
	}
	if _, ok := fields.Get("missing"); ok {
		t.Fatal("Get reported a missing field")
	}
}
