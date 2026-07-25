package router_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"

	router "github.com/faustbrian/golib/pkg/router"
)

func ExampleBuilder() {
	builder := router.New()
	_ = builder.Register(router.Route{
		Name: "users.show", Methods: []string{http.MethodGet},
		Path: "/users/{id}",
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			fmt.Fprint(writer, request.PathValue("id"))
		}),
	})
	compiled, _ := builder.Compile()
	response := httptest.NewRecorder()
	compiled.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	fmt.Println(response.Body.String())
	// Output: 42
}

func ExampleBuilder_Group() {
	builder := router.New()
	_ = builder.Group(router.GroupOptions{PathPrefix: "/api", NamePrefix: "api."}, func(group *router.Builder) error {
		return group.Register(router.Route{
			Name: "health", Methods: []string{http.MethodGet}, Path: "/health",
			Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusNoContent)
			}),
		})
	})
	compiled, _ := builder.Compile()
	fmt.Println(compiled.Routes()[0].Name, compiled.Routes()[0].Pattern)
	// Output: api.health /api/health
}

func ExampleBuilder_Mount() {
	builder := router.New()
	rpc := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fmt.Fprint(writer, request.URL.Path)
	})
	_ = builder.Mount("/rpc", rpc, router.MountOptions{StripPrefix: true})
	compiled, _ := builder.Compile()
	response := httptest.NewRecorder()
	compiled.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/rpc/method", nil))
	fmt.Println(response.Body.String())
	// Output: /method
}

func ExampleRouter_Path() {
	builder := router.New()
	_ = builder.Register(router.Route{
		Name: "files.show", Methods: []string{http.MethodGet}, Path: "/files/{name}",
		Handler: http.NotFoundHandler(),
	})
	compiled, _ := builder.Compile()
	path, _ := compiled.Path("files.show", router.Param("name", "a/b.txt"))
	fmt.Println(path)
	// Output: /files/a%2Fb.txt
}

func ExampleRouter_URL() {
	builder := router.New()
	_ = builder.Register(router.Route{
		Name: "tenant.user", Methods: []string{http.MethodGet},
		Host: "{tenant}.example.com", Path: "/users/{id}", Handler: http.NotFoundHandler(),
	})
	compiled, _ := builder.Compile()
	base, _ := router.NewBaseURL("https", "example.com")
	generated, _ := compiled.URL(
		"tenant.user", base, url.Values{"tab": {"profile"}},
		router.Param("tenant", "acme"), router.Param("id", "42"),
	)
	fmt.Println(generated)
	// Output: https://acme.example.com/users/42?tab=profile
}
