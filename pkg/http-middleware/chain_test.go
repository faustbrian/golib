package middleware_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	middleware "github.com/faustbrian/golib/pkg/http-middleware"
)

func TestChainExecutesInDeclaredOrderAndUnwindsInReverse(t *testing.T) {
	t.Parallel()

	var calls []string
	wrap := func(name string) middleware.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, name+":request")
				next.ServeHTTP(w, r)
				calls = append(calls, name+":response")
			})
		}
	}

	chain, err := middleware.New(wrap("first"), wrap("second"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	handler, err := chain.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls = append(calls, "handler")
	}))
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	want := []string{"first:request", "second:request", "handler", "second:response", "first:response"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestChainRejectsNilValuesWithTypedErrors(t *testing.T) {
	t.Parallel()

	_, err := middleware.New(nil)
	var construction *middleware.ConstructionError
	if !errors.As(err, &construction) || !errors.Is(err, middleware.ErrNilMiddleware) {
		t.Fatalf("New(nil) error = %v", err)
	}

	chain, err := middleware.New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = chain.Handler(nil)
	if !errors.As(err, &construction) || !errors.Is(err, middleware.ErrNilHandler) {
		t.Fatalf("Handler(nil) error = %v", err)
	}
}

func TestHandlerRejectsInvalidZeroDescriptorsAndNilResults(t *testing.T) {
	t.Parallel()

	zero := middleware.Chain{}.Append(middleware.Descriptor{})
	if _, err := zero.Handler(http.NotFoundHandler()); !errors.Is(err, middleware.ErrNilMiddleware) {
		t.Fatalf("zero descriptor error = %v", err)
	}
	chain, err := middleware.New(func(http.Handler) http.Handler { return nil })
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := chain.Handler(http.NotFoundHandler()); !errors.Is(err, middleware.ErrNilHandler) {
		t.Fatalf("nil result error = %v", err)
	}
}

func TestChainDepthIsBounded(t *testing.T) {
	t.Parallel()

	items := make([]middleware.Middleware, middleware.MaxChainDepth+1)
	for index := range items {
		items[index] = passthrough
	}
	if _, err := middleware.New(items...); !errors.Is(err, middleware.ErrChainTooDeep) {
		t.Fatalf("New() error = %v", err)
	}
}

func TestNamedDescriptorsAreImmutableAndInspectable(t *testing.T) {
	t.Parallel()

	descriptor, err := middleware.Named("security", passthrough)
	if err != nil {
		t.Fatalf("Named() error = %v", err)
	}
	chain, err := middleware.Described(descriptor)
	if err != nil {
		t.Fatalf("Described() error = %v", err)
	}

	got := chain.Descriptors()
	if len(got) != 1 || got[0].Name() != "security" {
		t.Fatalf("Descriptors() = %#v", got)
	}
	got[0] = middleware.Descriptor{}
	if chain.Descriptors()[0].Name() != "security" {
		t.Fatal("Descriptors returned mutable chain storage")
	}
}

func TestDescribedChainRejectsDuplicateNamesByDefault(t *testing.T) {
	t.Parallel()

	descriptor, err := middleware.Named("request-id", passthrough)
	if err != nil {
		t.Fatalf("Named() error = %v", err)
	}
	_, err = middleware.Described(descriptor, descriptor)
	if !errors.Is(err, middleware.ErrDuplicateName) {
		t.Fatalf("Described() error = %v", err)
	}
}

func TestExplicitDuplicateAndOrderingPoliciesAreInspectable(t *testing.T) {
	t.Parallel()

	requestID, err := middleware.Describe(middleware.DescriptorConfig{Name: "request-id", Middleware: passthrough, AllowDuplicate: true, Before: []string{"authentication"}})
	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	authentication, _ := middleware.Named("authentication", passthrough)
	chain, err := middleware.Described(requestID, requestID, authentication)
	if err != nil {
		t.Fatalf("Described() error = %v", err)
	}
	info := chain.Descriptors()[0].Info()
	if !info.AllowDuplicate || !reflect.DeepEqual(info.Before, []string{"authentication"}) {
		t.Fatalf("Info() = %#v", info)
	}
	info.Before[0] = "mutated"
	if chain.Descriptors()[0].Info().Before[0] != "authentication" {
		t.Fatal("descriptor metadata is mutable")
	}

	if _, err := middleware.Described(authentication, requestID); !errors.Is(err, middleware.ErrInvalidOrder) {
		t.Fatalf("reversed order error = %v", err)
	}
}

func TestChainCompositionDoesNotMutateInputs(t *testing.T) {
	t.Parallel()

	a, _ := middleware.Named("a", passthrough)
	b, _ := middleware.Named("b", passthrough)
	c, _ := middleware.Named("c", passthrough)
	left, _ := middleware.Described(b)
	right, _ := middleware.Described(c)

	combined, err := left.Prepend(a).Concat(right)
	if err != nil {
		t.Fatalf("composition error = %v", err)
	}
	if got := names(combined.Descriptors()); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("combined names = %v", got)
	}
	if got := names(left.Descriptors()); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("left names = %v", got)
	}
}

func TestConditionalApplicationUsesPredicatePerRequest(t *testing.T) {
	t.Parallel()

	conditional, err := middleware.When(
		func(r *http.Request) bool { return r.Header.Get("X-Apply") == "yes" },
		func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Applied", "yes")
				next.ServeHTTP(w, r)
			})
		},
	)
	if err != nil {
		t.Fatalf("When() error = %v", err)
	}
	chain, _ := middleware.New(conditional)
	handler, _ := chain.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tc := range []struct {
		name  string
		apply bool
	}{
		{name: "applied", apply: true},
		{name: "skipped", apply: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.apply {
				req.Header.Set("X-Apply", "yes")
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if got := recorder.Header().Get("X-Applied"); (got == "yes") != tc.apply {
				t.Fatalf("X-Applied = %q", got)
			}
		})
	}
}

func passthrough(next http.Handler) http.Handler { return next }

func names(descriptors []middleware.Descriptor) []string {
	result := make([]string, len(descriptors))
	for index, descriptor := range descriptors {
		result[index] = descriptor.Name()
	}
	return result
}
