package router

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
)

// Option configures a Builder.
type Option func(*Builder)

// WithLimits replaces all construction and generation limits.
func WithLimits(limits Limits) Option {
	return func(builder *Builder) {
		if !limits.valid() {
			builder.optionErr = &Error{Kind: ErrLimitExceeded, Field: "limits", Detail: "all limits must be positive"}
			return
		}
		builder.limits = limits
	}
}

// Builder owns mutable startup-time registration state.
type Builder struct {
	limits           Limits
	routes           []Route
	globalMiddleware []NamedMiddleware
	notFound         http.Handler
	methodNotAllowed http.Handler
	automaticOptions bool
	redirectPolicy   RedirectPolicy
	group            groupState
	groupDepth       int
	groups           int
	optionErr        error
	compiled         bool
}

// WithMiddleware sets router-wide middleware in request execution order.
func WithMiddleware(middleware ...NamedMiddleware) Option {
	return func(builder *Builder) {
		builder.globalMiddleware = middleware
	}
}

// New creates an empty single-owner route builder.
func New(options ...Option) *Builder {
	builder := &Builder{limits: DefaultLimits(), automaticOptions: true, redirectPolicy: FollowRedirects}
	for _, option := range options {
		if option == nil {
			builder.optionErr = &Error{Kind: ErrInvalidRoute, Field: "option", Detail: "nil option"}
			continue
		}
		option(builder)
	}
	if builder.optionErr == nil {
		if len(builder.globalMiddleware) > builder.limits.MaxMiddleware {
			builder.optionErr = &Error{Kind: ErrLimitExceeded, Field: "middleware", Detail: "router middleware depth exceeded"}
		} else {
			for _, middleware := range builder.globalMiddleware {
				if len(middleware.Name) > builder.limits.MaxNameBytes {
					builder.optionErr = &Error{Kind: ErrLimitExceeded, Field: "middleware", Detail: "router middleware name is too long"}
					break
				}
			}
			if builder.optionErr == nil {
				builder.globalMiddleware = append([]NamedMiddleware(nil), builder.globalMiddleware...)
			}
		}
	}
	return builder
}

// WithNotFound replaces the minimal default 404 handler.
func WithNotFound(handler http.Handler) Option {
	return func(builder *Builder) {
		if isNilHandler(handler) {
			builder.optionErr = &Error{Kind: ErrInvalidRoute, Field: "not-found", Detail: "nil error handler"}
			return
		}
		builder.notFound = handler
	}
}

// WithMethodNotAllowed replaces the minimal default 405 handler. The router
// sets Allow before invoking it.
func WithMethodNotAllowed(handler http.Handler) Option {
	return func(builder *Builder) {
		if isNilHandler(handler) {
			builder.optionErr = &Error{Kind: ErrInvalidRoute, Field: "method-not-allowed", Detail: "nil error handler"}
			return
		}
		builder.methodNotAllowed = handler
	}
}

// WithAutomaticOPTIONS controls package-generated OPTIONS responses.
func WithAutomaticOPTIONS(enabled bool) Option {
	return func(builder *Builder) { builder.automaticOptions = enabled }
}

// RedirectPolicy controls ServeMux canonical-path and subtree redirects.
type RedirectPolicy uint8

const (
	// FollowRedirects preserves the standard ServeMux redirect behavior.
	FollowRedirects RedirectPolicy = iota
	// RejectRedirects treats a match requiring canonicalization as not found.
	RejectRedirects
)

// WithRedirectPolicy selects explicit canonical-path redirect behavior.
func WithRedirectPolicy(policy RedirectPolicy) Option {
	return func(builder *Builder) {
		if policy != FollowRedirects && policy != RejectRedirects {
			builder.optionErr = &Error{Kind: ErrInvalidRoute, Field: "redirect", Detail: "invalid redirect policy"}
			return
		}
		builder.redirectPolicy = policy
	}
}

