package prompts_test

import (
	"errors"
	"strings"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestSanitizeNeutralizesTerminalAndBidiControls(t *testing.T) {
	t.Parallel()

	input := "safe\x1b]8;;https://evil.example\x07link\x1b]8;;\x07\rnext\u202Etxt\xff\nline"
	want := "safe\\u{1B}]8;;https://evil.example\\u{7}link\\u{1B}]8;;\\u{7}\\u{D}next\\u{202E}txt�\nline"
	if got := prompts.Sanitize(input); got != want {
		t.Fatalf("Sanitize() = %q, want %q", got, want)
	}
}

func TestPlainRendererPreservesMeaningAndWrapsGraphemes(t *testing.T) {
	t.Parallel()

	frame := prompts.NewFrame(
		prompts.Line(prompts.Text(prompts.RoleLabel, "Account")),
		prompts.Line(prompts.Text(prompts.RoleFocus, "A👩🏽‍💻界B")),
		prompts.Line(prompts.Text(prompts.RoleError, "bad\x1b value")),
		prompts.Line(prompts.Text(prompts.RoleSelected, "Primary")),
		prompts.Line(prompts.Text(prompts.RoleDisabled, "Legacy")),
	)

	got, err := (prompts.PlainRenderer{}).Render(frame, prompts.RenderOptions{Width: 80})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	want := "Account\n> A👩🏽‍💻界B\nerror: bad\\u{1B} value\n[x] Primary\n[disabled] Legacy\n"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
	if strings.ContainsRune(got, '\x1b') {
		t.Fatal("plain rendering emitted ESC")
	}

	wrapped, err := (prompts.PlainRenderer{}).Render(
		prompts.NewFrame(prompts.Line(prompts.Text(prompts.RoleValue, "1234567👩🏽‍💻界B"))),
		prompts.RenderOptions{Width: 8},
	)
	if err != nil {
		t.Fatalf("wrapped Render() error = %v", err)
	}
	if want := "1234567\n👩🏽‍💻界B\n"; wrapped != want {
		t.Fatalf("wrapped Render() = %q, want %q", wrapped, want)
	}
}

func TestPlainRendererProvidesDeterministicASCIIOnlyFallback(t *testing.T) {
	t.Parallel()

	frame := prompts.NewFrame(prompts.Line(prompts.Text(prompts.RoleValue, "界e\u0301👩")))
	got, err := (prompts.PlainRenderer{}).Render(frame, prompts.RenderOptions{ASCIIOnly: true})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if want := "\\u{754C}e\\u{301}\\u{1F469}\n"; got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestANSIRendererIsCapabilityDrivenAndSanitized(t *testing.T) {
	t.Parallel()

	theme := prompts.DefaultTheme().With(prompts.RoleWarning, prompts.Style{
		Foreground: prompts.RGB(255, 128, 0), Bold: true,
	})
	frame := prompts.NewFrame(prompts.Line(prompts.Text(prompts.RoleWarning, "unsafe\x1b warning")))
	renderer := prompts.ANSIRenderer{Theme: theme}

	plain, err := renderer.Render(frame, prompts.RenderOptions{Color: prompts.ColorNone})
	if err != nil {
		t.Fatalf("no-color Render() error = %v", err)
	}
	if plain != "warning: unsafe\\u{1B} warning\n" || strings.ContainsRune(plain, '\x1b') {
		t.Fatalf("no-color Render() = %q", plain)
	}

	ansi, err := renderer.Render(frame, prompts.RenderOptions{Color: prompts.ColorTrueColor})
	if err != nil {
		t.Fatalf("true-color Render() error = %v", err)
	}
	if ansi != "\x1b[1;38;2;255;128;0mwarning: unsafe\\u{1B} warning\x1b[0m\n" {
		t.Fatalf("true-color Render() = %q", ansi)
	}
}

func TestHyperlinksAreCapabilityDrivenAndSafe(t *testing.T) {
	t.Parallel()

	link, err := prompts.Hyperlink(
		prompts.RoleValue, "Documentation", "https://example.com/guide?q=1",
	)
	if err != nil {
		t.Fatalf("Hyperlink() error = %v", err)
	}
	if link.Target() != "https://example.com/guide?q=1" ||
		prompts.Text(prompts.RoleValue, "text").Target() != "" {
		t.Fatalf("link targets = %q and %q", link.Target(), prompts.Text(prompts.RoleValue, "text").Target())
	}
	frame := prompts.NewFrame(prompts.Line(link))
	plain, err := (prompts.PlainRenderer{}).Render(frame, prompts.RenderOptions{Hyperlinks: true})
	if err != nil || plain != "Documentation (https://example.com/guide?q=1)\n" {
		t.Fatalf("plain Render() = %q, %v", plain, err)
	}
	ansi, err := (prompts.ANSIRenderer{}).Render(frame, prompts.RenderOptions{Hyperlinks: true})
	want := "\x1b]8;;https://example.com/guide?q=1\x1b\\Documentation\x1b]8;;\x1b\\\n"
	if err != nil || ansi != want {
		t.Fatalf("ANSI Render() = %q, %v", ansi, err)
	}
	withTail := prompts.NewFrame(prompts.Line(link, prompts.Text(prompts.RoleValue, " tail")))
	ansi, err = (prompts.ANSIRenderer{}).Render(withTail, prompts.RenderOptions{Hyperlinks: true})
	want = "\x1b]8;;https://example.com/guide?q=1\x1b\\Documentation\x1b]8;;\x1b\\ tail\n"
	if err != nil || ansi != want {
		t.Fatalf("tailed ANSI Render() = %q, %v", ansi, err)
	}
	mail, err := prompts.Hyperlink(prompts.RoleValue, "Email", "mailto:help@example.com")
	if err != nil || mail.Target() != "mailto:help@example.com" {
		t.Fatalf("mailto Hyperlink() = %#v, %v", mail, err)
	}

	for _, target := range []string{
		"", "relative/path", "javascript:alert(1)", "https://example.com/\nunsafe",
		"https://example.com/\u202Eunsafe", "https://user@example.com/private",
	} {
		if _, err := prompts.Hyperlink(prompts.RoleValue, "unsafe", target); !errors.Is(err, prompts.ErrInvalidDefinition) {
			t.Fatalf("Hyperlink(%q) error = %v", target, err)
		}
	}
}

func TestThemeCompositionIsImmutable(t *testing.T) {
	t.Parallel()

	base := prompts.DefaultTheme()
	wantStyle := prompts.Style{Foreground: prompts.RGB(1, 2, 3), Underline: true}
	changed := base.With(prompts.RoleSuccess, wantStyle)
	if base.Style(prompts.RoleSuccess) == changed.Style(prompts.RoleSuccess) {
		t.Fatal("With() mutated or failed to change the theme")
	}
	if changed.Style(prompts.RoleSuccess) != wantStyle {
		t.Fatalf("changed style = %#v", changed.Style(prompts.RoleSuccess))
	}
	if base.Style(prompts.Role("invalid")) != (prompts.Style{}) {
		t.Fatal("invalid role returned a non-zero style")
	}
}

func TestSemanticFrameAccessorsReturnCopies(t *testing.T) {
	t.Parallel()

	segments := []prompts.Segment{prompts.Text(prompts.RoleValue, "value")}
	lines := []prompts.SemanticLine{prompts.Line(segments...)}
	frame := prompts.NewFrame(lines...)
	segments[0] = prompts.Text(prompts.RoleError, "mutated")
	lines[0] = prompts.Line(prompts.Text(prompts.RoleError, "mutated"))

	copyLines := frame.Lines()
	copySegments := copyLines[0].Segments()
	copySegments[0] = prompts.Text(prompts.RoleError, "mutated again")
	if got := frame.Lines()[0].Segments()[0]; got.Role != prompts.RoleValue || got.Content != "value" {
		t.Fatalf("frame retained caller or accessor mutation: %#v", got)
	}
}

func TestRendererRejectsInvalidCapabilities(t *testing.T) {
	t.Parallel()

	frame := prompts.NewFrame(prompts.Line(prompts.Text(prompts.RoleValue, "value")))
	for _, options := range []prompts.RenderOptions{
		{Width: -1},
		{Color: prompts.ColorProfile(200)},
	} {
		_, err := (prompts.ANSIRenderer{}).Render(frame, options)
		if !errors.Is(err, prompts.ErrRenderer) {
			t.Fatalf("Render(%#v) error = %v", options, err)
		}
	}
}

func TestRendererHandlesEmptyExplicitAndTinyLines(t *testing.T) {
	t.Parallel()

	renderer := prompts.PlainRenderer{}
	if got, err := renderer.Render(prompts.NewFrame(), prompts.RenderOptions{}); err != nil || got != "" {
		t.Fatalf("empty Render() = %q, %v", got, err)
	}
	frame := prompts.NewFrame(
		prompts.Line(),
		prompts.Line(prompts.Text(prompts.RoleValue, "first\nsecond")),
		prompts.Line(prompts.Text(prompts.RoleValue, "👩🏽‍💻A")),
	)
	got, err := renderer.Render(frame, prompts.RenderOptions{Width: 1})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if want := "\nf\ni\nr\ns\nt\ns\ne\nc\no\nn\nd\n👩🏽‍💻\nA\n"; got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestThemeMarkersAreSanitizedImmutableAndLocalizable(t *testing.T) {
	t.Parallel()

	base := prompts.DefaultTheme()
	localized := base.WithMarker(prompts.RoleError, "fejl\x1b: ")
	if base.Marker(prompts.RoleError) != "error: " {
		t.Fatal("WithMarker() mutated the base theme")
	}
	if localized.Marker(prompts.RoleError) != "fejl\\u{1B}: " {
		t.Fatalf("localized marker = %q", localized.Marker(prompts.RoleError))
	}
	if localized.Marker(prompts.Role("invalid")) != "" {
		t.Fatal("invalid role returned a marker")
	}
}

func TestANSIProfilesAndAttributes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		style   prompts.Style
		profile prompts.ColorProfile
		want    string
	}{
		{"ansi16 dark", prompts.Style{Foreground: prompts.ANSI(1)}, prompts.ColorANSI16, "\x1b[31mx\x1b[0m\n"},
		{"ansi16 bright", prompts.Style{Foreground: prompts.ANSI(9)}, prompts.ColorANSI16, "\x1b[91mx\x1b[0m\n"},
		{"ansi256 indexed", prompts.Style{Foreground: prompts.ANSI(42)}, prompts.ColorANSI256, "\x1b[38;5;42mx\x1b[0m\n"},
		{"truecolor indexed", prompts.Style{Foreground: prompts.ANSI(42)}, prompts.ColorTrueColor, "\x1b[38;5;42mx\x1b[0m\n"},
		{"ansi256 rgb", prompts.Style{Foreground: prompts.RGB(255, 128, 0)}, prompts.ColorANSI256, "\x1b[38;5;214mx\x1b[0m\n"},
		{"ansi16 rgb", prompts.Style{Foreground: prompts.RGB(255, 128, 0)}, prompts.ColorANSI16, "\x1b[33mx\x1b[0m\n"},
		{"ansi16 blue", prompts.Style{Foreground: prompts.RGB(0, 0, 255)}, prompts.ColorANSI16, "\x1b[34mx\x1b[0m\n"},
		{"attributes", prompts.Style{Dim: true, Underline: true}, prompts.ColorTrueColor, "\x1b[2;4mx\x1b[0m\n"},
		{"default color", prompts.Style{}, prompts.ColorTrueColor, "x\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			theme := prompts.DefaultTheme().With(prompts.RoleValue, test.style)
			got, err := (prompts.ANSIRenderer{Theme: theme}).Render(
				prompts.NewFrame(prompts.Line(prompts.Text(prompts.RoleValue, "x"))),
				prompts.RenderOptions{Color: test.profile},
			)
			if err != nil || got != test.want {
				t.Fatalf("Render() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestANSIStyleTransitionsNewlinesAndWrapping(t *testing.T) {
	t.Parallel()

	theme := prompts.DefaultTheme().
		WithMarker(prompts.RoleLabel, "").
		WithMarker(prompts.RoleValue, "").
		With(prompts.RoleLabel, prompts.Style{Bold: true}).
		With(prompts.RoleValue, prompts.Style{Foreground: prompts.ANSI(1)})
	frame := prompts.NewFrame(
		prompts.Line(
			prompts.Text(prompts.RoleLabel, "L"),
			prompts.Text(prompts.RoleValue, "12\n3456"),
		),
	)
	got, err := (prompts.ANSIRenderer{Theme: theme}).Render(frame, prompts.RenderOptions{
		Width: 3, Color: prompts.ColorANSI16,
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	want := "\x1b[1mL\x1b[0m\x1b[31m12\x1b[0m\n\x1b[31m345\x1b[0m\n\x1b[31m6\x1b[0m\n"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}
