package stateapi

// Box is a generic composite value used by analyzer fixtures.
type Box[T any] struct {
	Value T
}
