package router

import (
	"errors"
	"strings"
)

// GroupOptions composes a host, path and name prefix, middleware, and metadata
// into every route registered by a group callback.
type GroupOptions struct {
	Host       string
	PathPrefix string
	NamePrefix string
	Middleware []NamedMiddleware
	Metadata   map[string]string
}

type groupState struct {
	host       string
	pathPrefix string
	namePrefix string
	middleware []NamedMiddleware
	metadata   map[string]string
}

// Group transactionally flattens routes registered by define. If validation
// or define fails, no route from the group is published to the parent.
func (b *Builder) Group(options GroupOptions, define func(*Builder) error) error {
	if b == nil {
		return &Error{Kind: ErrCompileState, Field: "builder", Detail: "nil builder"}
	}
	if b.optionErr != nil {
		return b.optionErr
	}
	if b.compiled {
		return b.routeError(ErrCompileState, "builder", "", "already compiled")
	}
	if define == nil {
		return b.routeError(ErrInvalidRoute, "group", "", "nil group callback")
	}
	if b.groupDepth+1 > b.limits.MaxGroupDepth {
		return b.routeError(ErrLimitExceeded, "group", "", "group depth exceeded")
	}
	if b.groups >= b.limits.MaxGroups {
		return b.routeError(ErrLimitExceeded, "group", "", "group count exceeded")
	}
	state, err := b.composeGroup(options)
	if err != nil {
		return err
	}
	childLimits := b.limits
	childLimits.MaxGroups = b.limits.MaxGroups - b.groups - 1
	childLimits.MaxRoutes = b.limits.MaxRoutes - len(b.routes)
	child := &Builder{
		limits:           childLimits,
		globalMiddleware: append([]NamedMiddleware(nil), b.globalMiddleware...),
		notFound:         b.notFound,
		methodNotAllowed: b.methodNotAllowed,
		automaticOptions: b.automaticOptions,
		redirectPolicy:   b.redirectPolicy,
		group:            state,
		groupDepth:       b.groupDepth + 1,
	}
	err = define(child)
	child.compiled = true
	if err != nil {
		return err
	}
	addedGroups := 1 + child.groups
	b.routes = append(b.routes, child.routes...)
	b.groups += addedGroups
	return nil
}

func (b *Builder) composeGroup(options GroupOptions) (groupState, error) {
	if options.Host != "" {
		if err := b.validateHost(options.Host, ""); err != nil {
			return groupState{}, err
		}
		if b.group.host != "" && !strings.EqualFold(b.group.host, options.Host) {
			return groupState{}, b.routeError(ErrInvalidRoute, "host", "", "nested group host conflict")
		}
	}
	if err := b.validatePrefix(options.PathPrefix); err != nil {
		return groupState{}, err
	}
	pathPrefix := joinPrefix(b.group.pathPrefix, options.PathPrefix)
	if len(pathPrefix) > b.limits.MaxPatternBytes {
		return groupState{}, b.routeError(ErrLimitExceeded, "path", "", "composed group path prefix exceeded")
	}
	if len(options.NamePrefix) > b.limits.MaxNameBytes {
		return groupState{}, b.routeError(ErrLimitExceeded, "name", "", "group name prefix is too long")
	}
	if options.NamePrefix != "" && !validName(options.NamePrefix) {
		return groupState{}, b.routeError(ErrInvalidRoute, "name", "", "invalid group name prefix")
	}
	namePrefix := b.group.namePrefix + options.NamePrefix
	if len(namePrefix) > b.limits.MaxNameBytes {
		return groupState{}, b.routeError(ErrLimitExceeded, "name", "", "composed group name prefix exceeded")
	}
	if len(b.group.middleware)+len(options.Middleware) > b.limits.MaxMiddleware {
		return groupState{}, b.routeError(ErrLimitExceeded, "middleware", "", "group middleware depth exceeded")
	}
	for _, middleware := range options.Middleware {
		if len(middleware.Name) > b.limits.MaxNameBytes {
			return groupState{}, b.routeError(ErrLimitExceeded, "middleware", "", "group middleware name is too long")
		}
		if middleware.Middleware == nil || middleware.Name != "" && !validName(middleware.Name) {
			return groupState{}, b.routeError(ErrInvalidRoute, "middleware", "", "invalid group middleware")
		}
	}
	metadata, err := mergeMetadata(b.group.metadata, options.Metadata, b.limits)
	if err != nil {
		return groupState{}, err
	}
	host := b.group.host
	if host == "" {
		host = options.Host
	}
	return groupState{
		host:       host,
		pathPrefix: pathPrefix,
		namePrefix: namePrefix,
		middleware: append(append([]NamedMiddleware(nil), b.group.middleware...), options.Middleware...),
		metadata:   metadata,
	}, nil
}

