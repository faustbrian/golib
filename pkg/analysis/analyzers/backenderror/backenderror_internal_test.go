package backenderror

import (
	"go/constant"
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"
)

func TestTracerBoundsAndMissingReferences(t *testing.T) {
	t.Parallel()

	trace := tracer{
		visited:      make(map[ssa.Value]struct{}),
		visitedCount: maximumTraceNodes,
		origins:      make(map[flowKey]struct{}),
	}
	trace.value(nil)
	trace.value(ssa.NewConst(constant.MakeString("value"), types.Typ[types.String]))
	if trace.visitedCount != maximumTraceNodes || len(trace.visited) != 0 {
		t.Fatal("bounded trace visited a value")
	}

	trace.visitedCount = 0
	value := ssa.NewConst(constant.MakeString("value"), types.Typ[types.String])
	trace.value(value)
	trace.value(value)
	trace.storedValues(value)
	if trace.visitedCount != 1 || len(trace.visited) != 1 {
		t.Fatalf("visited count = %d, values = %d", trace.visitedCount, len(trace.visited))
	}
}

func TestSortFlowsUsesStableDescriptions(t *testing.T) {
	t.Parallel()

	flows := []flowKey{
		{packagePath: "backend", symbol: "Save", result: 0},
		{packagePath: "backend", symbol: "Load", result: 1},
	}
	sortFlows(flows)
	if flows[0].symbol != "Load" || flows[1].symbol != "Save" {
		t.Fatalf("sortFlows() = %#v", flows)
	}
}
