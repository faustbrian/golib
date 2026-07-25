package temporal

// Relation is one of Allen's thirteen relations for two non-empty intervals.
type Relation uint8

const (
	// RelationInvalid is the safe zero value and is not an Allen relation.
	RelationInvalid Relation = iota
	// Before ends strictly before the other interval starts.
	Before
	// Meets ends exactly where the other interval starts.
	Meets
	// Overlaps starts first and ends inside the other interval.
	Overlaps
	// Starts shares the start and ends first.
	Starts
	// During starts after and ends before the other interval.
	During
	// Finishes starts later and shares the end.
	Finishes
	// Equal shares both endpoints with the other interval.
	Equal
	// FinishedBy starts first and shares the end.
	FinishedBy
	// Contains starts before and ends after the other interval.
	Contains
	// StartedBy shares the start and ends after the other interval.
	StartedBy
	// OverlappedBy starts inside the other interval and ends later.
	OverlappedBy
	// MetBy starts exactly where the other interval ends.
	MetBy
	// After starts strictly after the other interval ends.
	After
)

var allRelations = [...]Relation{
	Before,
	Meets,
	Overlaps,
	Starts,
	During,
	Finishes,
	Equal,
	FinishedBy,
	Contains,
	StartedBy,
	OverlappedBy,
	MetBy,
	After,
}

// AllRelations returns all Allen relations in canonical order.
func AllRelations() []Relation {
	result := make([]Relation, len(allRelations))
	copy(result, allRelations[:])

	return result
}

// Valid reports whether r is one of Allen's thirteen relations.
func (r Relation) Valid() bool {
	return r >= Before && r <= After
}

// Converse returns the same relation with the operands exchanged.
func (r Relation) Converse() Relation {
	switch r {
	case Before:
		return After
	case Meets:
		return MetBy
	case Overlaps:
		return OverlappedBy
	case Starts:
		return StartedBy
	case During:
		return Contains
	case Finishes:
		return FinishedBy
	case Equal:
		return Equal
	case FinishedBy:
		return Finishes
	case Contains:
		return During
	case StartedBy:
		return Starts
	case OverlappedBy:
		return Overlaps
	case MetBy:
		return Meets
	case After:
		return Before
	default:
		return RelationInvalid
	}
}

// String returns the canonical lower-case relation name.
func (r Relation) String() string {
	switch r {
	case Before:
		return "before"
	case Meets:
		return "meets"
	case Overlaps:
		return "overlaps"
	case Starts:
		return "starts"
	case During:
		return "during"
	case Finishes:
		return "finishes"
	case Equal:
		return "equals"
	case FinishedBy:
		return "finished-by"
	case Contains:
		return "contains"
	case StartedBy:
		return "started-by"
	case OverlappedBy:
		return "overlapped-by"
	case MetBy:
		return "met-by"
	case After:
		return "after"
	default:
		return ""
	}
}
