package money_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompatibilityScriptUsesInjectedAPIDiff(t *testing.T) {
	t.Parallel()

	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "go.log")
	goPath := filepath.Join(bin, "go")
	fakeGo := `#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$FAKE_GO_LOG"
`
	if err := os.WriteFile(goPath, []byte(fakeGo), 0o700); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("./scripts/check-compatibility.sh")
	command.Env = append(
		os.Environ(),
		"FAKE_GO_LOG="+logPath,
		"GOLIB_APIDIFF=/tmp/injected-apidiff",
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run compatibility script: %v\n%s", err, output)
	}

	invocations, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "exec-tool /tmp/injected-apidiff"
	if strings.Count(string(invocations), want) != 2 {
		t.Fatalf("injected apidiff invocations = %q, want two %q calls", invocations, want)
	}
	if strings.Contains(string(invocations), " run ") || strings.HasPrefix(string(invocations), "run ") {
		t.Fatalf("compatibility script downloaded its own apidiff: %s", invocations)
	}
}
