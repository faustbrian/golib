package routertest_test

import (
	"net/http"
	"testing"

	router "github.com/faustbrian/golib/pkg/router"
	"github.com/faustbrian/golib/pkg/router/routertest"
)

func TestHelpersCompileServeAndAssert(t *testing.T) {
	t.Parallel()

	builder := router.New()
	if err := builder.Register(router.Route{
		Methods: []string{"GET"}, Path: "/health",
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		}),
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	compiled := routertest.MustCompile(t, builder)
	response := routertest.Serve(t, compiled, http.MethodGet, "/health")
	routertest.AssertStatus(t, response, http.StatusNoContent)
	table := routertest.RouteTable(t, compiled)
	if len(table) != 1 || table[0].Pattern != "/health" {
		t.Fatalf("route table: %#v", table)
	}
}
