package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func FuzzRunArgv(f *testing.F) {
	for _, seed := range [][]byte{
		{},
		[]byte("deploy\x00--force\x00target"),
		[]byte("deploy\x00--\x00--literal"),
		{0xff, 0x00, '-', 'x'},
		[]byte("__complete\x00de"),
	} {
		f.Add(seed)
	}
	force := cli.BoolOption("force").Short('f')
	target := cli.StringArgument("target")
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithSubcommands(cli.NewCommand(
			"deploy",
			cli.WithOptions(force),
			cli.WithArguments(target),
		)),
	))
	if err != nil {
		f.Fatalf("compile fuzz application: %v", err)
	}
	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 1<<20 {
			t.Skip()
		}
		parts := strings.Split(string(encoded), "\x00")
		result := application.Run(context.Background(), cli.Request{Args: parts})
		if result.ExitCode < 0 || result.ExitCode > 255 {
			t.Fatalf("non-portable exit code %d", result.ExitCode)
		}
	})
}

func FuzzCompileCommandGraph(f *testing.F) {
	f.Add("root", "child", "alias")
	f.Add("røøt", "e\u0301", "é")
	f.Add("root", "bad name", "\x1b")
	f.Fuzz(func(t *testing.T, rootName, childName, alias string) {
		if len(rootName)+len(childName)+len(alias) > 1<<16 {
			t.Skip()
		}
		root := cli.NewCommand(
			rootName,
			cli.WithSubcommands(cli.NewCommand(childName, cli.WithAliases(alias))),
		)
		_, _ = cli.Compile(root)
	})
}

func FuzzTypedConversion(f *testing.F) {
	f.Add("0", "1s", "json")
	f.Add("-9223372036854775808", "2562047h47m16.854775807s", "text")
	f.Add("9223372036854775808", "invalid", "secret\x1b")
	integer := cli.IntArgument("integer")
	duration := cli.DurationArgument("duration")
	format := cli.EnumArgument("format", "json", "text")
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithArguments(integer, duration, format),
	))
	if err != nil {
		f.Fatalf("compile conversion application: %v", err)
	}
	f.Fuzz(func(t *testing.T, integerValue, durationValue, formatValue string) {
		if len(integerValue)+len(durationValue)+len(formatValue) > 1<<20 {
			t.Skip()
		}
		result := application.Run(context.Background(), cli.Request{Args: []string{
			integerValue, durationValue, formatValue,
		}})
		if result.ExitCode < 0 || result.ExitCode > 255 {
			t.Fatalf("non-portable exit code %d", result.ExitCode)
		}
	})
}

func FuzzCompletionPartialArgv(f *testing.F) {
	f.Add([]byte("de"))
	f.Add([]byte("deploy\x00--fo"))
	f.Add([]byte{0xff})
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithSubcommands(cli.NewCommand(
			"deploy",
			cli.WithOptions(cli.StringOption("format")),
		)),
	))
	if err != nil {
		f.Fatalf("compile completion application: %v", err)
	}
	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 1<<20 {
			t.Skip()
		}
		_, _ = application.Complete(context.Background(), strings.Split(string(encoded), "\x00"))
	})
}

func FuzzLifecycleFailureAndCancellation(f *testing.F) {
	for _, seed := range []uint8{0, 1, 2, 4, 8, 16, 32, 64, 127, 255} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, sequence uint8) {
		ctx, cancel := context.WithCancelCause(context.Background())
		failure := errors.New("phase failure")
		cleanups := 0
		handlers := 0
		phase := func(mask uint8) func(context.Context, cli.Invocation) error {
			return func(context.Context, cli.Invocation) error {
				if sequence&mask != 0 {
					return failure
				}
				if sequence&0x80 != 0 {
					cancel(errors.New("fuzz cancellation"))
				}
				return nil
			}
		}
		application, err := cli.Compile(cli.NewCommand(
			"tool",
			cli.WithValidation(func(context.Context, cli.Input) error {
				if sequence&1 != 0 {
					return failure
				}
				return nil
			}),
			cli.WithMiddleware(func(ctx context.Context, _ cli.CommandMetadata, next cli.Next) error {
				if sequence&2 != 0 {
					return failure
				}
				return next(ctx)
			}),
			cli.WithPreRun(phase(4)),
			cli.WithHandler(func(ctx context.Context, invocation cli.Invocation) error {
				handlers++
				return phase(8)(ctx, invocation)
			}),
			cli.WithPostRun(phase(16)),
			cli.WithCleanup(func(context.Context, cli.Invocation) error {
				cleanups++
				if sequence&32 != 0 {
					return failure
				}
				return nil
			}),
		))
		if err != nil {
			t.Fatal(err)
		}
		result := application.Run(ctx, cli.Request{})
		if result.ExitCode < 0 || result.ExitCode > 255 || handlers > 1 || cleanups > 1 {
			t.Fatalf("invalid lifecycle result %#v, handlers=%d cleanups=%d", result, handlers, cleanups)
		}
	})
}

