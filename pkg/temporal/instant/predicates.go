package instant

import (
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// IsBefore reports the formal Allen before relation.
func (p Period) IsBefore(other Period) bool { return p.hasRelation(other, temporal.Before) }

// IsAfter reports the formal Allen after relation.
func (p Period) IsAfter(other Period) bool { return p.hasRelation(other, temporal.After) }

// Starts reports the formal Allen starts relation.
func (p Period) Starts(other Period) bool { return p.hasRelation(other, temporal.Starts) }

// Finishes reports the formal Allen finishes relation.
func (p Period) Finishes(other Period) bool { return p.hasRelation(other, temporal.Finishes) }

// During reports the formal Allen during relation.
func (p Period) During(other Period) bool { return p.hasRelation(other, temporal.During) }

// Overlaps reports whether the represented sets share at least one instant.
func (p Period) Overlaps(other Period) bool {
	_, ok := p.Intersect(other)
	return ok
}

// Contains reports whether every member of other is a member of p.
func (p Period) Contains(other Period) bool {
	if other.IsEmpty() {
		return true
	}
	if p.IsEmpty() {
		return false
	}
	intersection, ok := p.Intersect(other)
	return ok && intersection.SetEqual(other)
}

func (p Period) hasRelation(other Period, want temporal.Relation) bool {
	relation, err := p.RelationTo(other)
	return err == nil && relation == want
}
