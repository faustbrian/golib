package resilience_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/faustbrian/golib/pkg/resilience"
)

func TestGeneratedPolicyStacksMatchReferenceInvocationOrder(t *testing.T) {
	t.Parallel()

	for depth := range 17 {
		for logicalPolicies := range depth + 1 {
			name := fmt.Sprintf("depth_%d_logical_%d", depth, logicalPolicies)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assertPolicyStackMatchesReference(t, depth, logicalPolicies)
			})
		}
	}
}

func FuzzPolicyStackOrderAndEventHistory(fuzz *testing.F) {
	fuzz.Add(uint8(0), uint8(0), uint8(0))
	fuzz.Add(uint8(8), uint8(3), uint8(4))
	fuzz.Add(uint8(16), uint8(16), uint8(64))
	fuzz.Fuzz(func(t *testing.T, rawDepth, rawLogical, rawEvents uint8) {
		depth := int(rawDepth % 17)
		logicalPolicies := int(rawLogical) % (depth + 1)
		maxEvents := int(rawEvents%64) + 1
		assertPolicyStackMatchesReferenceWithEvents(t, depth, logicalPolicies, maxEvents)
	})
}

func assertPolicyStackMatchesReference(t testing.TB, depth, logicalPolicies int) {
	t.Helper()
	assertPolicyStackMatchesReferenceWithEvents(t, depth, logicalPolicies, 128)
}

func assertPolicyStackMatchesReferenceWithEvents(t testing.TB, depth, logicalPolicies, maxEvents int) {
	t.Helper()

	order := make([]string, 0, depth*2+1)
	policies := make([]resilience.Policy[string], 0, depth)
	wantOrder := make([]string, 0, depth*2+1)
	for index := range depth {
		scope := resilience.ScopeAttempt
		if index < logicalPolicies {
			scope = resilience.ScopeLogical
		}
		id := resilience.PolicyID(fmt.Sprintf("policy-%02d", index))
		policies = append(policies, recordingPolicy{id: id, scope: scope, order: &order})
		wantOrder = append(wantOrder, "enter:"+string(id))
	}
	wantOrder = append(wantOrder, "operation")
	for index := depth - 1; index >= 0; index-- {
		wantOrder = append(wantOrder, fmt.Sprintf("exit:policy-%02d", index))
	}

	executor, err := resilience.NewExecutor[string](policies...)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	executor, err = executor.WithTimeline(maxEvents)
	if err != nil {
		t.Fatalf("with timeline: %v", err)
	}
	result := executor.Execute(context.Background(), metadataFor(t, "model", "resource"), func(context.Context, resilience.Attempt) (string, error) {
		order = append(order, "operation")
		return "ok", nil
	})
	if result.Value != "ok" || result.Err != nil || result.Outcome.Kind != resilience.OutcomeSuccess {
		t.Fatalf("result = %+v", result)
	}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("order = %v, want %v", order, wantOrder)
	}
	wantEvents := min(depth+4, maxEvents)
	if len(result.Events) != wantEvents {
		t.Fatalf("events = %d, want %d", len(result.Events), wantEvents)
	}
}
