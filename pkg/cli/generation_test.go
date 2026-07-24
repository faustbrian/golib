package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func TestHelpIsGeneratedFromCompiledMetadata(t *testing.T) {
	t.Parallel()

	application := generationApplication(t)
	help, err := application.Help([]string{"deploy"}, cli.HelpOptions{Width: 48})
	if err != nil {
		t.Fatalf("generate help: %v", err)
	}
	want := `Deploy an application.

Usage:
  tool deploy [options] <target> [extra...]

Arguments:
  target       deployment target
  extra        additional deployment arguments

Options:
  -f, --force  replace an existing deployment
  -v, --verbose
               enable diagnostic output
               (inherited from tool)

Aliases:
  ship

Examples:
  tool deploy production

Documentation:
  https://example.com/deploy
`
	if help != want {
		t.Fatalf("help mismatch\n--- got ---\n%s--- want ---\n%s", help, want)
	}
}

func TestManifestAndMarkdownAreDeterministic(t *testing.T) {
	t.Parallel()

	application := generationApplication(t)
	first, err := application.ManifestJSON()
	if err != nil {
		t.Fatalf("generate manifest: %v", err)
	}
	second, err := application.ManifestJSON()
	if err != nil {
		t.Fatalf("regenerate manifest: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("manifest output changed between reads")
	}
	var manifest cli.Manifest
	if err := json.Unmarshal(first, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Schema != "go-cli/manifest/v1" || manifest.Name != "tool" || manifest.Version != "1.2.3" {
		t.Fatalf("manifest header = %#v", manifest)
	}
	if len(manifest.Commands) != 1 || manifest.Commands[0].Name != "deploy" {
		t.Fatalf("manifest commands = %#v", manifest.Commands)
	}
	if got := manifest.Commands[0].InheritedOptions[0].Source; got != "tool" {
		t.Fatalf("inherited source = %q, want tool", got)
	}

	markdown, err := application.Markdown()
	if err != nil {
		t.Fatalf("generate Markdown: %v", err)
	}
	if strings.HasSuffix(markdown, "\n\n") {
		t.Fatalf("Markdown has more than one trailing newline: %q", markdown)
	}
	for _, expected := range []string{
		"# `tool` command reference",
		"## `tool deploy`",
		"Deploy an application.",
		"### Arguments",
		"`target` (`string`, required)",
		"`-f`, `--force`",
		"Inherited from `tool`",
		"### Aliases",
		"`ship`",
		"### Examples",
		"tool deploy production",
		"https://example.com/deploy",
	} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("Markdown does not contain %q:\n%s", expected, markdown)
		}
	}
}

func TestMarkdownStripsControlsFromAllMetadata(t *testing.T) {
	t.Parallel()

	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithSummary("safe\u202eunsafe"),
		cli.WithDescription("description\x1b[31m"),
		cli.WithExamples("tool\rattack"),
		cli.WithDocumentation("https://example.com/\u202e"),
		cli.WithOptions(cli.StringOption("value").Description("option\u202e")),
	))
	if err != nil {
		t.Fatal(err)
	}
	markdown, err := application.Markdown()
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(markdown, "\u202e\x1b\r") {
		t.Fatalf("Markdown retained terminal controls: %q", markdown)
	}
}

func TestManifestRejectsANilApplication(t *testing.T) {
	t.Parallel()

	var application *cli.Application
	if _, err := application.ManifestJSON(); !errors.Is(err, cli.ErrInternal) {
		t.Fatalf("ManifestJSON() error = %v, want internal error", err)
	}
}

