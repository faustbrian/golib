package audit_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentationGateFailsWhenPackageDiscoveryFails(t *testing.T) {
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	fakeGo := filepath.Join(bin, "go")
	if err := os.WriteFile(fakeGo, []byte(`#!/bin/sh
if [ "$1" = "list" ]; then
    printf '%s\n' 'injected package discovery failure' >&2
    exit 23
fi
exec "$REAL_GO" "$@"
`), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("./scripts/check-docs.sh")
	command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "REAL_GO="+realGo)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("check-docs.sh succeeded after package discovery failed:\n%s", output)
	}
	if !strings.Contains(string(output), "injected package discovery failure") {
		t.Fatalf("check-docs.sh output = %q, want injected failure", output)
	}
}