func (b *Builder) flattenRoute(route Route) (Route, error) {
	flattened := cloneRoute(route)
	if b.group.host != "" {
		if flattened.Host != "" && !strings.EqualFold(flattened.Host, b.group.host) {
			return Route{}, b.routeError(ErrInvalidRoute, "host", route.Source, "route and group host conflict")
		}
		flattened.Host = b.group.host
	}
	flattened.Path = joinPrefix(b.group.pathPrefix, flattened.Path)
	flattened.Name = b.group.namePrefix + flattened.Name
	groupMiddleware := excludeInheritedMiddleware(b.group.middleware, flattened.ExcludeMiddleware)
	flattened.Middleware = append(groupMiddleware, flattened.Middleware...)
	metadata, err := mergeMetadata(b.group.metadata, flattened.Metadata, b.limits)
	if err != nil {
		var routeError *Error
		if errors.As(err, &routeError) {
			routeError.Source = route.Source
		}
		return Route{}, err
	}
	flattened.Metadata = metadata
	return flattened, nil
}

func excludeInheritedMiddleware(middleware []NamedMiddleware, exclusions []string) []NamedMiddleware {
	excluded := make(map[string]struct{}, len(exclusions))
	for _, name := range exclusions {
		excluded[name] = struct{}{}
	}
	filtered := make([]NamedMiddleware, 0, len(middleware))
	for _, candidate := range middleware {
		if candidate.Name != "" {
			if _, skip := excluded[candidate.Name]; skip {
				continue
			}
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func (b *Builder) validatePrefix(prefix string) error {
	if prefix == "" || prefix == "/" {
		return nil
	}
	if len(prefix) > b.limits.MaxPatternBytes {
		return b.routeError(ErrLimitExceeded, "path", "", "group path prefix is too long")
	}
	lower := strings.ToLower(prefix)
	if prefix[0] != '/' ||
		strings.ContainsAny(prefix, "{}?#") || strings.Contains(prefix, "//") ||
		strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return b.routeError(ErrInvalidRoute, "path", "", "invalid group path prefix")
	}
	for _, segment := range strings.Split(prefix, "/") {
		if segment == "." || segment == ".." || strings.EqualFold(segment, "%2e") || strings.EqualFold(segment, "%2e%2e") {
			return b.routeError(ErrInvalidRoute, "path", "", "invalid group path prefix")
		}
	}
	return nil
}

func joinPrefix(prefix, path string) string {
	if prefix == "" || prefix == "/" {
		return path
	}
	if path == "" {
		return strings.TrimSuffix(prefix, "/")
	}
	return strings.TrimSuffix(prefix, "/") + path
}

func mergeMetadata(inherited, local map[string]string, limits Limits) (map[string]string, error) {
	if len(inherited)+len(local) > limits.MaxMetadataEntries {
		return nil, &Error{Kind: ErrLimitExceeded, Field: "metadata", Detail: "metadata entry count exceeded"}
	}
	merged := cloneMetadata(inherited)
	for key, value := range local {
		if err := validateMetadataEntry(key, value, limits); err != nil {
			return nil, err
		}
		if _, conflict := merged[key]; conflict {
			return nil, &Error{Kind: ErrInvalidRoute, Field: "metadata", Detail: "metadata key conflict"}
		}
		merged[key] = value
	}
	return merged, nil
}

func validateMetadataEntry(key, value string, limits Limits) *Error {
	if key == "" || len(key) > limits.MaxMetadataKeyBytes || len(value) > limits.MaxMetadataValueBytes {
		return &Error{Kind: ErrLimitExceeded, Field: "metadata", Detail: "invalid metadata entry"}
	}
	return nil
}
