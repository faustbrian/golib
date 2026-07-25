package prompts

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
)

// Role identifies semantic meaning independently of terminal styling.
type Role string

const (
	RoleLabel    Role = "label"
	RoleValue    Role = "value"
	RoleHint     Role = "hint"
	RoleHelp     Role = "help"
	RoleError    Role = "error"
	RoleSuccess  Role = "success"
	RoleWarning  Role = "warning"
	RoleFocus    Role = "focus"
	RoleSelected Role = "selected"
	RoleDisabled Role = "disabled"
	RoleProgress Role = "progress"
)

// Segment is a semantic unit of untrusted caller-facing text.
type Segment struct {
	Role    Role
	Content string
	target  string
}

// Text creates a semantic text segment.
func Text(role Role, content string) Segment {
	return Segment{Role: role, Content: content}
}

// Hyperlink creates a semantic link with a safe absolute HTTP, HTTPS, or
// mailto target. Unsupported or control-bearing targets are rejected.
func Hyperlink(role Role, content, target string) (Segment, error) {
	parsed, err := url.ParseRequestURI(target)
	if content == "" || err != nil || !safeLinkTarget(parsed, target) {
		return Segment{}, invalidBehaviorDefinition("define hyperlink", "", ErrInvalidDefinition)
	}

	return Segment{Role: role, Content: content, target: parsed.String()}, nil
}

// Target returns the link target, or an empty string for ordinary text.
func (segment Segment) Target() string { return segment.target }

func safeLinkTarget(parsed *url.URL, target string) bool {
	for _, char := range target {
		if unicode.IsControl(char) || isBidiControl(char) {
			return false
		}
	}
	if parsed.User != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return parsed.Host != ""
	case "mailto":
		return parsed.Opaque != ""
	default:
		return false
	}
}

// SemanticLine is a defensively owned sequence of segments.
type SemanticLine struct {
	segments []Segment
}

// Line creates a semantic line and copies the supplied segments.
func Line(segments ...Segment) SemanticLine {
	return SemanticLine{segments: append([]Segment(nil), segments...)}
}

// Segments returns a defensive copy of the line's segments.
func (line SemanticLine) Segments() []Segment {
	return append([]Segment(nil), line.segments...)
}

// Frame is the authoritative semantic screen representation.
type Frame struct {
	lines []SemanticLine
}

// NewFrame creates a semantic frame with deeply copied lines.
func NewFrame(lines ...SemanticLine) Frame {
	owned := make([]SemanticLine, len(lines))
	for index, line := range lines {
		owned[index] = Line(line.segments...)
	}

	return Frame{lines: owned}
}

// Lines returns a deep copy of the semantic lines.
func (frame Frame) Lines() []SemanticLine {
	return NewFrame(frame.lines...).lines
}

// RenderOptions are explicit output capabilities.
type RenderOptions struct {
	Width      int
	Color      ColorProfile
	ASCIIOnly  bool
	Hyperlinks bool
}

// Renderer converts an owned semantic frame into terminal output.
type Renderer interface {
	Render(Frame, RenderOptions) (string, error)
}

// PlainRenderer emits deterministic linear output without terminal controls.
type PlainRenderer struct {
	Theme Theme
}

func (renderer PlainRenderer) Render(frame Frame, options RenderOptions) (string, error) {
	return renderSemantic(frame, options, renderer.Theme.resolved(), false, false)
}

// ANSIRenderer emits capability-limited ANSI styling and falls back to plain
// output when color is unavailable.
type ANSIRenderer struct {
	Theme Theme
}

func (renderer ANSIRenderer) Render(frame Frame, options RenderOptions) (string, error) {
	theme := renderer.Theme.resolved()

	return renderSemantic(frame, options, theme, options.Color != ColorNone, options.Hyperlinks)
}

type renderCell struct {
	text   string
	role   Role
	target string
}

