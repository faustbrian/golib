package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestNextStableVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		part    string
		current string
		want    string
	}{
		{name: "initial patch", part: "patch", current: "v0.0.0", want: "v0.0.1"},
		{name: "patch", part: "patch", current: "v1.2.3", want: "v1.2.4"},
		{name: "minor", part: "minor", current: "v1.2.3", want: "v1.3.0"},
		{name: "major", part: "major", current: "v1.2.3", want: "v2.0.0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command("go", "run", ".", "next", test.part, test.current)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("semvercheck next error = %v: %s", err, output)
			}
			if got := strings.TrimSpace(string(output)); got != test.want {
				t.Errorf("semvercheck next output = %q, want %q", got, test.want)
			}
		})
	}
}
