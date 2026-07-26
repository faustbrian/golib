package eventsourcing_test

import (
	"bufio"
	"strings"
	"testing"
)

func TestCoreModuleHasNoOptionalDependencyDirectives(t *testing.T) {
	t.Parallel()

	module := readContractFile(t, "go.mod")
	seen := map[string]bool{"module": false, "go": false}
	scanner := bufio.NewScanner(strings.NewReader(module))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "module", "go":
			if seen[fields[0]] {
				t.Fatalf("duplicate core module directive %q", fields[0])
			}
			seen[fields[0]] = true
		case "require", "replace", "exclude", "tool":
			t.Fatalf("core module contains optional dependency directive %q", fields[0])
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan core module: %v", err)
	}
	if !seen["module"] || !seen["go"] {
		t.Fatalf("core module directives = %#v", seen)
	}
}
