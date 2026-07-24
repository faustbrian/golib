package router

import (
	"fmt"
	"net/http"
	"runtime"
	"slices"
	"sort"
	"strings"
)

// Compile validates the complete route set and returns an immutable router.
func (b *Builder) Compile() (*Router, error) {
	if b == nil {
		return nil, &Error{Kind: ErrCompileState, Field: "builder", Detail: "nil builder"}
	}
	if b.optionErr != nil {
		return nil, b.optionErr
	}
	if b.compiled {
		return nil, &Error{Kind: ErrCompileState, Field: "builder", Detail: "already compiled"}
	}
	if err := b.validateGlobalMiddleware(); err != nil {
		return nil, err
	}

	routes := b.PendingRoutes()
	sort.Slice(routes, func(left, right int) bool {
		return routeSortKey(routes[left]) < routeSortKey(routes[right])
	})
	if err := validateNames(routes, b.routeError); err != nil {
		return nil, err
	}
	if err := validateHostPatternConflicts(routes, b.routeError); err != nil {
		return nil, err
	}
	if err := validateServeMuxConflicts(routes); err != nil {
		return nil, err
	}

	compiled := &Router{
		limits:           b.limits,
		supported:        make(map[string]struct{}),
		named:            make(map[string]generationRoute),
		hosts:            make([]compiledHost, 0),
		table:            make([]RouteInfo, 0, len(routes)),
		notFound:         b.notFound,
		methodNotAllowed: b.methodNotAllowed,
		automaticOptions: b.automaticOptions,
		redirectPolicy:   b.redirectPolicy,
		canonicalizer:    http.NewServeMux(),
	}
	hostIndexes := make(map[string]int)
	for _, route := range routes {
		methods := append([]string(nil), route.Methods...)
		slices.Sort(methods)
		route.Methods = methods

		middleware, names, err := b.resolveMiddleware(route)
		if err != nil {
			return nil, err
		}
		info, hostParameters, err := routeInformation(route, names, b.limits)
		if err != nil {
			return nil, err
		}
		compiled.table = append(compiled.table, info)
		infoIndex := len(compiled.table) - 1
		if route.Name != "" {
			compiled.named[route.Name] = generationRoute{host: route.Host, path: route.Path}
		}

		hostKey := hostSignature(route.Host)
		hostIndex, exists := hostIndexes[hostKey]
		if !exists {
			hostIndex = len(compiled.hosts)
			hostIndexes[hostKey] = hostIndex
			compiled.hosts = append(compiled.hosts, compiledHost{
				pattern:   route.Host,
				mux:       http.NewServeMux(),
				methods:   make(map[string]struct{}),
				redirects: make([]*http.ServeMux, 0),
			})
		}
		host := &compiled.hosts[hostIndex]
		if root := redirectRoot(route.Path); root != "" {
			redirects := http.NewServeMux()
			for _, method := range methods {
				redirects.Handle(method+" "+root, http.NotFoundHandler())
			}
			host.redirects = append(host.redirects, redirects)
		}
		handler, err := applyMiddleware(route.Handler, middleware, route.Source)
		if err != nil {
			return nil, err
		}
		handler = compiled.matchingHandler(infoIndex, hostParameters, handler)
		for _, method := range methods {
			host.mux.Handle(method+" "+route.Path, handler)
			host.methods[method] = struct{}{}
			compiled.supported[method] = struct{}{}
			if method == http.MethodGet {
				compiled.supported[http.MethodHead] = struct{}{}
			}
		}
	}
	sort.Slice(compiled.hosts, func(left, right int) bool {
		return hostSpecificity(compiled.hosts[left].pattern) > hostSpecificity(compiled.hosts[right].pattern) ||
			hostSpecificity(compiled.hosts[left].pattern) == hostSpecificity(compiled.hosts[right].pattern) &&
				compiled.hosts[left].pattern < compiled.hosts[right].pattern
	})
	b.compiled = true
	return compiled, nil
}

