package router

import (
	"net/http"
	"net/url"
	"strings"
)

// MountOptions configures an explicit standard-handler mount.
type MountOptions struct {
	Name          string
	Methods       []string
	Host          string
	Middleware    []NamedMiddleware
	Metadata      map[string]string
	Documentation string
	Operation     string
	Source        string
	StripPrefix   bool
}

// Mount registers handler below an explicit path boundary. The mount is one
// ordinary remainder-wildcard route and therefore follows the active redirect
// policy for a request missing the boundary's trailing slash.
func (b *Builder) Mount(prefix string, handler http.Handler, options MountOptions) error {
	if b == nil {
		return &Error{Kind: ErrCompileState, Field: "builder", Detail: "nil builder"}
	}
	if len(prefix) > b.limits.MaxPatternBytes {
		return b.routeError(ErrLimitExceeded, "path", options.Source, "mount prefix is too long")
	}
	if prefix == "" || prefix[0] != '/' || strings.Contains(prefix, "{") {
		return b.routeError(ErrInvalidRoute, "path", options.Source, "invalid mount prefix")
	}
	if err := b.validatePrefix(prefix); err != nil {
		return err
	}
	boundary := strings.TrimSuffix(prefix, "/")
	if boundary == "" {
		boundary = "/"
	}
	mounted := handler
	if options.StripPrefix && !isNilHandler(handler) {
		decodedBoundary, err := url.PathUnescape(boundary)
		if err != nil {
			return b.routeError(ErrInvalidRoute, "path", options.Source, "invalid mount prefix escape")
		}
		mounted = stripMountPrefix(boundary, decodedBoundary, handler)
	}
	methods := options.Methods
	if len(methods) == 0 {
		methods = []string{
			http.MethodDelete, http.MethodGet, http.MethodHead,
			http.MethodOptions, http.MethodPatch, http.MethodPost,
			http.MethodPut, http.MethodTrace,
		}
	}
	pattern := boundary + "/{mount...}"
	if boundary == "/" {
		pattern = "/{mount...}"
	}
	return b.Register(Route{
		Name:          options.Name,
		Methods:       methods,
		Host:          options.Host,
		Path:          pattern,
		Handler:       mounted,
		Middleware:    options.Middleware,
		Metadata:      options.Metadata,
		Documentation: options.Documentation,
		Operation:     options.Operation,
		Source:        options.Source,
	})
}

func stripMountPrefix(boundary, decodedBoundary string, handler http.Handler) http.Handler {
	segments := 0
	if boundary != "/" {
		segments = strings.Count(strings.Trim(boundary, "/"), "/") + 1
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path := strings.TrimPrefix(request.URL.Path, decodedBoundary)
		if len(path) == len(request.URL.Path) ||
			decodedBoundary != "/" && path != "" && path[0] != '/' {
			http.NotFound(writer, request)
			return
		}
		rawPath := ""
		if request.URL.RawPath != "" {
			parts := strings.Split(strings.TrimPrefix(request.URL.EscapedPath(), "/"), "/")
			if len(parts) < segments {
				http.NotFound(writer, request)
				return
			}
			parts = parts[segments:]
			rawPath = strings.Join(parts, "/")
			if segments > 0 && len(parts) > 0 {
				rawPath = "/" + rawPath
			}
		}
		clone := request.Clone(request.Context())
		clonedURL := *request.URL
		clonedURL.Path = path
		clonedURL.RawPath = rawPath
		clone.URL = &clonedURL
		handler.ServeHTTP(writer, clone)
	})
}
