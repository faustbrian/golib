package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func TestBidiControlsCannotReachCommandSurfaces(t *testing.T) {
	t.Parallel()

	const bidi = "\u202e"
	if _, err := cli.Compile(cli.NewCommand("safe" + bidi + "name")); err == nil {
		t.Fatal("Compile() error = nil, want bidi command name rejection")
	}

	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
			return invocation.Output().Info("before" + bidi + "after")
		}),
	))
	if err != nil {
		t.Fatalf("compile safe command: %v", err)
	}
	stdout := new(bytes.Buffer)
	result := application.Run(context.Background(), cli.Request{Stdout: stdout})
	if result.Err != nil {
		t.Fatalf("Run() error = %v", result.Err)
	}
	if strings.Contains(stdout.String(), bidi) {
		t.Fatalf("stdout contains bidi control: %q", stdout.String())
	}

	result = application.Run(context.Background(), cli.Request{Args: []string{"--unsafe" + bidi}})
	if result.Err == nil {
		t.Fatal("Run() error = nil, want unknown option")
	}
	if strings.Contains(result.Err.Error(), bidi) {
		t.Fatalf("public error contains bidi control: %q", result.Err.Error())
	}
}
