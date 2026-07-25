package temporal_test

import (
	"testing"

	temporal "github.com/faustbrian/golib/pkg/temporal"
)

func TestRelationsHaveCoherentConverses(t *testing.T) {
	t.Parallel()

	for _, relation := range temporal.AllRelations() {
		if !relation.Valid() {
			t.Fatalf("relation %d is not valid", relation)
		}

		if got := relation.Converse().Converse(); got != relation {
			t.Fatalf("Converse().Converse() = %v, want %v", got, relation)
		}

		if relation.String() == "" {
			t.Fatalf("String() is empty for relation %d", relation)
		}
	}
}

func TestRelationsAreExhaustiveAndUnique(t *testing.T) {
	t.Parallel()

	relations := temporal.AllRelations()
	if len(relations) != 13 {
		t.Fatalf("len(AllRelations()) = %d, want 13", len(relations))
	}

	seen := make(map[temporal.Relation]struct{}, len(relations))
	for _, relation := range relations {
		if _, ok := seen[relation]; ok {
			t.Fatalf("duplicate relation %v", relation)
		}
		seen[relation] = struct{}{}
	}
}

func TestInvalidRelationIsSafe(t *testing.T) {
	t.Parallel()

	invalid := temporal.Relation(255)
	if invalid.Valid() {
		t.Fatal("Valid() = true for unknown relation")
	}

	if got := invalid.Converse(); got != temporal.RelationInvalid {
		t.Fatalf("Converse() = %v, want RelationInvalid", got)
	}
	if got := invalid.String(); got != "" {
		t.Fatalf("String() = %q, want empty string", got)
	}
}
