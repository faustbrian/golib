package middleware

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestConstructionErrorMessagesAndValidationEdges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		error *ConstructionError
		want  string
	}{
		{&ConstructionError{Op: "name", Index: -1, Name: "bad", Err: ErrInvalidName}, `middleware: name "bad": middleware: invalid name`},
		{&ConstructionError{Op: "chain", Index: 2, Err: ErrNilMiddleware}, "middleware: chain at index 2: middleware: nil middleware"},
		{&ConstructionError{Op: "handler", Index: -1, Err: ErrNilHandler}, "middleware: handler: middleware: nil handler"},
	} {
		if got := tc.error.Error(); got != tc.want || !errors.Is(tc.error, tc.error.Err) {
			t.Fatalf("error = %q, want %q", got, tc.want)
		}
	}

	tooMany := make([]string, 65)
	for index := range tooMany {
		tooMany[index] = "item"
	}
	for _, configuration := range []DescriptorConfig{
		{Name: "", Middleware: passthroughEdge},
		{Name: strings.Repeat("a", 129), Middleware: passthroughEdge},
		{Name: "bad/name", Middleware: passthroughEdge},
		{Name: "good", Middleware: nil},
		{Name: "good", Middleware: passthroughEdge, Before: tooMany},
		{Name: "good", Middleware: passthroughEdge, After: []string{"good"}},
		{Name: "good", Middleware: passthroughEdge, Before: []string{"bad name"}},
	} {
		if _, err := Describe(configuration); err == nil {
			t.Fatalf("Describe(%+v) succeeded", configuration)
		}
	}

	descriptors := make([]Descriptor, MaxChainDepth+1)
	for index := range descriptors {
		descriptors[index] = Descriptor{middleware: passthroughEdge}
	}
	if _, err := Described(descriptors...); !errors.Is(err, ErrChainTooDeep) {
		t.Fatalf("Described() error = %v", err)
	}
	if _, err := Described(Descriptor{name: "bad name", middleware: passthroughEdge}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("invalid internal descriptor error = %v", err)
	}
}

func TestDescriptorOrderAndConditionalEdges(t *testing.T) {
	t.Parallel()
	first, _ := Describe(DescriptorConfig{Name: "first", Middleware: passthroughEdge})
	second, _ := Describe(DescriptorConfig{Name: "second", Middleware: passthroughEdge, After: []string{"first"}})
	if _, err := Described(first, second); err != nil {
		t.Fatal(err)
	}
	if _, err := Described(second, first); !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("order error = %v", err)
	}

	allow, _ := Describe(DescriptorConfig{Name: "dup", Middleware: passthroughEdge, AllowDuplicate: true})
	deny, _ := Named("dup", passthroughEdge)
	if _, err := Described(allow, deny); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate error = %v", err)
	}

	if _, err := When(nil, passthroughEdge); !errors.Is(err, ErrNilPredicate) {
		t.Fatalf("predicate error = %v", err)
	}
	if _, err := When(func(*http.Request) bool { return true }, nil); !errors.Is(err, ErrNilMiddleware) {
		t.Fatalf("middleware error = %v", err)
	}
	conditional, err := When(func(*http.Request) bool { return true }, func(http.Handler) http.Handler { return nil })
	if err != nil {
		t.Fatal(err)
	}
	chain, err := New(conditional)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chain.Handler(http.NotFoundHandler()); !errors.Is(err, ErrNilHandler) {
		t.Fatalf("conditional nil result error = %v", err)
	}

	duplicate, _ := Describe(DescriptorConfig{Name: "duplicate", Middleware: passthroughEdge, AllowDuplicate: true})
	after, _ := Describe(DescriptorConfig{Name: "after", Middleware: passthroughEdge, After: []string{"duplicate"}})
	if _, err := Described(duplicate, after, duplicate); !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("duplicate target order error = %v", err)
	}
}

func passthroughEdge(next http.Handler) http.Handler { return next }
