package policy_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/policy"
)

func TestBuiltinRegistryOwnsEveryShippedRule(t *testing.T) {
	t.Parallel()

	registry, err := policy.Builtin()
	if err != nil {
		t.Fatalf("Builtin() error = %v", err)
	}
	want := []string{
		"api/backend-error-boundary",
		"api/forbidden-call",
		"api/interface-naming",
		"api/interface-placement",
		"architecture/import-boundary",
		"context/blocking-api-context",
		"context/no-background",
		"context/no-stored-context",
		"http/client-timeout",
		"http/no-default-client",
		"lifecycle/cleanup-ownership",
		"lifecycle/lock-across-call",
		"lifecycle/no-constructor-goroutine",
		"lifecycle/no-global-goroutine",
		"lifecycle/no-init",
		"lifecycle/no-process-control",
		"lifecycle/transaction-rollback",
		"lifecycle/unbounded-goroutine-fanout",
		"observability/dynamic-label-name",
		"observability/high-cardinality-label",
		"safety/no-mutable-global",
		"security/no-unsafe",
		"security/sensitive-sink",
	}
	if got := registry.IDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs() = %#v, want %#v", got, want)
	}
	entries := registry.Entries()
	if len(entries) != len(want) {
		t.Fatalf("len(Entries()) = %d, want %d", len(entries), len(want))
	}
	entries[0].Owner = "mutated"
	second := registry.Entries()
	if len(second) == 0 {
		t.Fatal("Entries() unexpectedly became empty")
	}
	if second[0].Owner == "mutated" {
		t.Fatal("Entries() exposed registry storage")
	}
}

func TestBuiltinRulesAreDocumented(t *testing.T) {
	t.Parallel()

	registry, err := policy.Builtin()
	if err != nil {
		t.Fatalf("Builtin() error = %v", err)
	}
	catalog, err := os.ReadFile("../docs/rules.md")
	if err != nil {
		t.Fatalf("ReadFile(rule catalog) error = %v", err)
	}
	for _, id := range registry.IDs() {
		if !strings.Contains(string(catalog), "`"+id+"`") {
			t.Errorf("rule catalog does not document %s", id)
		}
	}
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatalf("ReadFile(README) error = %v", err)
	}
	for _, command := range []string{
		"analysis check",
		"go vet -vettool",
		"analysis rules",
		"analysis validate-config",
	} {
		if !strings.Contains(string(readme), command) {
			t.Errorf("README does not document %q", command)
		}
	}
}
