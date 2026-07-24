package router

import (
	"context"
	"net"
	"net/http"
	"net/url"
	pathpkg "path"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type routeContextKey struct{}
type pathValuesContextKey struct{}

// RouteInfo is a safe immutable view of a compiled route. Methods, Parameters,
// Middleware, and Metadata are copied whenever information crosses the API.
type RouteInfo struct {
	Name          string
	Methods       []string
	Host          string
	Pattern       string
	Parameters    []string
	Middleware    []string
	Metadata      map[string]string
	Documentation string
	Operation     string
	Source        string
}

type compiledHost struct {
	pattern   string
	mux       *http.ServeMux
	methods   map[string]struct{}
	redirects []*http.ServeMux
}

// Router is an immutable concurrency-safe compiled HTTP handler.
type Router struct {
	limits           Limits
	hosts            []compiledHost
	table            []RouteInfo
	supported        map[string]struct{}
	named            map[string]generationRoute
	notFound         http.Handler
	methodNotAllowed http.Handler
	automaticOptions bool
	redirectPolicy   RedirectPolicy
	canonicalizer    *http.ServeMux
}

// Routes returns the deterministic compiled route table.
func (r *Router) Routes() []RouteInfo {
	if r == nil {
		return nil
	}
	table := make([]RouteInfo, len(r.table))
	for index, info := range r.table {
		table[index] = cloneRouteInfo(info)
	}
	return table
}

// MatchedRoute returns the route selected for request, when called from its
// handler or middleware chain.
func MatchedRoute(request *http.Request) (RouteInfo, bool) {
	if request == nil {
		return RouteInfo{}, false
	}
	info, ok := request.Context().Value(routeContextKey{}).(RouteInfo)
	if !ok {
		return RouteInfo{}, false
	}
	return cloneRouteInfo(info), true
}

// ServeHTTP dispatches one request without mutating compiled state.
func (r *Router) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if r == nil || request == nil || request.URL == nil || invalidAuthority(request.Host) {
		writeError(writer, http.StatusBadRequest, "bad request")
		return
	}
	if len(request.Method) > r.limits.MaxMethodBytes || !isToken(request.Method) ||
		request.Method == http.MethodConnect {
		writeError(writer, http.StatusBadRequest, "bad request")
		return
	}
	if requestTargetTooLong(request, r.limits.MaxRequestTargetBytes) {
		writeError(writer, http.StatusRequestURITooLong, "request target too long")
		return
	}
	if request.RequestURI == "*" {
		if request.Method != http.MethodOptions {
			writeError(writer, http.StatusBadRequest, "bad request")
			return
		}
		if !r.automaticOptions {
			r.serveNotFound(writer, request)
			return
		}
		allowed := r.allMethods()
		if len(allowed) > 0 {
			writer.Header().Set("Allow", strings.Join(allowed, ", "))
		}
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	if r.redirectPolicy == FollowRedirects && nonCanonicalPath(request.URL.EscapedPath()) {
		r.canonicalizer.ServeHTTP(writer, request)
		return
	}

	hosts := r.matchingHosts(authorityHost(request.Host))
	if host := r.matchingHostForMethod(hosts, request); host != nil {
		host.mux.ServeHTTP(writer, request)
		return
	}
	allowed := r.allowedMethods(hosts, request)
	if request.Method == http.MethodOptions && r.automaticOptions && len(allowed) > 0 {
		writer.Header().Set("Allow", strings.Join(allowed, ", "))
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	if len(allowed) > 0 {
		writer.Header().Set("Allow", strings.Join(allowed, ", "))
		if r.methodNotAllowed != nil {
			r.methodNotAllowed.ServeHTTP(writer, request)
		} else {
			http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}
		return
	}
	if _, supported := r.supported[request.Method]; !supported && request.Method != http.MethodOptions {
		writeError(writer, http.StatusNotImplemented, "method not implemented")
		return
	}
	r.serveNotFound(writer, request)
}

func requestTargetTooLong(request *http.Request, limit int) bool {
	if len(request.RequestURI) > limit || len(request.URL.Path) > limit ||
		len(request.URL.RawPath) > limit || len(request.URL.RawQuery) > limit {
		return true
	}
	escapedPath := request.URL.EscapedPath()
	return len(escapedPath) > limit || len(request.URL.RawQuery) > limit-len(escapedPath)
}

func (r *Router) serveNotFound(writer http.ResponseWriter, request *http.Request) {
	if r.notFound != nil {
		r.notFound.ServeHTTP(writer, request)
		return
	}
	http.NotFound(writer, request)
}

func (r *Router) matchingHandler(infoIndex int, hostParameters []string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		info := cloneRouteInfo(r.table[infoIndex])
		current := make(map[string]struct{}, len(info.Parameters))
		for _, name := range info.Parameters {
			current[name] = struct{}{}
		}
		inherited, _ := request.Context().Value(pathValuesContextKey{}).(map[string]string)
		for name, value := range inherited {
			if _, shadowed := current[name]; !shadowed {
				request.SetPathValue(name, value)
			}
		}
		values, _ := matchHost(r.table[infoIndex].Host, authorityHost(request.Host))
		for index, name := range hostParameters {
			request.SetPathValue(name, values[index])
		}
		resolved := make(map[string]string, len(inherited)+len(info.Parameters))
		for name, value := range inherited {
			resolved[name] = value
		}
		for _, name := range info.Parameters {
			resolved[name] = request.PathValue(name)
		}
		ctx := context.WithValue(request.Context(), pathValuesContextKey{}, resolved)
		request = request.WithContext(context.WithValue(ctx, routeContextKey{}, info))
		handler.ServeHTTP(writer, request)
	})
}

