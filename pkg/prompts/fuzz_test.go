package prompts_test

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func FuzzSanitizeNeutralizesTerminalControls(fuzz *testing.F) {
	fuzz.Add("plain")
	fuzz.Add("\x1b[31mred\x1b[0m\r\u202e")
	fuzz.Add("emoji 👩‍💻 and e\u0301")
	fuzz.Fuzz(func(t *testing.T, input string) {
		output := prompts.Sanitize(input)
		if !utf8.ValidString(output) {
			t.Fatal("Sanitize() returned invalid UTF-8")
		}
		for _, value := range output {
			if value != '\n' && unicode.IsControl(value) {
				t.Fatalf("Sanitize() retained control U+%04X", value)
			}
		}
	})
}

func FuzzPlainRenderingIsDeterministic(fuzz *testing.F) {
	fuzz.Add("label", "value", uint8(20))
	fuzz.Add("\x1b[2J", "界e\u0301", uint8(1))
	fuzz.Fuzz(func(t *testing.T, label, value string, rawWidth uint8) {
		width := int(rawWidth % 80)
		frame := prompts.NewFrame(prompts.Line(prompts.Text(prompts.RoleLabel, label), prompts.Text(prompts.RoleValue, value)))
		first, err := (prompts.PlainRenderer{}).Render(frame, prompts.RenderOptions{Width: width})
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}
		second, err := (prompts.PlainRenderer{}).Render(frame, prompts.RenderOptions{Width: width})
		if err != nil || first != second {
			t.Fatalf("Render() was nondeterministic: %q, %q, %v", first, second, err)
		}
		if strings.Contains(first, "\x1b") {
			t.Fatalf("plain output retained ESC: %q", first)
		}
	})
}

func FuzzHyperlinkNeverInjectsTerminalControls(fuzz *testing.F) {
	fuzz.Add("label", "https://example.com/guide")
	fuzz.Add("\x1b]8;;unsafe", "javascript:alert(1)")
	fuzz.Fuzz(func(t *testing.T, label, target string) {
		if len(label) > 1024 || len(target) > 1024 {
			t.Skip()
		}
		link, err := prompts.Hyperlink(prompts.RoleValue, label, target)
		if err != nil {
			if !errors.Is(err, prompts.ErrInvalidDefinition) {
				t.Fatalf("Hyperlink() error = %v", err)
			}

			return
		}
		output, err := (prompts.ANSIRenderer{}).Render(
			prompts.NewFrame(prompts.Line(link)), prompts.RenderOptions{Hyperlinks: true},
		)
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}
		owned := "\x1b]8;;" + link.Target() + "\x1b\\"
		output = strings.Replace(output, owned, "", 1)
		output = strings.Replace(output, "\x1b]8;;\x1b\\", "", 1)
		if strings.ContainsRune(output, '\x1b') {
			t.Fatalf("hyperlink output retained unowned ESC: %q", output)
		}
	})
}

func FuzzDecoderBoundsArbitraryBytes(fuzz *testing.F) {
	fuzz.Add([]byte("text\x1b[A"), uint8(3), false)
	fuzz.Add([]byte("\x1b[200~paste\n\x1b[201~"), uint8(7), true)
	fuzz.Add([]byte{0xff, 0x00, 0x1b}, uint8(1), false)
	fuzz.Fuzz(func(t *testing.T, input []byte, rawSplit uint8, byteInput bool) {
		decoder, err := prompts.NewDecoder(prompts.DecoderConfig{
			MaxPasteBytes: 256, MaxBufferBytes: 512, ByteInput: byteInput,
		})
		if err != nil {
			t.Fatalf("NewDecoder() error = %v", err)
		}
		split := 0
		if len(input) > 0 {
			split = int(rawSplit) % (len(input) + 1)
		}
		for _, chunk := range [][]byte{input[:split], input[split:]} {
			events, feedErr := decoder.Feed(chunk)
			if feedErr != nil {
				if !errors.Is(feedErr, prompts.ErrReader) {
					t.Fatalf("Feed() error = %v", feedErr)
				}
				continue
			}
			for index := range events {
				event := &events[index]
				if fmt.Sprint(event) != "[INPUT EVENT]" {
					t.Fatalf("event formatting = %q", fmt.Sprint(event))
				}
				event.Destroy()
			}
		}
		if _, flushErr := decoder.Flush(); flushErr != nil && !errors.Is(flushErr, prompts.ErrReader) {
			t.Fatalf("Flush() error = %v", flushErr)
		}
	})
}

