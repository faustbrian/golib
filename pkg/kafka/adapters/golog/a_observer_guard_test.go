package golog

import (
	"fmt"
	"testing"
)

func TestIdentityPolicyCriticalGuardsTerminateDeterministically(t *testing.T) {
	exactAllowlist := make([]string, maxAllowedValues)
	for index := range exactAllowlist {
		exactAllowlist[index] = fmt.Sprintf("client-%03d", index)
	}
	if _, err := normalizeAllowlist(
		exactAllowlist,
		maxIdentityLength,
		validIdentity,
	); err != nil {
		t.Fatalf("exact allowlist boundary error = %v", err)
	}

	for _, topic := range []string{
		"a", "z",
		"A", "Z",
		"0", "9",
		"a.b", "a_b", "a-b",
	} {
		if !validTopic(topic, maxTopicLength) {
			t.Fatalf("valid topic boundary %q rejected", topic)
		}
	}
	for _, topic := range []string{
		"`", "{",
		"@", "[",
		"/", ":",
	} {
		if validTopic(topic, maxTopicLength) {
			t.Fatalf("invalid topic boundary %q accepted", topic)
		}
	}
}