func TestEnumContractsAreValidatedAndPublished(t *testing.T) {
	t.Parallel()

	for _, root := range []*cli.Command{
		cli.NewCommand("tool", cli.WithArguments(cli.EnumArgument("mode"))),
		cli.NewCommand("tool", cli.WithOptions(cli.EnumOption("mode"))),
		cli.NewCommand("tool", cli.WithOptions(cli.EnumOption("mode", "safe", "safe"))),
		cli.NewCommand("tool", cli.WithOptions(
			cli.EnumOption("mode", "safe", "fast").Default("unsafe"),
		)),
		cli.NewCommand("tool", cli.WithOptions(cli.EnumOption("mode", "safe\u202efast"))),
		cli.NewCommand("tool", cli.WithOptions(cli.EnumOption("mode", string([]byte{0xff})))),
		cli.NewCommand("tool", cli.WithArguments(cli.TimeArgument("when", ""))),
		cli.NewCommand("tool", cli.WithOptions(cli.TimeOption("when", "bad\nlayout"))),
		cli.NewCommand("tool", cli.WithOptions(cli.TypedOption[string]("value", "", func(value string) (string, error) {
			return value, nil
		}))),
	} {
		if _, err := cli.Compile(root); !errors.Is(err, cli.ErrInternal) {
			t.Fatalf("Compile() error = %v, want invalid enum rejection", err)
		}
	}

	mode := cli.EnumOption("mode", "safe", "fast")
	secretMode := cli.EnumOption("secret-mode", "token-one", "token-two").Secret()
	target := cli.EnumArgument("target", "local", "remote")
	when := cli.TimeArgument("when", time.RFC3339)
	deadline := cli.TimeOption("deadline", time.RFC1123)
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithSubcommands(cli.NewCommand(
			"run",
			cli.WithOptions(mode, secretMode, deadline),
			cli.WithArguments(target, when),
		)),
	))
	if err != nil {
		t.Fatal(err)
	}
	var manifest cli.Manifest
	encoded, err := application.ManifestJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	if got := manifest.Commands[0].Options[0].AllowedValues; !reflect.DeepEqual(got, []string{"safe", "fast"}) {
		t.Fatalf("manifest option allowed values = %v", got)
	}
	if got := manifest.Commands[0].Arguments[0].AllowedValues; !reflect.DeepEqual(got, []string{"local", "remote"}) {
		t.Fatalf("manifest argument allowed values = %v", got)
	}
	if got := manifest.Commands[0].Arguments[1].Format; got != time.RFC3339 {
		t.Fatalf("manifest argument format = %q", got)
	}
	if got := manifest.Commands[0].Options[2].Format; got != time.RFC1123 {
		t.Fatalf("manifest option format = %q", got)
	}
	if got := manifest.Commands[0].Options[1].AllowedValues; len(got) != 0 {
		t.Fatalf("secret manifest allowed values = %v, want redacted", got)
	}
	metadata := application.Root().Children()[0].Options()[0]
	if got := metadata.AllowedValues(); !reflect.DeepEqual(got, []string{"safe", "fast"}) {
		t.Fatalf("metadata allowed values = %v", got)
	}
	secretMetadata := application.Root().Children()[0].Options()[1]
	if got := secretMetadata.AllowedValues(); len(got) != 0 {
		t.Fatalf("secret metadata allowed values = %v, want redacted", got)
	}
	if got := application.Root().Children()[0].Options()[2].Format(); got != time.RFC1123 {
		t.Fatalf("metadata format = %q", got)
	}
	markdown, err := application.Markdown()
	if err != nil || !strings.Contains(markdown, "format `"+time.RFC3339+"`") ||
		!strings.Contains(markdown, "format `"+time.RFC1123+"`") {
		t.Fatalf("time formats missing from Markdown: %v\n%s", err, markdown)
	}
}

func TestMarkdownPublishesAllContractMetadata(t *testing.T) {
	t.Parallel()

	identity := cli.StringArgument("identity").Secret().Description("account identity")
	region := cli.StringArgument("region").Optional()
	extra := cli.StringsArgument("extra").Remainder()
	mode := cli.EnumOption("mode", "safe", "fast").Short('m').Persistent().
		Required().Default("safe").Description("execution mode")
	token := cli.StringOption("token").Secret()
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithSummary("summary"),
		cli.WithDescription("description"),
		cli.WithHidden(true),
		cli.WithExperimental(true),
		cli.WithDeprecated("use a replacement"),
		cli.WithReplacement("new`tool"),
		cli.WithArguments(identity, region, extra),
		cli.WithOptions(mode, token),
	))
	if err != nil {
		t.Fatal(err)
	}
	markdown, err := application.Markdown()
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"description", "Experimental", "Hidden", "Deprecated",
		"<code>new`tool</code>", "`identity` (`string`, required, secret)",
		"`region` (`string`, optional)", "`extra` (`string-slice`, remainder)",
		"`-m`, `--mode` (`enum`, required, defaulted, persistent)",
		"`--token` (`string`, secret)",
		"Allowed values: `safe`, `fast`.",
	} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("Markdown does not contain %q:\n%s", expected, markdown)
		}
	}

	backtickApplication, err := cli.Compile(cli.NewCommand("to`ol"))
	if err != nil {
		t.Fatal(err)
	}
	markdown, err = backtickApplication.Markdown()
	if err != nil || !strings.Contains(markdown, "<code>to`ol</code>") {
		t.Fatalf("backtick command Markdown = %q, error = %v", markdown, err)
	}
}

func TestManifestPublishesTheRootCommandContract(t *testing.T) {
	t.Parallel()

	verbose := cli.BoolOption("verbose").Description("diagnostics")
	target := cli.StringArgument("target").Description("target name")
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithSummary("root summary"),
		cli.WithOptions(verbose),
		cli.WithArguments(target),
	))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := application.ManifestJSON()
	if err != nil {
		t.Fatal(err)
	}
	var manifest cli.Manifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Root.Path != "tool" || manifest.Root.Summary != "root summary" ||
		len(manifest.Root.Arguments) != 1 || len(manifest.Root.Options) != 1 {
		t.Fatalf("manifest root = %#v, want complete root command", manifest.Root)
	}
}