// Register validates and copies one route descriptor.
func (b *Builder) Register(route Route) error {
	if b == nil {
		return &Error{Kind: ErrCompileState, Field: "builder", Detail: "nil builder"}
	}
	if b.optionErr != nil {
		return b.optionErr
	}
	if b.compiled {
		return &Error{Kind: ErrCompileState, Field: "builder", Detail: "already compiled"}
	}
	if len(b.routes) >= b.limits.MaxRoutes {
		return b.routeError(ErrLimitExceeded, "routes", route.Source, "route count exceeded")
	}
	if len(route.Methods) > b.limits.MaxMethodsPerRoute {
		return b.routeError(ErrLimitExceeded, "methods", route.Source, "method count exceeded")
	}
	if len(route.Middleware) > b.limits.MaxMiddleware || len(route.ExcludeMiddleware) > b.limits.MaxMiddleware {
		return b.routeError(ErrLimitExceeded, "middleware", route.Source, "middleware list exceeded")
	}
	if len(route.Metadata) > b.limits.MaxMetadataEntries {
		return b.routeError(ErrLimitExceeded, "metadata", route.Source, "metadata entry count exceeded")
	}
	for _, name := range route.ExcludeMiddleware {
		if len(name) > b.limits.MaxNameBytes {
			return b.routeError(ErrLimitExceeded, "middleware", route.Source, "middleware exclusion name is too long")
		}
	}
	var err error
	route, err = b.flattenRoute(route)
	if err != nil {
		return err
	}
	if err := b.validateRoute(route); err != nil {
		return err
	}
	b.routes = append(b.routes, cloneRoute(route))
	return nil
}

// PendingRoutes returns copied descriptors registered before compilation.
func (b *Builder) PendingRoutes() []Route {
	if b == nil {
		return nil
	}
	routes := make([]Route, len(b.routes))
	for index, route := range b.routes {
		routes[index] = cloneRoute(route)
	}
	return routes
}

func (b *Builder) validateRoute(route Route) error {
	if isNilHandler(route.Handler) {
		return b.routeError(ErrInvalidRoute, "handler", route.Source, "handler is nil")
	}
	if len(route.Methods) == 0 || len(route.Methods) > b.limits.MaxMethodsPerRoute {
		return b.routeError(ErrInvalidRoute, "methods", route.Source, "invalid method count")
	}
	seenMethods := make(map[string]struct{}, len(route.Methods))
	for _, method := range route.Methods {
		if len(method) > b.limits.MaxMethodBytes {
			return b.routeError(ErrLimitExceeded, "methods", route.Source, "method token is too long")
		}
		if method == "" || method != strings.ToUpper(method) || !isToken(method) {
			return b.routeError(ErrInvalidRoute, "methods", route.Source, "method must be an uppercase token")
		}
		if method == http.MethodConnect {
			return b.routeError(ErrUnsupported, "methods", route.Source, "CONNECT routing is not supported")
		}
		if _, exists := seenMethods[method]; exists {
			return b.routeError(ErrInvalidRoute, "methods", route.Source, "duplicate method")
		}
		seenMethods[method] = struct{}{}
	}
	if len(route.Name) > b.limits.MaxNameBytes {
		return b.routeError(ErrLimitExceeded, "name", route.Source, "route name is too long")
	}
	if route.Name != "" && !validName(route.Name) {
		return b.routeError(ErrInvalidRoute, "name", route.Source, "invalid route name")
	}
	if err := b.validateHost(route.Host, route.Source); err != nil {
		return err
	}
	if err := b.validatePath(route.Path, route.Source); err != nil {
		return err
	}
	if routeWildcardCount(route.Host, route.Path) > b.limits.MaxWildcardsPerRoute {
		return b.routeError(ErrLimitExceeded, "path", route.Source, "wildcard count exceeded")
	}
	if len(route.Source) > b.limits.MaxSourceBytes {
		return b.routeError(ErrLimitExceeded, "source", route.Source, "source label is too long")
	}
	if len(route.Operation) > b.limits.MaxOperationBytes {
		return b.routeError(ErrLimitExceeded, "operation", route.Source, "operation identifier is too long")
	}
	if len(route.Documentation) > b.limits.MaxDocumentationBytes {
		return b.routeError(ErrLimitExceeded, "documentation", route.Source, "documentation is too long")
	}
	if len(route.Middleware) > b.limits.MaxMiddleware {
		return b.routeError(ErrLimitExceeded, "middleware", route.Source, "middleware depth exceeded")
	}
	seenMiddleware := make(map[string]struct{}, len(route.Middleware))
	for _, middleware := range route.Middleware {
		if len(middleware.Name) > b.limits.MaxNameBytes {
			return b.routeError(ErrLimitExceeded, "middleware", route.Source, "middleware name is too long")
		}
		if middleware.Middleware == nil {
			return b.routeError(ErrInvalidRoute, "middleware", route.Source, "nil middleware")
		}
		if middleware.Name != "" {
			if !validName(middleware.Name) {
				return b.routeError(ErrInvalidRoute, "middleware", route.Source, "invalid middleware name")
			}
			if _, exists := seenMiddleware[middleware.Name]; exists {
				return b.routeError(ErrInvalidRoute, "middleware", route.Source, "duplicate middleware name")
			}
			seenMiddleware[middleware.Name] = struct{}{}
		}
	}
	return validateServeMuxPattern(route.Methods[0], route.Path, b.routeError, route.Source)
}

