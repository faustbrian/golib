package reference_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestPointerEvaluatesRFC6901Tokens(t *testing.T) {
	t.Parallel()

	root, err := jsonvalue.Object([]jsonvalue.Member{
		{Name: "a/b", Value: mustObject(t, []jsonvalue.Member{
			{Name: "m~n", Value: mustArray(t, []jsonvalue.Value{
				mustString(t, "found"),
			})},
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	pointer, err := reference.ParsePointer("/a~1b/m~0n/0")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := pointer.Evaluate(root)
	if err != nil {
		t.Fatal(err)
	}
	if text, ok := resolved.Text(); !ok || text != "found" {
		t.Fatalf("got %#v", resolved)
	}
	if pointer.String() != "/a~1b/m~0n/0" {
		t.Fatalf("canonical pointer = %q", pointer.String())
	}
}

func TestFragmentDistinguishesRootPointerAndAnchor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw     string
		kind    reference.FragmentKind
		pointer string
		anchor  string
	}{
		{raw: "", kind: reference.FragmentRoot},
		{raw: "/a%20b", kind: reference.FragmentPointer, pointer: "/a b"},
		{raw: "named%2Danchor", kind: reference.FragmentAnchor, anchor: "named-anchor"},
	}
	for _, test := range tests {
		fragment, err := reference.ParseFragment(test.raw)
		if err != nil {
			t.Fatal(err)
		}
		if fragment.Kind() != test.kind {
			t.Fatalf("fragment %q kind = %v", test.raw, fragment.Kind())
		}
		if fragment.Pointer().String() != test.pointer {
			t.Fatalf("fragment %q pointer = %q", test.raw, fragment.Pointer())
		}
		if fragment.Anchor() != test.anchor {
			t.Fatalf("fragment %q anchor = %q", test.raw, fragment.Anchor())
		}
	}
}

func TestPointerRejectsMalformedOrMissingTargets(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"not-a-pointer", "/bad~2escape", "/trailing~"} {
		if _, err := reference.ParsePointer(raw); !errors.Is(err, reference.ErrInvalidPointer) {
			t.Fatalf("ParsePointer(%q) error = %v", raw, err)
		}
	}
	for _, raw := range []string{"%ff", "/bad~2escape"} {
		if _, err := reference.ParseFragment(raw); !errors.Is(err, reference.ErrInvalidFragment) {
			t.Fatalf("invalid fragment %q error = %v", raw, err)
		}
	}

	array := mustArray(t, []jsonvalue.Value{mustString(t, "only")})
	for _, raw := range []string{"/", "/01", "/-", "/x", "/1", "/2", "/999999999999999999999999"} {
		pointer, err := reference.ParsePointer(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pointer.Evaluate(array); !errors.Is(err, reference.ErrTargetNotFound) {
			t.Fatalf("Evaluate(%q) error = %v", raw, err)
		}
	}
	missing, err := reference.ParsePointer("/missing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := missing.Evaluate(mustObject(t, nil)); !errors.Is(err, reference.ErrTargetNotFound) {
		t.Fatalf("missing object member error = %v", err)
	}
	nested, err := reference.ParsePointer("/child")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nested.Evaluate(mustString(t, "scalar")); !errors.Is(err, reference.ErrTargetNotFound) {
		t.Fatalf("scalar traversal error = %v", err)
	}
	root, err := reference.ParsePointer("")
	if err != nil || root.String() != "" {
		t.Fatalf("root pointer = %q, %v", root.String(), err)
	}
}

func mustObject(t *testing.T, members []jsonvalue.Member) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Object(members)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustArray(t *testing.T, elements []jsonvalue.Value) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Array(elements)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustString(t *testing.T, text string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.String(text)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
