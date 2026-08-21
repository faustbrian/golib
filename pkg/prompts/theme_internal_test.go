package prompts

import "testing"

func TestThemeResolutionAndANSIExactBoundaries(t *testing.T) {
	t.Parallel()

	partial := Theme{styles: map[Role]Style{RoleValue: {Bold: true}}}
	if resolved := partial.resolved(); !resolved.Style(RoleValue).Bold || resolved.markers != nil {
		t.Fatalf("partial resolved theme = %#v", resolved)
	}
	if got := ansiColor(ANSI(8), ColorANSI16); got != "90" {
		t.Fatalf("ANSI index 8 = %q", got)
	}
	if got := ansiColor(ANSI(24), ColorANSI16); got != "90" {
		t.Fatalf("wrapped ANSI index = %q", got)
	}
	if got := rgbToANSI256(255, 128, 0); got != 214 {
		t.Fatalf("RGB ANSI256 = %d", got)
	}
	for name, test := range map[string]struct {
		red, green, blue uint8
		want             uint8
	}{
		"green": {0, 255, 0, 46},
		"blue":  {0, 0, 255, 21},
		"white": {255, 255, 255, 231},
	} {
		if got := rgbToANSI256(test.red, test.green, test.blue); got != test.want {
			t.Fatalf("%s RGB ANSI256 = %d", name, got)
		}
	}
	for name, test := range map[string]struct {
		red, green, blue uint8
		want             uint8
	}{
		"below": {127, 127, 127, 0},
		"red":   {128, 0, 0, 1},
		"green": {0, 128, 0, 2},
		"blue":  {0, 0, 128, 4},
		"white": {128, 128, 128, 7},
	} {
		if got := rgbToANSI16(test.red, test.green, test.blue); got != test.want {
			t.Fatalf("%s RGB ANSI16 = %d", name, got)
		}
	}
}
