package openinghours

const (
	// MaxCompositionDepth bounds immutable algebra expression nesting.
	MaxCompositionDepth = 16
)

type compositionOperation uint8

const (
	compositionUnion compositionOperation = iota
	compositionIntersection
	compositionSubtract
	compositionOverlay
)

type composition struct {
	operation compositionOperation
	left      Schedule
	right     Schedule
}

// Union returns a schedule open whenever either operand is open.
func (s Schedule) Union(other Schedule) (Schedule, error) {
	return s.compose(other, compositionUnion)
}

// Intersection returns a schedule open only when both operands are open.
func (s Schedule) Intersection(other Schedule) (Schedule, error) {
	return s.compose(other, compositionIntersection)
}

// Subtract returns a schedule open when s is open and other is closed.
func (s Schedule) Subtract(other Schedule) (Schedule, error) {
	return s.compose(other, compositionSubtract)
}

// Overlay returns a schedule where explicit right-hand rules override left-hand
// availability and inherited right-hand rules leave it unchanged.
func (s Schedule) Overlay(other Schedule) (Schedule, error) {
	return s.compose(other, compositionOverlay)
}

func (s Schedule) compose(other Schedule, operation compositionOperation) (Schedule, error) {
	if s.data == nil || other.data == nil || s.data.timezone != other.data.timezone {
		return Schedule{}, newError("compose", CodeTimezoneMismatch)
	}
	depth := max(s.data.depth, other.data.depth) + 1
	if depth > MaxCompositionDepth {
		return Schedule{}, newError("compose", CodeLimitExceeded)
	}

	return Schedule{data: &scheduleData{
		timezone:    s.data.timezone,
		location:    s.data.location,
		depth:       depth,
		composition: &composition{operation: operation, left: s, right: other},
	}}, nil
}
