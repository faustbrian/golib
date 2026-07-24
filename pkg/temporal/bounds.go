// Package temporal defines shared bounds, Allen relations, typed errors, and
// resource limits for bounded temporal algebra packages.
package temporal

import "fmt"

// Side identifies one endpoint of a bounded interval.
type Side uint8

const (
	// Start is the lower or beginning endpoint.
	Start Side = iota + 1
	// End is the upper or ending endpoint.
	End
)

// Valid reports whether s identifies an interval endpoint.
func (s Side) Valid() bool { return s == Start || s == End }

// String returns the canonical endpoint name.
func (s Side) String() string {
	switch s {
	case Start:
		return "start"
	case End:
		return "end"
	default:
		return ""
	}
}

// Bounds describes endpoint inclusion for a bounded interval.
type Bounds uint8

const (
	// ClosedOpen includes the start and excludes the end. It is the zero-value
	// operational default.
	ClosedOpen Bounds = iota
	// Closed includes both endpoints.
	Closed
	// Open excludes both endpoints.
	Open
	// OpenClosed excludes the start and includes the end.
	OpenClosed
)

var allBounds = [...]Bounds{ClosedOpen, Closed, Open, OpenClosed}

// AllBounds returns the four supported modes in canonical order.
func AllBounds() []Bounds {
	result := make([]Bounds, len(allBounds))
	copy(result, allBounds[:])

	return result
}

// Valid reports whether b is one of the four supported modes.
func (b Bounds) Valid() bool {
	return b <= OpenClosed
}

// IncludesStart reports whether the start endpoint is a member.
func (b Bounds) IncludesStart() bool {
	return b == ClosedOpen || b == Closed
}

// IncludesEnd reports whether the end endpoint is a member.
func (b Bounds) IncludesEnd() bool {
	return b == Closed || b == OpenClosed
}

// Includes reports whether the selected endpoint is included.
func (b Bounds) Includes(side Side) (bool, error) {
	switch side {
	case Start:
		return b.IncludesStart(), nil
	case End:
		return b.IncludesEnd(), nil
	default:
		return false, fmt.Errorf("%w: invalid side %d", ErrBounds, side)
	}
}

// WithSide returns bounds with the selected endpoint included or excluded.
func (b Bounds) WithSide(side Side, included bool) (Bounds, error) {
	switch side {
	case Start:
		if included {
			return b.IncludeStart(), nil
		}
		return b.ExcludeStart(), nil
	case End:
		if included {
			return b.IncludeEnd(), nil
		}
		return b.ExcludeEnd(), nil
	default:
		return b, fmt.Errorf("%w: invalid side %d", ErrBounds, side)
	}
}

// IncludeStart returns a bounds value with an included start.
func (b Bounds) IncludeStart() Bounds {
	if b.IncludesEnd() {
		return Closed
	}

	return ClosedOpen
}

// IncludeEnd returns a bounds value with an included end.
func (b Bounds) IncludeEnd() Bounds {
	if b.IncludesStart() {
		return Closed
	}

	return OpenClosed
}

// ExcludeStart returns a bounds value with an excluded start.
func (b Bounds) ExcludeStart() Bounds {
	if b.IncludesEnd() {
		return OpenClosed
	}

	return Open
}

// ExcludeEnd returns a bounds value with an excluded end.
func (b Bounds) ExcludeEnd() Bounds {
	if b.IncludesStart() {
		return ClosedOpen
	}

	return Open
}

// String returns ISO 80000 bracket notation for the bounds.
func (b Bounds) String() string {
	switch b {
	case ClosedOpen:
		return "[)"
	case Closed:
		return "[]"
	case Open:
		return "()"
	case OpenClosed:
		return "(]"
	default:
		return ""
	}
}

// MarshalText implements encoding.TextMarshaler.
func (b Bounds) MarshalText() ([]byte, error) {
	if !b.Valid() {
		return nil, fmt.Errorf("%w: %d", ErrBounds, b)
	}

	return []byte(b.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (b *Bounds) UnmarshalText(text []byte) error {
	if len(text) > DefaultLimits().ParseBytes {
		return &LimitError{
			Field: "parse_bytes",
			Value: len(text),
			Max:   DefaultLimits().ParseBytes,
		}
	}

	var parsed Bounds
	switch string(text) {
	case "[)":
		parsed = ClosedOpen
	case "[]":
		parsed = Closed
	case "()":
		parsed = Open
	case "(]":
		parsed = OpenClosed
	default:
		return fmt.Errorf("%w: %q", ErrBounds, text)
	}

	*b = parsed

	return nil
}
