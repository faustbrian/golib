package middleware_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	middleware "github.com/faustbrian/golib/pkg/http-middleware"
)

var recommendedLayers = []string{
	"recovery", "proxy", "request-id", "observe", "cors", "secure-header",
	"admission", "body-limit", "deadline", "authentication", "rate-limit",
	"authorization", "idempotency", "compression",
}

func TestRecommendedStackHasExactRequestAndResponseOrder(t *testing.T) {
	t.Parallel()
	var events []string
	items := make([]middleware.Middleware, len(recommendedLayers))
	for index, name := range recommendedLayers {
		items[index] = auditLayer(name, &events, nil)
	}
	chain, err := middleware.New(items...)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := chain.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		events = append(events, "request:application", "response:application")
	}))
	if err != nil {
		t.Fatal(err)
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	want := make([]string, 0, 2*len(recommendedLayers)+2)
	for _, name := range recommendedLayers {
		want = append(want, "request:"+name)
	}
	want = append(want, "request:application", "response:application")
	for index := len(recommendedLayers) - 1; index >= 0; index-- {
		want = append(want, "response:"+recommendedLayers[index])
	}
	if !slices.Equal(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestEveryRecommendedLayerShortCircuitUnwindsOnlyEnteredLayers(t *testing.T) {
	t.Parallel()
	for stop, stoppedName := range recommendedLayers {
		stop, stoppedName := stop, stoppedName
		t.Run(stoppedName, func(t *testing.T) {
			t.Parallel()
			var events []string
			items := make([]middleware.Middleware, len(recommendedLayers))
			for index, name := range recommendedLayers {
				var action func(http.ResponseWriter, *http.Request) bool
				if index == stop {
					action = func(w http.ResponseWriter, _ *http.Request) bool {
						w.WriteHeader(http.StatusTeapot)
						return true
					}
				}
				items[index] = auditLayer(name, &events, action)
			}
			chain, _ := middleware.New(items...)
			handler, _ := chain.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("application ran after short circuit")
			}))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			if recorder.Code != http.StatusTeapot {
				t.Fatalf("status = %d", recorder.Code)
			}
			want := make([]string, 0, 2*(stop+1))
			for index := 0; index <= stop; index++ {
				want = append(want, "request:"+recommendedLayers[index])
			}
			for index := stop; index >= 0; index-- {
				want = append(want, "response:"+recommendedLayers[index])
			}
			if !slices.Equal(events, want) {
				t.Fatalf("events = %v, want %v", events, want)
			}
		})
	}
}

func TestEveryRecommendedLayerPanicUnwindsOnlyEnteredLayers(t *testing.T) {
	t.Parallel()
	positions := append(append([]string(nil), recommendedLayers...), "application")
	for stop, stoppedName := range positions {
		stop, stoppedName := stop, stoppedName
		t.Run(stoppedName, func(t *testing.T) {
			t.Parallel()
			var events []string
			items := make([]middleware.Middleware, len(recommendedLayers))
			for index, name := range recommendedLayers {
				var action func(http.ResponseWriter, *http.Request) bool
				if index == stop {
					action = func(http.ResponseWriter, *http.Request) bool {
						panic(stoppedName)
					}
				}
				items[index] = auditLayer(name, &events, action)
			}
			chain, _ := middleware.New(items...)
			handler, _ := chain.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				events = append(events, "request:application")
				panic(stoppedName)
			}))
			func() {
				defer func() {
					if recovered := recover(); recovered != stoppedName {
						t.Fatalf("panic = %v, want %q", recovered, stoppedName)
					}
				}()
				handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
			}()

			entered := min(stop, len(recommendedLayers)-1)
			want := make([]string, 0, 2*(entered+1)+1)
			for index := 0; index <= entered; index++ {
				want = append(want, "request:"+recommendedLayers[index])
			}
			if stop == len(recommendedLayers) {
				want = append(want, "request:application")
			}
			for index := entered; index >= 0; index-- {
				want = append(want, "response:"+recommendedLayers[index])
			}
			if !slices.Equal(events, want) {
				t.Fatalf("events = %v, want %v", events, want)
			}
		})
	}
}

func TestChainReentryAndCancellationHaveNoHiddenState(t *testing.T) {
	t.Parallel()
	type depthKey struct{}
	var events []string
	var resolved http.Handler
	reenter := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			depth, _ := r.Context().Value(depthKey{}).(int)
			events = append(events, fmt.Sprintf("enter:%d", depth))
			defer func() { events = append(events, fmt.Sprintf("exit:%d", depth)) }()
			if depth == 0 {
				ctx := context.WithValue(r.Context(), depthKey{}, 1)
				resolved.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	conditional, err := middleware.When(func(r *http.Request) bool {
		return r.Context().Err() == nil
	}, reenter)
	if err != nil {
		t.Fatal(err)
	}
	chain, _ := middleware.New(conditional)
	resolved, err = chain.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		events = append(events, "application")
	}))
	if err != nil {
		t.Fatal(err)
	}
	resolved.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolved.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
	want := []string{"enter:0", "enter:1", "application", "exit:1", "exit:0", "application"}
	if !slices.Equal(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func auditLayer(name string, events *[]string, action func(http.ResponseWriter, *http.Request) bool) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*events = append(*events, "request:"+name)
			defer func() { *events = append(*events, "response:"+name) }()
			if action != nil && action(w, r) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