func FuzzInteractiveSecretBytesNeverRendersInput(fuzz *testing.F) {
	fuzz.Add([]byte("token"))
	fuzz.Add([]byte{0, 1, 2, 0xff})
	fuzz.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 512 {
			t.Skip()
		}
		canary := []byte("secret-" + hex.EncodeToString(raw))
		prompt, err := prompts.NewSecretBytesPrompt(prompts.SecretBytesConfig{
			ID: "token", Label: "Token", Class: prompts.SecretToken,
		})
		if err != nil {
			t.Fatalf("NewSecretBytesPrompt() error = %v", err)
		}
		terminal := prompts.NewVirtualTerminal(40, 8)
		terminal.Push(prompts.PasteBytesEvent(canary), prompts.KeyEvent(prompts.KeyEnter))
		result, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
		if err != nil && !errors.Is(err, prompts.ErrReader) {
			t.Fatalf("Run() error = %v", err)
		}
		if result != nil {
			result.Destroy()
		}
		if strings.Contains(terminal.Output(), string(canary)) {
			t.Fatalf("secret appeared in output: %q", terminal.Output())
		}
	})
}

func FuzzInteractiveNormalizedEventSequence(fuzz *testing.F) {
	fuzz.Add([]byte{0, 1, 2, 3, 14})
	fuzz.Add([]byte{8, 9, 10, 11, 12, 13, 14})
	fuzz.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 512 {
			t.Skip()
		}
		prompt, err := prompts.NewText(prompts.TextConfig{ID: "text", Label: "Text"})
		if err != nil {
			t.Fatal(err)
		}
		terminal := prompts.NewVirtualTerminal(40, 8)
		events := make([]prompts.InputEvent, 0, len(raw)+1)
		for _, value := range raw {
			switch value % 15 {
			case 0:
				events = append(events, prompts.RuneEvent(rune('a'+value%26)))
			case 1:
				events = append(events, prompts.PasteEvent("e\u0301"))
			case 2:
				events = append(events, prompts.KeyEvent(prompts.KeyBackspace))
			case 3:
				events = append(events, prompts.KeyEvent(prompts.KeyDelete))
			case 4:
				events = append(events, prompts.KeyEvent(prompts.KeyLeft))
			case 5:
				events = append(events, prompts.KeyEvent(prompts.KeyRight))
			case 6:
				events = append(events, prompts.KeyEvent(prompts.KeyHome))
			case 7:
				events = append(events, prompts.KeyEvent(prompts.KeyEnd))
			case 8:
				events = append(events, prompts.KeyEvent(prompts.KeyWordLeft))
			case 9:
				events = append(events, prompts.KeyEvent(prompts.KeyWordRight))
			case 10:
				events = append(events, prompts.KeyEvent(prompts.KeyTab))
			case 11:
				events = append(events, prompts.KeyEvent(prompts.KeyIgnored))
			case 12:
				events = append(events, prompts.ResizeEvent(int(value%80), int(value%24)))
			case 13:
				events = append(events, prompts.KeyEvent(prompts.KeyCtrlD))
			case 14:
				events = append(events, prompts.KeyEvent(prompts.KeyEnter))
			}
		}
		events = append(events, prompts.KeyEvent(prompts.KeyEnter))
		if err := terminal.Push(events...); err != nil {
			t.Fatalf("Push() error = %v", err)
		}
		terminal.CloseInput()
		_, err = prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
		if err != nil && !errors.Is(err, prompts.ErrEndOfInput) &&
			!errors.Is(err, prompts.ErrReader) {
			t.Fatalf("Run() error = %v", err)
		}
	})
}

func FuzzSearchOrderingIsDeterministic(fuzz *testing.F) {
	fuzz.Add("a")
	fuzz.Add("production remote")
	fuzz.Add("界")
	options := selectionOptionsForFuzz()
	fuzz.Fuzz(func(t *testing.T, query string) {
		if utf8.RuneCountInString(query) > 64 {
			t.Skip()
		}
		policy := prompts.SearchPolicy{MaxOptions: 8, MaxResults: 8, MaxQueryRunes: 64}
		first, err := prompts.Search(options, query, policy)
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		second, err := prompts.Search(options, query, policy)
		if err != nil || !equalStrings(optionIDs(first), optionIDs(second)) {
			t.Fatalf("Search() was nondeterministic: %q, %q, %v", optionIDs(first), optionIDs(second), err)
		}
	})
}

