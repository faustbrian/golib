package prompts

import (
	"context"
	"fmt"
	"strings"

	"github.com/rivo/uniseg"
)

// MessageKind selects a textual semantic role.
type MessageKind uint8

const (
	MessageInfo MessageKind = iota
	MessageWarning
	MessageError
	MessageSuccess
)

// Message is a caller-localized note or status message.
type Message struct {
	Kind        MessageKind
	Title, Body string
}

// WriteMessage writes a deterministic semantic note without terminal state.
func WriteMessage(ctx context.Context, message Message, execution Execution) error {
	if message.Kind > MessageSuccess || message.Title == "" {
		return invalidBehaviorDefinition("write message", "message", ErrInvalidDefinition)
	}
	role := RoleValue
	switch message.Kind {
	case MessageInfo:
		role = RoleValue
	case MessageWarning:
		role = RoleWarning
	case MessageError:
		role = RoleError
	case MessageSuccess:
		role = RoleSuccess
	}
	lines := []SemanticLine{Line(Text(role, message.Title))}
	if message.Body != "" {
		for line := range strings.SplitSeq(message.Body, "\n") {
			lines = append(lines, Line(Text(RoleValue, line)))
		}
	}
	return renderOutput(ctx, "message", NewFrame(lines...), execution)
}

// Table is a bounded rectangular display model.
type Table struct {
	Headers             []string
	Rows                [][]string
	MaxRows, MaxColumns int
}

// WriteTable renders a deterministic Unicode-width-aligned linear table.
func WriteTable(ctx context.Context, table Table, execution Execution) error {
	asciiOnly := !execution.Capabilities.Unicode
	maximumRows, maximumColumns := table.MaxRows, table.MaxColumns
	if maximumRows == 0 {
		maximumRows = 1_000
	}
	if maximumColumns == 0 {
		maximumColumns = 50
	}
	if len(table.Headers) == 0 || maximumRows < 1 || maximumColumns < 1 ||
		len(table.Headers) > maximumColumns || len(table.Rows) > maximumRows {
		return invalidBehaviorDefinition("write table", "table", ErrInvalidDefinition)
	}
	columns := len(table.Headers)
	widths := make([]int, columns)
	for index, header := range table.Headers {
		widths[index] = uniseg.StringWidth(renderText(header, asciiOnly))
	}
	for _, row := range table.Rows {
		if len(row) != columns {
			return invalidBehaviorDefinition("write table", "table", ErrInvalidDefinition)
		}
		for index, value := range row {
			widths[index] = max(widths[index], uniseg.StringWidth(renderText(value, asciiOnly)))
		}
	}
	lines := make([]SemanticLine, 0, len(table.Rows)+1)
	lines = append(lines, tableLine(table.Headers, widths, RoleLabel, asciiOnly))
	for _, row := range table.Rows {
		lines = append(lines, tableLine(row, widths, RoleValue, asciiOnly))
	}
	return renderOutput(ctx, "table", NewFrame(lines...), execution)
}

func tableLine(values []string, widths []int, role Role, asciiOnly bool) SemanticLine {
	var output strings.Builder
	output.WriteString("| ")
	for index, value := range values {
		value = renderText(value, asciiOnly)
		output.WriteString(value)
		output.WriteString(strings.Repeat(" ", widths[index]-uniseg.StringWidth(value)))
		output.WriteString(" |")
		if index+1 < len(values) {
			output.WriteByte(' ')
		}
	}
	return Line(Text(role, output.String()))
}

// KeyValue is one declaration-ordered summary entry.
type KeyValue struct {
	Key, Value string
}

// WriteSummary renders declaration-order key/value lines.
func WriteSummary(ctx context.Context, values []KeyValue, execution Execution) error {
	if len(values) == 0 {
		return invalidBehaviorDefinition("write summary", "summary", ErrInvalidDefinition)
	}
	lines := make([]SemanticLine, 0, len(values))
	for _, value := range values {
		if value.Key == "" {
			return invalidBehaviorDefinition("write summary", "summary", fmt.Errorf("%w: summary key is required", ErrInvalidDefinition))
		}
		lines = append(lines, Line(Text(RoleLabel, value.Key+": "), Text(RoleValue, value.Value)))
	}
	return renderOutput(ctx, "summary", NewFrame(lines...), execution)
}
