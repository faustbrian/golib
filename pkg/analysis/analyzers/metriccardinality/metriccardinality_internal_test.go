package metriccardinality

import (
	"go/types"
	"testing"
)

func TestContainsHighCardinalityRejectsUnrelatedTypes(t *testing.T) {
	t.Parallel()

	configured := map[symbolKey]struct{}{}
	if containsHighCardinality(types.NewTuple(), configured) {
		t.Fatal("tuple unexpectedly contains high-cardinality evidence")
	}

	packageLess := types.NewNamed(
		types.NewTypeName(0, nil, "Local", nil),
		types.Typ[types.String],
		nil,
	)
	if containsHighCardinality(packageLess, configured) {
		t.Fatal("package-less named type unexpectedly contains evidence")
	}
}
