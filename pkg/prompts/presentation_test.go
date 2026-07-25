package prompts_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestMessagesAndNotesPreserveTextualMeaning(t *testing.T) {
	t.Parallel()

	terminal := prompts.NewVirtualTerminal(40, 8)
	execution := prompts.Execution{Output: terminal}
	messages := []struct {
		kind   prompts.MessageKind
		marker string
	}{
		{prompts.MessageInfo, "Note"},
		{prompts.MessageWarning, "warning: Careful"},
		{prompts.MessageError, "error: Failed"},
		{prompts.MessageSuccess, "success: Done"},
	}
	for _, message := range messages {
		if err := prompts.WriteMessage(context.Background(), prompts.Message{
			Kind: message.kind, Title: strings.TrimSuffix(strings.TrimPrefix(message.marker, "warning: "), ""),
			Body: "first\nsecond\x1b[31m",
		}, execution); err != nil {
			t.Fatalf("WriteMessage() error = %v", err)
		}
	}
	output := terminal.Output()
	for _, value := range []string{"Note", "warning:", "error:", "success:", "first", "second\\u{1B}[31m"} {
		if !strings.Contains(output, value) {
			t.Fatalf("message output missing %q: %q", value, output)
		}
	}
}

func TestTableAndSummaryAreBoundedDeterministicAndEscaped(t *testing.T) {
	t.Parallel()

	table := prompts.Table{
		Headers: []string{"Name", "State"},
		Rows:    [][]string{{"alpha", "ready"}, {"界", "bad\rvalue"}},
		MaxRows: 4, MaxColumns: 2,
	}
	terminal := prompts.NewVirtualTerminal(80, 24)
	if err := prompts.WriteTable(context.Background(), table, prompts.Execution{
		Output: terminal, Capabilities: prompts.Capabilities{Unicode: true},
	}); err != nil {
		t.Fatalf("WriteTable() error = %v", err)
	}
	output := terminal.Output()
	for _, value := range []string{"| Name  | State", "| alpha | ready", "| 界", "bad\\u{D}value"} {
		if !strings.Contains(output, value) {
			t.Fatalf("table output missing %q: %q", value, output)
		}
	}

	terminal = prompts.NewVirtualTerminal(80, 24)
	if err := prompts.WriteTable(context.Background(), table, prompts.Execution{Output: terminal}); err != nil {
		t.Fatalf("ASCII WriteTable() error = %v", err)
	}
	if output := terminal.Output(); !strings.Contains(output, "| \\u{754C}") || strings.Contains(output, "界") {
		t.Fatalf("ASCII table output = %q", output)
	}

	terminal = prompts.NewVirtualTerminal(80, 24)
	if err := prompts.WriteSummary(context.Background(), []prompts.KeyValue{
		{Key: "region", Value: "eu"}, {Key: "mode", Value: "safe"},
	}, prompts.Execution{Output: terminal}); err != nil {
		t.Fatalf("WriteSummary() error = %v", err)
	}
	if output := terminal.Output(); !strings.Contains(output, "region: eu") || !strings.Contains(output, "mode: safe") || strings.Index(output, "region") > strings.Index(output, "mode") {
		t.Fatalf("summary output = %q", output)
	}
}

func TestPresentationRejectsInvalidDefinitionsAndPropagatesIO(t *testing.T) {
	t.Parallel()

	invalidTables := []prompts.Table{
		{},
		{Headers: []string{"a"}, Rows: [][]string{{"a", "b"}}},
		{Headers: []string{"a"}, Rows: [][]string{{"a"}}, MaxRows: -1},
		{Headers: []string{"a", "b"}, Rows: [][]string{{"a", "b"}}, MaxColumns: 1},
	}
	for _, table := range invalidTables {
		if err := prompts.WriteTable(context.Background(), table, prompts.Execution{Output: io.Discard}); !errors.Is(err, prompts.ErrInvalidDefinition) {
			t.Fatalf("WriteTable(%#v) error = %v", table, err)
		}
	}
	if err := prompts.WriteSummary(context.Background(), []prompts.KeyValue{{}}, prompts.Execution{Output: io.Discard}); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("invalid summary error = %v", err)
	}
	if err := prompts.WriteSummary(context.Background(), nil, prompts.Execution{Output: io.Discard}); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("empty summary error = %v", err)
	}
	if err := prompts.WriteMessage(context.Background(), prompts.Message{Kind: prompts.MessageKind(200), Title: "bad"}, prompts.Execution{Output: io.Discard}); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("invalid message error = %v", err)
	}
	if err := prompts.WriteMessage(context.Background(), prompts.Message{Kind: prompts.MessageInfo}, prompts.Execution{Output: io.Discard}); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("empty message error = %v", err)
	}
	if err := prompts.WriteMessage(context.Background(), prompts.Message{Kind: prompts.MessageInfo, Title: "note"}, prompts.Execution{Output: &failingWriter{err: io.ErrClosedPipe}}); !errors.Is(err, prompts.ErrWriter) {
		t.Fatalf("message writer error = %v", err)
	}
}