func FuzzInteractiveSecretNeverRendersInput(fuzz *testing.F) {
	fuzz.Add([]byte("token"))
	fuzz.Add([]byte{0, 1, 2, 0xff})
	fuzz.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 512 {
			t.Skip()
		}
		canary := "secret-" + hex.EncodeToString(raw)
		prompt, err := prompts.NewSecret(prompts.SecretConfig{ID: "token", Label: "Token", Class: prompts.SecretToken})
		if err != nil {
			t.Fatalf("NewSecret() error = %v", err)
		}
		terminal := prompts.NewVirtualTerminal(40, 8)
		terminal.Push(prompts.PasteEvent(canary), prompts.KeyEvent(prompts.KeyEnter))
		_, err = prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
		if err != nil && !errors.Is(err, prompts.ErrReader) {
			t.Fatalf("Run() error = %v", err)
		}
		if strings.Contains(terminal.Output(), canary) {
			t.Fatalf("secret appeared in output: %q", terminal.Output())
		}
	})
}

func FuzzSecretEntryFailureNeverDiscloses(fuzz *testing.F) {
	fuzz.Add([]byte("token"), uint8(0))
	fuzz.Add([]byte{0, 1, 2, 0xff}, uint8(5))
	fuzz.Fuzz(func(t *testing.T, raw []byte, outcome uint8) {
		if len(raw) > 512 {
			t.Skip()
		}
		canary := "secret-failure-" + hex.EncodeToString(raw)
		prompt, err := prompts.NewSecret(prompts.SecretConfig{
			ID: "token", Label: "Token", Class: prompts.SecretToken,
		})
		if err != nil {
			t.Fatal(err)
		}
		terminal := prompts.NewVirtualTerminal(40, 8)
		events := []prompts.InputEvent{prompts.PasteEvent(canary)}
		allowed := []error{nil, prompts.ErrCanceled, prompts.ErrEndOfInput,
			prompts.ErrTerminalDetached, prompts.ErrReader}
		switch outcome % 7 {
		case 0:
			events = append(events, prompts.KeyEvent(prompts.KeyEnter))
		case 1:
			events = append(events, prompts.KeyEvent(prompts.KeyEscape))
		case 2:
			events = append(events, prompts.KeyEvent(prompts.KeyCtrlC))
		case 3:
			events = append(events, prompts.KeyEvent(prompts.KeyCtrlD))
		case 4:
			events = append(events, prompts.InputEvent{Kind: prompts.EventEOF})
		case 5:
			events = append(events, prompts.InputEvent{Kind: prompts.EventDetached})
		case 6:
			events = append(events, prompts.ResizeEvent(-1, -1))
		}
		if err := terminal.Push(events...); err != nil {
			t.Fatalf("Push() error = %v", err)
		}
		_, runErr := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
		matched := false
		for _, candidate := range allowed {
			if candidate == nil && runErr == nil || candidate != nil && errors.Is(runErr, candidate) {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("Run() error = %v", runErr)
		}
		if strings.Contains(terminal.Output(), canary) {
			t.Fatalf("secret appeared in output: %q", terminal.Output())
		}
		if !terminal.Released() || !terminal.EchoEnabled() {
			t.Fatalf("terminal was not restored: released=%t echo=%t",
				terminal.Released(), terminal.EchoEnabled())
		}
	})
}

func selectionOptionsForFuzz() []prompts.Option[string] {
	configs := []prompts.OptionConfig[string]{
		{ID: "alpha", Label: "Alpha", Description: "first", Value: "a"},
		{ID: "prod", Label: "Production", Description: "remote", Value: "p"},
		{ID: "unicode", Label: "界", Description: "wide", Value: "u"},
	}
	options := make([]prompts.Option[string], 0, len(configs))
	for _, config := range configs {
		option, err := prompts.NewOption(config)
		if err != nil {
			panic(err)
		}
		options = append(options, option)
	}
	return options
}