func FuzzHelpMarkdownAndManifestGeneration(f *testing.F) {
	f.Add("summary", "description", "example", uint16(80))
	f.Add("safe\u202eunsafe", "line\x1b[31m", "one\rtwo", uint16(1))
	f.Add(strings.Repeat("x", 1024), "", "", uint16(65535))
	f.Fuzz(func(t *testing.T, summary, description, example string, width uint16) {
		if len(summary)+len(description)+len(example) > 1<<15 {
			t.Skip()
		}
		application, err := cli.Compile(cli.NewCommand(
			"tool",
			cli.WithSummary(summary),
			cli.WithDescription(description),
			cli.WithExamples(example),
			cli.WithOptions(cli.EnumOption("mode", "safe", "fast")),
		))
		if err != nil {
			return
		}
		help, err := application.Help(nil, cli.HelpOptions{Width: int(width)})
		if err != nil {
			t.Fatal(err)
		}
		markdown, err := application.Markdown()
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := application.ManifestJSON()
		if err != nil {
			t.Fatal(err)
		}
		var decoded cli.Manifest
		if err := json.Unmarshal(manifest, &decoded); err != nil || decoded.Root.Name != "tool" {
			t.Fatalf("invalid manifest: %v, %#v", err, decoded)
		}
		if containsTerminalControl(help) || containsTerminalControl(markdown) {
			t.Fatal("generated human documentation retained terminal controls")
		}
		shells := []cli.Shell{cli.ShellBash, cli.ShellZsh, cli.ShellFish, cli.ShellPowerShell}
		completion, err := application.Completion(shells[int(width)%len(shells)])
		if err != nil || strings.Contains(completion, "eval") || strings.Contains(completion, "Invoke-Expression") {
			t.Fatalf("unsafe completion generation: %v", err)
		}
	})
}

func FuzzJSONErrorAndSuccessRendering(f *testing.F) {
	f.Add([]byte("ordinary"), false)
	f.Add([]byte("control\x1b\u202e\r\n"), true)
	f.Add([]byte{0xff, 0xfe}, false)
	f.Fuzz(func(t *testing.T, value []byte, fail bool) {
		if len(value) > 1<<15 {
			t.Skip()
		}
		application, err := cli.Compile(cli.NewCommand(
			"tool",
			cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
				if fail {
					return errors.New(string(value))
				}
				return invocation.Output().SetData(map[string]string{"value": string(value)})
			}),
		))
		if err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		result := application.Run(context.Background(), cli.Request{
			Stdout: &stdout, Stderr: &stderr,
			Output: cli.OutputPolicy{Mode: cli.OutputJSON},
		})
		var envelope map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
			t.Fatalf("invalid JSON output %q: %v", stdout.Bytes(), err)
		}
		if stderr.Len() != 0 || result.ExitCode < 0 || result.ExitCode > 255 {
			t.Fatalf("JSON boundary = %#v, stderr %q", result, stderr.Bytes())
		}
	})
}

func FuzzHumanTerminalRendering(f *testing.F) {
	f.Add([]byte("ordinary"))
	f.Add([]byte("ansi\x1b[31m osc\x1b]8;;url\a bidi\u202e return\r"))
	f.Add([]byte{0xff, 'x'})
	f.Fuzz(func(t *testing.T, value []byte) {
		if len(value) > 1<<15 {
			t.Skip()
		}
		application, err := cli.Compile(cli.NewCommand(
			"tool",
			cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
				return invocation.Output().Info(string(value))
			}),
		))
		if err != nil {
			t.Fatal(err)
		}
		var stdout bytes.Buffer
		result := application.Run(context.Background(), cli.Request{Stdout: &stdout})
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		if containsTerminalControl(stdout.String()) {
			t.Fatalf("human output retained terminal controls: %q", stdout.String())
		}
	})
}

func containsTerminalControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) && character != '\n' && character != '\t' ||
			unicode.Is(unicode.Bidi_Control, character) {
			return true
		}
	}
	return false
}