func TestHelpIncludesCompatibilityMetadataAndHonorsWidth(t *testing.T) {
	t.Parallel()

	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithSubcommands(cli.NewCommand(
			"legacy",
			cli.WithSummary("A legacy command with deliberately long explanatory text."),
			cli.WithExperimental(true),
			cli.WithDeprecated("scheduled for removal"),
			cli.WithReplacement("tool modern"),
		)),
	))
	if err != nil {
		t.Fatal(err)
	}

	help, err := application.Help([]string{"legacy"}, cli.HelpOptions{Width: 40})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Experimental: yes",
		"Deprecated: scheduled for removal",
		"Replacement: tool modern",
	} {
		if !strings.Contains(help, expected) {
			t.Fatalf("help does not contain %q:\n%s", expected, help)
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(help, "\n"), "\n") {
		if len([]rune(line)) > 40 {
			t.Fatalf("line exceeds requested width: %q", line)
		}
	}
}

func TestCompletionGenerationIsDeterministicAndConcurrentSafe(t *testing.T) {
	t.Parallel()

	application := generationApplication(t)
	shells := []cli.Shell{cli.ShellBash, cli.ShellZsh, cli.ShellFish, cli.ShellPowerShell}
	var wait sync.WaitGroup
	for _, shell := range shells {
		shell := shell
		wait.Add(1)
		go func() {
			defer wait.Done()
			first, err := application.Completion(shell)
			if err != nil {
				t.Errorf("generate %s completion: %v", shell, err)
				return
			}
			second, err := application.Completion(shell)
			if err != nil {
				t.Errorf("regenerate %s completion: %v", shell, err)
				return
			}
			if first != second {
				t.Errorf("%s completion is not stable", shell)
			}
			if !strings.Contains(first, "tool") {
				t.Errorf("%s completion lacks the executable name", shell)
			}
		}()
	}
	wait.Wait()
}

func TestCompletionScriptsNeverEvaluateShellInput(t *testing.T) {
	t.Parallel()

	application := generationApplication(t)
	for _, shell := range []cli.Shell{
		cli.ShellBash, cli.ShellZsh, cli.ShellFish, cli.ShellPowerShell,
	} {
		script, err := application.Completion(shell)
		if err != nil {
			t.Fatal(err)
		}
		for _, unsafe := range []string{"eval", "Invoke-Expression"} {
			if strings.Contains(script, unsafe) {
				t.Fatalf("%s completion contains unsafe %q", shell, unsafe)
			}
		}
	}
}

func TestBashCompletionTreatsTokensAndCandidatesLiterally(t *testing.T) {
	t.Parallel()

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}
	directory := t.TempDir()
	tool := filepath.Join(directory, "tool")
	// #nosec G306 -- this fixture must be executable to test shell completion.
	if err := os.WriteFile(tool, []byte(`#!/usr/bin/env bash
printf '%s\n' '$(touch completion-injected)'
printf '%s\n' ':4'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	script, err := generationApplication(t).Completion(cli.ShellBash)
	if err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(directory, "tool.bash")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	// #nosec G204 -- bash is resolved with LookPath and inputs are fixed fixtures.
	command := exec.CommandContext(t.Context(), bash, "-c", `
source "$COMPLETION_SCRIPT"
COMP_WORDS=(tool '$(touch input-injected)')
COMP_CWORD=1
declaration=$(complete -p tool)
function_name=${declaration#*-F }
function_name=${function_name%% *}
"$function_name"
printf '%s' "${COMPREPLY[0]}"
`)
	command.Dir = directory
	command.Env = append(os.Environ(),
		"COMPLETION_SCRIPT="+scriptPath,
		"PATH="+directory+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run Bash completion: %v\n%s", err, output)
	}
	if got := string(output); got != "$(touch completion-injected)" {
		t.Fatalf("literal candidate = %q", got)
	}
	for _, marker := range []string{"input-injected", "completion-injected"} {
		if _, err := os.Stat(filepath.Join(directory, marker)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("completion executed %s", marker)
		}
	}
}

func generationApplication(t *testing.T) *cli.Application {
	t.Helper()
	verbose := cli.BoolOption("verbose").
		Short('v').
		Persistent().
		Description("enable diagnostic output")
	force := cli.BoolOption("force").
		Short('f').
		Description("replace an existing deployment")
	target := cli.StringArgument("target").Description("deployment target")
	extra := cli.StringsArgument("extra").Description("additional deployment arguments")
	application, err := cli.Compile(cli.NewCommand(
		"tool",
		cli.WithVersion("1.2.3"),
		cli.WithSummary("Test deployment tool"),
		cli.WithOptions(verbose),
		cli.WithSubcommands(cli.NewCommand(
			"deploy",
			cli.WithAliases("ship"),
			cli.WithSummary("Deploy an application."),
			cli.WithExamples("tool deploy production"),
			cli.WithDocumentation("https://example.com/deploy"),
			cli.WithArguments(target, extra),
			cli.WithOptions(force),
		)),
	))
	if err != nil {
		t.Fatalf("compile generation application: %v", err)
	}

	return application
}