func (b *Builder) validateHost(host, source string) error {
	if host == "" {
		return nil
	}
	if len(host) > b.limits.MaxHostBytes {
		return b.routeError(ErrLimitExceeded, "host", source, "host pattern is too long")
	}
	if !ascii(host) || strings.ContainsAny(host, "/?#@ :[]\\\t\r\n") {
		return b.routeError(ErrInvalidRoute, "host", source, "invalid host pattern")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" {
			return b.routeError(ErrInvalidRoute, "host", source, "empty host label")
		}
		if strings.HasPrefix(label, "{") && strings.HasSuffix(label, "}") {
			name := label[1 : len(label)-1]
			if len(name) > b.limits.MaxWildcardNameBytes {
				return b.routeError(ErrLimitExceeded, "host", source, "host wildcard name is too long")
			}
			if !validName(name) {
				return b.routeError(ErrInvalidRoute, "host", source, "invalid host wildcard")
			}
			continue
		}
		if strings.ContainsAny(label, "{}") {
			return b.routeError(ErrInvalidRoute, "host", source, "invalid host wildcard")
		}
		if !safeHostLabel(label) {
			return b.routeError(ErrInvalidRoute, "host", source, "invalid host label")
		}
	}
	return nil
}

func (b *Builder) validatePath(path, source string) error {
	if len(path) > b.limits.MaxPatternBytes {
		return b.routeError(ErrLimitExceeded, "path", source, "path pattern is too long")
	}
	if path == "" || path[0] != '/' {
		return b.routeError(ErrInvalidRoute, "path", source, "path must be an absolute bounded pattern")
	}
	for _, segment := range strings.Split(path, "/") {
		decoded, err := url.PathUnescape(segment)
		if err != nil {
			return b.routeError(ErrInvalidRoute, "path", source, "invalid path escape")
		}
		if decoded == "." || decoded == ".." {
			return b.routeError(ErrInvalidRoute, "path", source, "dot segments are not supported")
		}
		if segment != "{$}" {
			if name, _, wildcard := pathWildcard(segment); wildcard && len(name) > b.limits.MaxWildcardNameBytes {
				return b.routeError(ErrLimitExceeded, "path", source, "path wildcard name is too long")
			}
		}
	}
	return nil
}

func routeWildcardCount(host, path string) int {
	count := strings.Count(path, "{")
	for _, label := range strings.Split(host, ".") {
		if isWildcardLabel(label) {
			count++
		}
	}
	return count
}

func (b *Builder) routeError(kind error, field, source, detail string) error {
	return &Error{Kind: kind, Field: field, Source: bounded(source, b.limits.MaxSourceBytes), Detail: detail}
}

func validateServeMuxPattern(method, path string, errorFn func(error, string, string, string) error, source string) (err error) {
	return validateServeMuxRegistration(func() {
		http.NewServeMux().Handle(method+" "+path, http.NotFoundHandler())
	}, errorFn, source)
}

func validateServeMuxRegistration(register func(), errorFn func(error, string, string, string) error, source string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			detail, controlled := controlledRegistrationError(recovered)
			if !controlled {
				panic(recovered)
			}
			err = errorFn(ErrInvalidRoute, "path", source, detail)
		}
	}()
	register()
	return nil
}

func isNilHandler(handler http.Handler) bool {
	if handler == nil {
		return true
	}
	value := reflect.ValueOf(handler)
	kind := value.Kind()
	if kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface ||
		kind == reflect.Map || kind == reflect.Pointer || kind == reflect.Slice {
		return value.IsNil()
	}
	return false
}

func validName(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || index > 0 && strings.ContainsRune("._:-", character) {
			continue
		}
		return false
	}
	return true
}

func isToken(value string) bool {
	const punctuation = "!#$%&'*+-.^_`|~"
	for _, character := range value {
		if character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' || strings.ContainsRune(punctuation, character) {
			continue
		}
		return false
	}
	return value != ""
}
