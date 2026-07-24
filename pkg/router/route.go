package router

import "net/http"

// Middleware is the standard HTTP middleware shape.
type Middleware = func(http.Handler) http.Handler

// NamedMiddleware makes a middleware layer visible through introspection.
// Name may be empty when exclusion and duplicate detection are not needed.
type NamedMiddleware struct {
	Name       string
	Middleware Middleware
}

// Route is an explicit route descriptor. Builder.Register copies every slice
// and map before retaining it.
type Route struct {
	Name              string
	Methods           []string
	Host              string
	Path              string
	Handler           http.Handler
	Middleware        []NamedMiddleware
	ExcludeMiddleware []string
	Metadata          map[string]string
	Documentation     string
	Operation         string
	Source            string
}

func cloneRoute(route Route) Route {
	cloned := route
	cloned.Methods = append([]string(nil), route.Methods...)
	cloned.Middleware = append([]NamedMiddleware(nil), route.Middleware...)
	cloned.ExcludeMiddleware = append([]string(nil), route.ExcludeMiddleware...)
	if route.Metadata != nil {
		cloned.Metadata = make(map[string]string, len(route.Metadata))
		for key, value := range route.Metadata {
			cloned.Metadata[key] = value
		}
	}
	return cloned
}
