package searchtest_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/search"
	"github.com/faustbrian/golib/pkg/search/searchtest"
)

func TestFakeConformsToDeclaredSharedSemantics(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	fake, err := searchtest.NewFake(limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := searchtest.RunConformance(t.Context(), searchtest.ConformanceConfig{
		Adapter: fake, Limits: limits,
		TenantA: "conformance-a", TenantB: "conformance-b", LogicalIndex: "documents",
		Refresh: search.RefreshWaitFor,
	}); err != nil {
		t.Fatal(err)
	}
}