func validateServeMuxConflicts(routes []Route) error {
	muxes := make(map[string]*http.ServeMux)
	for _, route := range routes {
		signature := hostSignature(route.Host)
		mux := muxes[signature]
		if mux == nil {
			mux = http.NewServeMux()
			muxes[signature] = mux
		}
		methods := append([]string(nil), route.Methods...)
		slices.Sort(methods)
		for _, method := range methods {
			if err := registerPattern(mux, method+" "+route.Path, http.NotFoundHandler(), route.Source); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *Builder) validateGlobalMiddleware() error {
	seen := make(map[string]struct{}, len(b.globalMiddleware))
	for _, middleware := range b.globalMiddleware {
		if middleware.Middleware == nil {
			return b.routeError(ErrInvalidRoute, "middleware", "", "nil router middleware")
		}
		if middleware.Name != "" {
			if !validName(middleware.Name) {
				return b.routeError(ErrInvalidRoute, "middleware", "", "invalid router middleware name")
			}
			if _, exists := seen[middleware.Name]; exists {
				return b.routeError(ErrInvalidRoute, "middleware", "", "duplicate router middleware name")
			}
			seen[middleware.Name] = struct{}{}
		}
	}
	return nil
}

func (b *Builder) resolveMiddleware(route Route) ([]NamedMiddleware, []string, error) {
	excluded := make(map[string]struct{}, len(route.ExcludeMiddleware))
	for _, name := range route.ExcludeMiddleware {
		if name == "" || !validName(name) {
			return nil, nil, b.routeError(ErrInvalidRoute, "middleware", route.Source, "invalid middleware exclusion")
		}
		if _, duplicate := excluded[name]; duplicate {
			return nil, nil, b.routeError(ErrInvalidRoute, "middleware", route.Source, "duplicate middleware exclusion")
		}
		excluded[name] = struct{}{}
	}
	resolved := make([]NamedMiddleware, 0, len(b.globalMiddleware)+len(route.Middleware))
	for _, middleware := range b.globalMiddleware {
		if _, skip := excluded[middleware.Name]; !skip {
			resolved = append(resolved, middleware)
		}
	}
	resolved = append(resolved, route.Middleware...)
	if len(resolved) > b.limits.MaxMiddleware {
		return nil, nil, b.routeError(ErrLimitExceeded, "middleware", route.Source, "resolved middleware depth exceeded")
	}
	seen := make(map[string]struct{}, len(resolved))
	names := make([]string, 0, len(resolved))
	for _, middleware := range resolved {
		if middleware.Name == "" {
			names = append(names, "")
			continue
		}
		if _, duplicate := seen[middleware.Name]; duplicate {
			return nil, nil, b.routeError(ErrInvalidRoute, "middleware", route.Source, "duplicate resolved middleware name")
		}
		seen[middleware.Name] = struct{}{}
		names = append(names, middleware.Name)
	}
	return resolved, names, nil
}

func validateNames(routes []Route, errorFn func(error, string, string, string) error) error {
	names := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		if route.Name == "" {
			continue
		}
		if _, exists := names[route.Name]; exists {
			return errorFn(ErrDuplicateName, "name", route.Source, "route name is already registered")
		}
		names[route.Name] = struct{}{}
	}
	return nil
}

func validateHostPatternConflicts(routes []Route, errorFn func(error, string, string, string) error) error {
	for left := range routes {
		for right := left + 1; right < len(routes); right++ {
			if hostSignature(routes[left].Host) == hostSignature(routes[right].Host) {
				continue
			}
			if hostSpecificity(routes[left].Host) != hostSpecificity(routes[right].Host) ||
				!hostPatternsOverlap(routes[left].Host, routes[right].Host) ||
				!methodSetsOverlap(routes[left].Methods, routes[right].Methods) {
				continue
			}
			return errorFn(ErrConflict, "host", routes[right].Source, "ambiguous overlapping host patterns")
		}
	}
	return nil
}

func hostSignature(host string) string {
	labels := strings.Split(host, ".")
	for index, label := range labels {
		if isWildcardLabel(label) {
			labels[index] = "{}"
		}
	}
	return strings.ToLower(strings.Join(labels, "."))
}

func hostPatternsOverlap(left, right string) bool {
	if left == "" || right == "" {
		return true
	}
	leftLabels := strings.Split(left, ".")
	rightLabels := strings.Split(right, ".")
	if len(leftLabels) != len(rightLabels) {
		return false
	}
	for index := range leftLabels {
		if !isWildcardLabel(leftLabels[index]) && !isWildcardLabel(rightLabels[index]) &&
			!strings.EqualFold(leftLabels[index], rightLabels[index]) {
			return false
		}
	}
	return true
}

func methodSetsOverlap(left, right []string) bool {
	for _, leftMethod := range left {
		for _, rightMethod := range right {
			if leftMethod == rightMethod || leftMethod == http.MethodGet && rightMethod == http.MethodHead ||
				leftMethod == http.MethodHead && rightMethod == http.MethodGet {
				return true
			}
		}
	}
	return false
}

func routeSortKey(route Route) string {
	methods := append([]string(nil), route.Methods...)
	slices.Sort(methods)
	return route.Name + "\x00" + route.Host + "\x00" + route.Path + "\x00" + strings.Join(methods, ",")
}

func registerPattern(mux *http.ServeMux, pattern string, handler http.Handler, source string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			detail, controlled := controlledRegistrationError(recovered)
			if !controlled {
				panic(recovered)
			}
			err = &Error{Kind: ErrConflict, Field: "pattern", Source: source, Detail: bounded(detail, 80)}
		}
	}()
	mux.Handle(pattern, handler)
	return nil
}

func controlledRegistrationError(recovered any) (string, bool) {
	registrationError, ok := recovered.(error)
	if !ok {
		return "", false
	}
	if _, runtimeFailure := recovered.(runtime.Error); runtimeFailure {
		return "", false
	}
	return fmt.Sprint(registrationError), true
}

func applyMiddleware(handler http.Handler, middleware []NamedMiddleware, source string) (http.Handler, error) {
	for index := len(middleware) - 1; index >= 0; index-- {
		handler = middleware[index].Middleware(handler)
		if isNilHandler(handler) {
			return nil, &Error{
				Kind: ErrInvalidRoute, Field: "middleware", Source: source,
				Detail: "middleware returned a nil handler",
			}
		}
	}
	return handler, nil
}

func hostSpecificity(pattern string) int {
	if pattern == "" {
		return -1
	}
	score := 0
	for _, label := range strings.Split(pattern, ".") {
		if !isWildcardLabel(label) {
			score++
		}
	}
	return score
}