func (r *Router) matchingHosts(host string) []*compiledHost {
	matches := make([]*compiledHost, 0, len(r.hosts))
	for index := range r.hosts {
		if _, matched := matchHost(r.hosts[index].pattern, host); matched {
			matches = append(matches, &r.hosts[index])
		}
	}
	return matches
}

func (r *Router) matchingHostForMethod(hosts []*compiledHost, request *http.Request) *compiledHost {
	escapedPath := request.URL.EscapedPath()
	for _, host := range hosts {
		if r.redirectPolicy == RejectRedirects && host.requiresRedirect(request) {
			continue
		}
		_, pattern := host.mux.Handler(request)
		if pattern != "" && (r.redirectPolicy == FollowRedirects || !nonCanonicalPath(escapedPath)) {
			return host
		}
	}
	return nil
}

func (r *Router) allowedMethods(hosts []*compiledHost, request *http.Request) []string {
	allowed := make(map[string]struct{})
	for _, host := range hosts {
		for method := range host.methods {
			candidate := request.Clone(request.Context())
			candidate.Method = method
			escapedPath := candidate.URL.EscapedPath()
			if r.redirectPolicy == RejectRedirects && host.requiresRedirect(candidate) {
				continue
			}
			_, pattern := host.mux.Handler(candidate)
			if pattern == "" || r.redirectPolicy == RejectRedirects && nonCanonicalPath(escapedPath) {
				continue
			}
			allowed[method] = struct{}{}
			if method == http.MethodGet {
				allowed[http.MethodHead] = struct{}{}
			}
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	if r.automaticOptions {
		allowed[http.MethodOptions] = struct{}{}
	}
	methods := make([]string, 0, len(allowed))
	for method := range allowed {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	return methods
}

func (h *compiledHost) requiresRedirect(request *http.Request) bool {
	if nonCanonicalPath(request.URL.EscapedPath()) {
		return true
	}
	for _, redirects := range h.redirects {
		if _, pattern := redirects.Handler(request); pattern != "" {
			return true
		}
	}
	return false
}

func redirectRoot(pattern string) string {
	if strings.HasSuffix(pattern, "/") {
		return strings.TrimSuffix(pattern, "/")
	}
	segments := strings.Split(pattern, "/")
	if len(segments) > 0 && (segments[len(segments)-1] == "{$}" || strings.HasSuffix(segments[len(segments)-1], "...}")) {
		return strings.TrimSuffix(strings.TrimSuffix(pattern, segments[len(segments)-1]), "/")
	}
	return ""
}

func nonCanonicalPath(value string) bool {
	if value == "" {
		return true
	}
	if value[0] != '/' {
		return true
	}
	cleaned := pathpkg.Clean(value)
	if strings.HasSuffix(value, "/") && cleaned != "/" {
		cleaned += "/"
	}
	return cleaned != value
}

func (r *Router) allMethods() []string {
	methods := make([]string, 0, len(r.supported)+1)
	for method := range r.supported {
		methods = append(methods, method)
	}
	if len(methods) > 0 {
		methods = append(methods, http.MethodOptions)
	}
	slices.Sort(methods)
	methods = slices.Compact(methods)
	return methods
}

func routeInformation(route Route, middleware []string, limits Limits) (RouteInfo, []string, error) {
	parameters := make([]string, 0)
	hostParameters := make([]string, 0)
	seen := make(map[string]struct{})
	for _, label := range strings.Split(route.Host, ".") {
		if isWildcardLabel(label) {
			name := label[1 : len(label)-1]
			if _, duplicate := seen[name]; duplicate {
				return RouteInfo{}, nil, &Error{Kind: ErrInvalidRoute, Field: "host", Source: route.Source, Detail: "duplicate wildcard name"}
			}
			seen[name] = struct{}{}
			parameters = append(parameters, name)
			hostParameters = append(hostParameters, name)
		}
	}
	for _, segment := range strings.Split(route.Path, "/") {
		if len(segment) < 3 || segment[0] != '{' || segment[len(segment)-1] != '}' || segment == "{$}" {
			continue
		}
		name := strings.TrimSuffix(segment[1:len(segment)-1], "...")
		if _, duplicate := seen[name]; duplicate {
			return RouteInfo{}, nil, &Error{Kind: ErrInvalidRoute, Field: "path", Source: route.Source, Detail: "duplicate wildcard name"}
		}
		seen[name] = struct{}{}
		parameters = append(parameters, name)
	}
	if len(parameters) > limits.MaxURLParameters {
		return RouteInfo{}, nil, &Error{Kind: ErrLimitExceeded, Field: "parameters", Source: route.Source, Detail: "parameter count exceeded"}
	}
	return RouteInfo{
		Name:          route.Name,
		Methods:       append([]string(nil), route.Methods...),
		Host:          route.Host,
		Pattern:       route.Path,
		Parameters:    parameters,
		Middleware:    append([]string(nil), middleware...),
		Metadata:      cloneMetadata(route.Metadata),
		Documentation: route.Documentation,
		Operation:     route.Operation,
		Source:        route.Source,
	}, hostParameters, nil
}

func cloneRouteInfo(info RouteInfo) RouteInfo {
	cloned := info
	cloned.Methods = append([]string(nil), info.Methods...)
	cloned.Parameters = append([]string(nil), info.Parameters...)
	cloned.Middleware = append([]string(nil), info.Middleware...)
	cloned.Metadata = cloneMetadata(info.Metadata)
	return cloned
}

func cloneMetadata(metadata map[string]string) map[string]string {
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func matchHost(pattern, host string) ([]string, bool) {
	if pattern == "" {
		return nil, true
	}
	patternLabels := strings.Split(pattern, ".")
	hostLabels := strings.Split(host, ".")
	if len(patternLabels) != len(hostLabels) {
		return nil, false
	}
	values := make([]string, 0)
	for index, label := range patternLabels {
		if isWildcardLabel(label) {
			if hostLabels[index] == "" {
				return nil, false
			}
			values = append(values, hostLabels[index])
			continue
		}
		if !strings.EqualFold(label, hostLabels[index]) {
			return nil, false
		}
	}
	return values, true
}

func isWildcardLabel(label string) bool {
	return len(label) > 2 && label[0] == '{' && label[len(label)-1] == '}'
}

func authorityHost(authority string) string {
	host, _, err := net.SplitHostPort(authority)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(authority, "[]")
}

func invalidAuthority(authority string) bool {
	if authority == "" {
		return false
	}
	if len(authority) > maxTrustedAuthorityBytes || !ascii(authority) ||
		strings.ContainsAny(authority, "\x00\r\n\\/@") {
		return true
	}
	parsed, err := url.Parse("//" + authority)
	if err != nil || parsed.Host != authority || parsed.User != nil ||
		parsed.Hostname() == "" || parsed.Path != "" {
		return true
	}
	if port := parsed.Port(); port != "" {
		number, parseErr := strconv.Atoi(port)
		return parseErr != nil || number < 1 || number > 65_535
	}
	return strings.HasSuffix(authority, ":")
}

func writeError(writer http.ResponseWriter, status int, message string) {
	http.Error(writer, message, status)
}
