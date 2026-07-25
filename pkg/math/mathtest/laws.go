// Package mathtest provides reusable assertions for numeric laws and codecs.
package mathtest

// T is the subset of testing.T used by the helpers.
type T interface {
	Helper()
	Errorf(format string, arguments ...any)
}

// EqualValue is a value with numeric equality.
type EqualValue[V any] interface {
	Equal(V) bool
}

// Operation is a possibly bounded or cancellable binary operation.
type Operation[V any] func(V, V) (V, error)

// Commutative checks operation(a, b) == operation(b, a) for every pair.
func Commutative[V EqualValue[V]](t T, values []V, operation Operation[V]) {
	t.Helper()
	for _, left := range values {
		for _, right := range values {
			forward, err := operation(left, right)
			if err != nil {
				t.Errorf("forward operation failed: %v", err)
				continue
			}
			reverse, err := operation(right, left)
			if err != nil {
				t.Errorf("reverse operation failed: %v", err)
				continue
			}
			if !forward.Equal(reverse) {
				t.Errorf("operation is not commutative for %v and %v", left, right)
			}
		}
	}
}

// Associative checks operation(operation(a, b), c) equals
// operation(a, operation(b, c)) for every triple.
func Associative[V EqualValue[V]](t T, values []V, operation Operation[V]) {
	t.Helper()
	for _, first := range values {
		for _, second := range values {
			for _, third := range values {
				leftPair, err := operation(first, second)
				if err != nil {
					t.Errorf("left pair failed: %v", err)
					continue
				}
				left, err := operation(leftPair, third)
				if err != nil {
					t.Errorf("left association failed: %v", err)
					continue
				}
				rightPair, err := operation(second, third)
				if err != nil {
					t.Errorf("right pair failed: %v", err)
					continue
				}
				right, err := operation(first, rightPair)
				if err != nil {
					t.Errorf("right association failed: %v", err)
					continue
				}
				if !left.Equal(right) {
					t.Errorf("operation is not associative for %v, %v, and %v", first, second, third)
				}
			}
		}
	}
}

// Identity checks both left and right identity behavior.
func Identity[V EqualValue[V]](t T, values []V, identity V, operation Operation[V]) {
	t.Helper()
	for _, value := range values {
		left, err := operation(identity, value)
		if err != nil {
			t.Errorf("left identity failed: %v", err)
			continue
		}
		right, err := operation(value, identity)
		if err != nil {
			t.Errorf("right identity failed: %v", err)
			continue
		}
		if !left.Equal(value) || !right.Equal(value) {
			t.Errorf("identity does not preserve %v", value)
		}
	}
}

// RoundTrip checks representation-preserving encode/decode behavior.
func RoundTrip[V EqualValue[V]](t T, values []V, encode func(V) ([]byte, error), decode func([]byte) (V, error)) {
	t.Helper()
	for _, value := range values {
		encoded, err := encode(value)
		if err != nil {
			t.Errorf("encode failed: %v", err)
			continue
		}
		decoded, err := decode(encoded)
		if err != nil {
			t.Errorf("decode failed: %v", err)
			continue
		}
		if !decoded.Equal(value) {
			t.Errorf("round trip changed %v", value)
		}
	}
}
