package prompts

import (
	"fmt"
	"maps"
	"strconv"
	"strings"
)

type colorKind uint8

const (
	colorDefault colorKind = iota
	colorANSI
	colorRGB
)

// Color is a semantic terminal color value.
type Color struct {
	kind       colorKind
	index      uint8
	red, green uint8
	blue       uint8
}

// ANSI creates an indexed ANSI color.
func ANSI(index uint8) Color {
	return Color{kind: colorANSI, index: index}
}

// RGB creates a true-color value that renderers downsample when required.
func RGB(red, green, blue uint8) Color {
	return Color{kind: colorRGB, red: red, green: green, blue: blue}
}

// Style assigns visual attributes to one semantic role.
type Style struct {
	Foreground Color
	Bold       bool
	Underline  bool
	Dim        bool
}

// Theme is an immutable role-to-style and role-to-text-marker mapping.
type Theme struct {
	styles  map[Role]Style
	markers map[Role]string
}

// DefaultTheme returns the sober color-independent default theme.
func DefaultTheme() Theme {
	return Theme{
		styles: map[Role]Style{
			RoleLabel:    {Bold: true},
			RoleHint:     {Dim: true},
			RoleHelp:     {Dim: true},
			RoleError:    {Foreground: ANSI(1), Bold: true},
			RoleSuccess:  {Foreground: ANSI(2), Bold: true},
			RoleWarning:  {Foreground: ANSI(3), Bold: true},
			RoleFocus:    {Foreground: ANSI(6), Bold: true},
			RoleSelected: {Foreground: ANSI(2)},
			RoleDisabled: {Foreground: ANSI(8), Dim: true},
			RoleProgress: {Foreground: ANSI(4)},
		},
		markers: map[Role]string{
			RoleHint:     "hint: ",
			RoleHelp:     "help: ",
			RoleError:    "error: ",
			RoleSuccess:  "success: ",
			RoleWarning:  "warning: ",
			RoleFocus:    "> ",
			RoleSelected: "[x] ",
			RoleDisabled: "[disabled] ",
			RoleProgress: "progress: ",
		},
	}
}

// With returns a copy with one role's style replaced.
func (theme Theme) With(role Role, style Style) Theme {
	copyTheme := theme.resolved().clone()
	copyTheme.styles[role] = style

	return copyTheme
}

// WithMarker returns a copy with one role's textual marker replaced.
func (theme Theme) WithMarker(role Role, marker string) Theme {
	copyTheme := theme.resolved().clone()
	copyTheme.markers[role] = Sanitize(marker)

	return copyTheme
}

// Style returns the role's style or the zero style for an unknown role.
func (theme Theme) Style(role Role) Style {
	return theme.styles[role]
}

// Marker returns the role's color-independent textual marker.
func (theme Theme) Marker(role Role) string {
	return theme.markers[role]
}

func (theme Theme) resolved() Theme {
	if theme.styles == nil && theme.markers == nil {
		return DefaultTheme()
	}

	return theme
}

func (theme Theme) clone() Theme {
	styles := make(map[Role]Style, len(theme.styles))
	maps.Copy(styles, theme.styles)
	markers := make(map[Role]string, len(theme.markers))
	maps.Copy(markers, theme.markers)

	return Theme{styles: styles, markers: markers}
}

func ansiStyle(style Style, profile ColorProfile) string {
	codes := make([]string, 0, 4)
	if style.Bold {
		codes = append(codes, "1")
	}
	if style.Dim {
		codes = append(codes, "2")
	}
	if style.Underline {
		codes = append(codes, "4")
	}
	if color := ansiColor(style.Foreground, profile); color != "" {
		codes = append(codes, color)
	}
	if len(codes) == 0 {
		return ""
	}

	return "\x1b[" + strings.Join(codes, ";") + "m"
}

func ansiColor(color Color, profile ColorProfile) string {
	if color.kind == colorDefault || profile == ColorNone {
		return ""
	}
	if color.kind == colorANSI {
		index := color.index
		if profile == ColorANSI16 {
			index %= 16
			if index < 8 {
				return strconv.FormatUint(uint64(30+index), 10)
			}

			return strconv.FormatUint(uint64(90+index-8), 10)
		}

		return fmt.Sprintf("38;5;%d", index)
	}
	if profile == ColorTrueColor {
		return fmt.Sprintf("38;2;%d;%d;%d", color.red, color.green, color.blue)
	}
	if profile == ColorANSI256 {
		return fmt.Sprintf("38;5;%d", rgbToANSI256(color.red, color.green, color.blue))
	}

	return strconv.FormatUint(uint64(30+rgbToANSI16(color.red, color.green, color.blue)), 10)
}

func rgbToANSI256(red, green, blue uint8) uint8 {
	// Each scaled channel is proven to be in [0,5].
	return 16 + 36*uint8((uint16(red)*5+127)/255) + //nolint:gosec
		6*uint8((uint16(green)*5+127)/255) + uint8((uint16(blue)*5+127)/255) //nolint:gosec
}

func rgbToANSI16(red, green, blue uint8) uint8 {
	var index uint8
	if red >= 128 {
		index |= 1
	}
	if green >= 128 {
		index |= 2
	}
	if blue >= 128 {
		index |= 4
	}

	return index
}
