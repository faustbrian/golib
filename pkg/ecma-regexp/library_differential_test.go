package ecmascript_test

import (
	"context"
	"testing"

	"github.com/dlclark/regexp2"
	"github.com/dop251/goja"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestOverlappingLibraryDifferential(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		input   string
	}{
		{pattern: "(a|ab)+c", input: "xxababczz"},
		{pattern: "(?<=a)b", input: "zab"},
		{pattern: "(a)?b\\1", input: "aba"},
		{pattern: "^a.*c$", input: "abbbc"},
		{pattern: "[A-Z]+", input: "42 HELLO"},
		{pattern: "a+?", input: "zaaa"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.pattern, func(t *testing.T) {
			t.Parallel()

			program, err := ecmascript.Compile(
				test.pattern,
				"",
				ecmascript.DefaultCompileOptions(),
			)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			result, matched, err := program.Find(
				context.Background(),
				test.input,
				ecmascript.DefaultMatchOptions(),
			)
			if err != nil {
				t.Fatalf("Find() error = %v", err)
			}
			start := -1
			value := ""
			if matched {
				start = result.Full().Span().Start.Rune
				value = result.Full().Value().LossyString()
			}

			regexp2Pattern, err := regexp2.Compile(
				test.pattern,
				regexp2.ECMAScript,
			)
			if err != nil {
				t.Fatalf("regexp2.Compile() error = %v", err)
			}
			regexp2Match, err := regexp2Pattern.FindStringMatch(test.input)
			if err != nil {
				t.Fatalf("regexp2.FindStringMatch() error = %v", err)
			}
			regexp2Start := -1
			regexp2Value := ""
			if regexp2Match != nil {
				regexp2Start = regexp2Match.Index
				regexp2Value = regexp2Match.String()
			}
			if start != regexp2Start || value != regexp2Value {
				t.Errorf(
					"regexp2 = (%d, %q), engine = (%d, %q)",
					regexp2Start,
					regexp2Value,
					start,
					value,
				)
			}

			vm := goja.New()
			if err := vm.Set("pattern", test.pattern); err != nil {
				t.Fatalf("goja.Set(pattern) error = %v", err)
			}
			if err := vm.Set("input", test.input); err != nil {
				t.Fatalf("goja.Set(input) error = %v", err)
			}
			gojaMatch, err := vm.RunString("new RegExp(pattern).exec(input)")
			if err != nil {
				t.Fatalf("goja RegExp error = %v", err)
			}
			gojaStart := -1
			gojaValue := ""
			if !goja.IsNull(gojaMatch) {
				object := gojaMatch.ToObject(vm)
				gojaStart = int(object.Get("index").ToInteger())
				gojaValue = object.Get("0").String()
			}
			if start != gojaStart || value != gojaValue {
				t.Errorf(
					"goja = (%d, %q), engine = (%d, %q)",
					gojaStart,
					gojaValue,
					start,
					value,
				)
			}
		})
	}
}
