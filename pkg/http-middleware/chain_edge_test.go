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
		{&ConstructionError{Op: "chain", Index: 0, Err: ErrNilMiddleware}, "middleware: chain at index 0: middleware: nil middleware"},
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

func TestExactChainConstructionBoundsAreAccepted(t *testing.T) {
	t.Parallel()

	dependencies := make([]string, 64)
	for index := range dependencies {
		dependencies[index] = "target"
	}
	for name, configuration := range map[string]DescriptorConfig{
		"name": {
			Name: strings.Repeat("a", 128), Middleware: passthroughEdge,
		},
		"before": {
			Name: "source", Middleware: passthroughEdge, Before: dependencies,
		},
		"after": {
			Name: "source", Middleware: passthroughEdge, After: dependencies,
		},
	} {
		if _, err := Describe(configuration); err != nil {
			t.Fatalf("Describe(%s exact bound) error = %v", name, err)
		}
	}

	items := make([]Middleware, MaxChainDepth)
	descriptors := make([]Descriptor, MaxChainDepth)
	for index := range items {
		items[index] = passthroughEdge
		descriptors[index] = Descriptor{middleware: passthroughEdge}
	}
	if _, err := New(items...); err != nil {
		t.Fatalf("New(exact depth) error = %v", err)
	}
	if _, err := Described(descriptors...); err != nil {
		t.Fatalf("Described(exact depth) error = %v", err)
	}
}

func TestConstructionErrorsPreserveExactLocations(t *testing.T) {
	t.Parallel()

	tooManyDependencies := make([]string, 65)
	for index := range tooManyDependencies {
		tooManyDependencies[index] = "target"
	}
	tooManyMiddleware := make([]Middleware, MaxChainDepth+1)
	tooManyDescriptors := make([]Descriptor, MaxChainDepth+1)
	for index := range tooManyMiddleware {
		tooManyMiddleware[index] = passthroughEdge
		tooManyDescriptors[index] = Descriptor{middleware: passthroughEdge}
	}

	_, invalidName := Describe(DescriptorConfig{Name: "", Middleware: passthroughEdge})
	_, nilMiddleware := Describe(DescriptorConfig{Name: "named"})
	_, excessiveOrder := Describe(DescriptorConfig{
		Name: "named", Middleware: passthroughEdge, Before: tooManyDependencies,
	})
	_, invalidOrderName := Describe(DescriptorConfig{
		Name: "named", Middleware: passthroughEdge, Before: []string{"bad name"},
	})
	_, excessiveUnnamed := New(tooManyMiddleware...)
	_, excessiveDescribed := Described(tooManyDescriptors...)
	_, nilHandler := (Chain{}).Handler(nil)
	_, nilPredicate := When(nil, passthroughEdge)
	_, nilConditionalMiddleware := When(func(*http.Request) bool { return true }, nil)

	for name, test := range map[string]struct {
		err   error
		op    string
		index int
		name  string
		cause error
	}{
		"invalid name": {invalidName, "name", -1, "", ErrInvalidName},
		"nil descriptor middleware": {
			nilMiddleware, "descriptor", -1, "named", ErrNilMiddleware,
		},
		"excessive order": {
			excessiveOrder, "descriptor order", -1, "named", ErrChainTooDeep,
		},
		"invalid order name": {
			invalidOrderName, "descriptor order", -1, "named", ErrInvalidName,
		},
		"excessive unnamed chain": {
			excessiveUnnamed, "chain", MaxChainDepth, "", ErrChainTooDeep,
		},
		"excessive described chain": {
			excessiveDescribed, "chain", MaxChainDepth, "", ErrChainTooDeep,
		},
		"nil handler":   {nilHandler, "handler", -1, "", ErrNilHandler},
		"nil predicate": {nilPredicate, "condition", -1, "", ErrNilPredicate},
		"nil conditional middleware": {
			nilConditionalMiddleware, "condition", -1, "", ErrNilMiddleware,
		},
	} {
		var construction *ConstructionError
		if !errors.As(test.err, &construction) || !errors.Is(test.err, test.cause) {
			t.Fatalf("%s error = %v", name, test.err)
		}
		if construction.Op != test.op || construction.Index != test.index ||
			construction.Name != test.name {
			t.Fatalf("%s location = %#v", name, construction)
		}
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

	unnamed := Descriptor{middleware: passthroughEdge}
	invalid := Descriptor{name: "bad name", middleware: passthroughEdge}
	if _, err := Described(unnamed, invalid); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("descriptor after unnamed error = %v", err)
	}
	selfBefore := Descriptor{
		name: "self-before", middleware: passthroughEdge, before: []string{"self-before"},
	}
	if _, err := Described(selfBefore); !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("self before error = %v", err)
	}
	selfAfter := Descriptor{
		name: "self-after", middleware: passthroughEdge, after: []string{"self-after"},
	}
	if _, err := Described(selfAfter); !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("self after error = %v", err)
	}
}

func passthroughEdge(next http.Handler) http.Handler { return next }
