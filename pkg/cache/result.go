package cache

// State describes whether a cache lookup is fresh, absent, or stale.
type State uint8

const (
	// Hit indicates a fresh cached value.
	Hit State = iota + 1
	// Miss indicates no usable cached value.
	Miss
	// Stale indicates a decoded value beyond its fresh TTL but within StaleFor.
	Stale
)

// Result is the explicit outcome of a typed cache read.
type Result[V any] struct {
	State    State
	Value    V
	Negative bool
}