func renderSemantic(
	frame Frame,
	options RenderOptions,
	theme Theme,
	ansiStyles bool,
	hyperlinks bool,
) (string, error) {
	if options.Width < 0 || options.Color > ColorTrueColor {
		return "", &Error{Kind: ErrorRenderer, Operation: "render frame", Cause: ErrRenderer}
	}

	var output strings.Builder
	for _, line := range frame.lines {
		cells := make([]renderCell, 0, len(line.segments)*2)
		for _, segment := range line.segments {
			if marker := theme.Marker(segment.Role); marker != "" {
				cells = appendCells(cells, renderText(marker, options.ASCIIOnly), segment.Role, "")
			}
			content := renderText(segment.Content, options.ASCIIOnly)
			target := segment.target
			if target != "" && !hyperlinks {
				content += " (" + renderText(target, options.ASCIIOnly) + ")"
				target = ""
			}
			cells = appendCells(cells, content, segment.Role, target)
		}
		writeCells(&output, cells, options, theme, ansiStyles, hyperlinks)
	}

	return output.String(), nil
}

func renderText(content string, asciiOnly bool) string {
	content = Sanitize(content)
	if !asciiOnly {
		return content
	}
	var output strings.Builder
	for _, char := range content {
		if char > unicode.MaxASCII {
			fmt.Fprintf(&output, "\\u{%X}", char)
		} else {
			output.WriteRune(char)
		}
	}

	return output.String()
}

func appendCells(cells []renderCell, content string, role Role, target string) []renderCell {
	graphemes := uniseg.NewGraphemes(content)
	for graphemes.Next() {
		cells = append(cells, renderCell{text: graphemes.Str(), role: role, target: target})
	}

	return cells
}

func writeCells(
	output *strings.Builder,
	cells []renderCell,
	options RenderOptions,
	theme Theme,
	ansiStyles bool,
	hyperlinks bool,
) {
	width := 0
	active := Role("")
	activeTarget := ""
	lineStarted := false
	breakLine := func() {
		if hyperlinks && activeTarget != "" {
			output.WriteString("\x1b]8;;\x1b\\")
		}
		if ansiStyles && active != "" {
			output.WriteString("\x1b[0m")
		}
		output.WriteByte('\n')
		width = 0
		active = ""
		activeTarget = ""
		lineStarted = false
	}

	for _, cell := range cells {
		if cell.text == "\n" {
			breakLine()
			continue
		}
		cellWidth := uniseg.StringWidth(cell.text)
		if options.Width > 0 && lineStarted && width+cellWidth > options.Width {
			breakLine()
		}
		if hyperlinks && activeTarget != cell.target {
			if activeTarget != "" {
				output.WriteString("\x1b]8;;\x1b\\")
			}
			if cell.target != "" {
				output.WriteString("\x1b]8;;")
				output.WriteString(cell.target)
				output.WriteString("\x1b\\")
			}
			activeTarget = cell.target
		}
		if ansiStyles && active != cell.role {
			if active != "" {
				output.WriteString("\x1b[0m")
			}
			if prefix := ansiStyle(theme.Style(cell.role), options.Color); prefix != "" {
				output.WriteString(prefix)
				active = cell.role
			} else {
				active = ""
			}
		}
		output.WriteString(cell.text)
		width += cellWidth
		lineStarted = true
	}
	breakLine()
}

// Sanitize neutralizes terminal controls, carriage returns, bidi controls,
// and invalid UTF-8 while preserving line feeds for semantic layout.
func Sanitize(content string) string {
	var output strings.Builder
	for _, char := range content {
		if char == '\n' {
			output.WriteRune(char)
			continue
		}
		if unicode.IsControl(char) || isBidiControl(char) {
			fmt.Fprintf(&output, "\\u{%X}", char)
			continue
		}
		output.WriteRune(char)
	}

	return output.String()
}

func isBidiControl(char rune) bool {
	return char == '\u061c' || char == '\u200e' || char == '\u200f' ||
		(char >= '\u202a' && char <= '\u202e') ||
		(char >= '\u2066' && char <= '\u2069')
}
