package builder

import (
	"sort"
	"sync"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
)

// MethodRegistry is an explicitly owned concurrent registry. It starts no
// goroutines and returns immutable, lexically ordered snapshots.
type MethodRegistry struct {
	mu      sync.RWMutex
	methods map[string]openrpc.Method
}

// NewMethodRegistry constructs an empty registry.
func NewMethodRegistry() *MethodRegistry {
	return &MethodRegistry{methods: make(map[string]openrpc.Method)}
}

// Register atomically adds a method by its case-sensitive name.
func (registry *MethodRegistry) Register(method openrpc.Method) error {
	if registry == nil || method.Name() == "" {
		return ErrInvalidRegistry
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.methods == nil {
		registry.methods = make(map[string]openrpc.Method)
	}
	if _, duplicate := registry.methods[method.Name()]; duplicate {
		return ErrDuplicateMethod
	}
	registry.methods[method.Name()] = method
	return nil
}

// Methods returns a lexically ordered snapshot.
func (registry *MethodRegistry) Methods() []openrpc.Method {
	if registry == nil {
		return nil
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	names := make([]string, 0, len(registry.methods))
	for name := range registry.methods {
		names = append(names, name)
	}
	sort.Strings(names)
	methods := make([]openrpc.Method, len(names))
	for index, name := range names {
		methods[index] = registry.methods[name]
	}
	return methods
}
