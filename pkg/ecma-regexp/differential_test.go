package ecmascript_test

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"slices"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

type differentialVector struct {
	Pattern string `json:"pattern"`
	Flags   string `json:"flags"`
	Input   string `json:"input"`
}

type differentialOutcome struct {
	Matched  bool        `json:"matched"`
	Start    int         `json:"start"`
	End      int         `json:"end"`
	Captures *[][]uint16 `json:"captures"`
}

func TestDifferentialMatchingAgainstJavaScriptEngines(t *testing.T) {
	vectors := []differentialVector{
		{Pattern: "(a|ab)+c", Flags: "d", Input: "ababc"},
		{Pattern: "a+?b", Input: "zaaab"},
		{Pattern: "(a)?b\\1", Input: "b"},
		{Pattern: "(?=(a+))a*b\\1", Input: "baaabac"},
		{Pattern: "(?!b)a", Input: "za"},
		{Pattern: "(?<=([ab]+)([bc]+))$", Input: "abc"},
		{Pattern: "(?<word>a)\\k<word>", Input: "zaa"},
		{Pattern: "(?:(?<x>a)|(?<x>b))\\k<x>", Flags: "u", Input: "zbb"},
		{Pattern: "(?i:a)b", Input: "zAb"},
		{Pattern: "^[^\\d]\\s\\w$", Input: "A _"},
		{Pattern: "\\bcat\\B", Input: "a catfish"},
		{Pattern: "^.$", Flags: "u", Input: "😀"},
		{Pattern: "^..$", Input: "😀"},
		{Pattern: "^\\uD83D\\uDE00$", Flags: "u", Input: "😀"},
		{Pattern: "^\\p{Script=Greek}+$", Flags: "u", Input: "Ωω"},
		{Pattern: "^[\\q{ab|cd}--\\q{cd}]$", Flags: "v", Input: "ab"},
		{Pattern: "^[[a-z]&&[^aeiou]]+$", Flags: "v", Input: "bcdf"},
		{Pattern: "^[\\q{|a}]$", Flags: "v", Input: ""},
		{Pattern: "^.$", Flags: "s", Input: "\n"},
		{Pattern: "^b$", Flags: "m", Input: "a\nb\nc"},
		{Pattern: "^[a-z]$", Flags: "ui", Input: "ſ"},
		{Pattern: "^\\w$", Flags: "ui", Input: "K"},
		{Pattern: "^\\11$", Input: "\t"},
		{Pattern: "^[a-\\d]$", Input: "-"},
		{Pattern: "^(?=a)+a$", Input: "a"},
	}

	engines := map[string][]string{}
	if node, err := exec.LookPath("node"); err == nil {
		engines["node"] = []string{node, "-e"}
	}
	if deno, err := exec.LookPath("deno"); err == nil {
		engines["deno"] = []string{deno, "eval"}
	}
	if bun, err := exec.LookPath("bun"); err == nil {
		engines["bun"] = []string{bun, "-e"}
	}
	if len(engines) < 2 {
		t.Fatalf(
			"ECMAScript differential gate found %d JavaScript engine(s); want at least 2 of Node, Deno, or Bun",
			len(engines),
		)
	}
	for name, command := range engines {
		t.Run(name, func(t *testing.T) {
			compareDifferentialOutcomes(t, name, command, vectors)
		})
	}
}

func compareDifferentialOutcomes(t *testing.T, engine string, command []string, vectors []differentialVector) {
	t.Helper()
	want := runJSDifferential(t, command, vectors)
	for index, vector := range vectors {
		program, err := ecmascript.Compile(vector.Pattern, vector.Flags, ecmascript.DefaultCompileOptions())
		if err != nil {
			t.Errorf("Compile(%q, %q) error = %v", vector.Pattern, vector.Flags, err)
			continue
		}
		result, matched, err := program.Find(
			context.Background(),
			vector.Input,
			ecmascript.DefaultMatchOptions(),
		)
		if err != nil {
			t.Errorf("Find(%q, %q, %q) error = %v", vector.Pattern, vector.Flags, vector.Input, err)
			continue
		}
		if matched != want[index].Matched {
			if knownJavaScriptEngineDivergence(engine, vector) {
				t.Logf(
					"reported %s divergence for Find(%q, %q, %q)",
					engine,
					vector.Pattern,
					vector.Flags,
					vector.Input,
				)
				continue
			}
			t.Errorf("Find(%q, %q, %q) matched = %t; %s = %t", vector.Pattern, vector.Flags, vector.Input, matched, engine, want[index].Matched)
			continue
		}
		if !matched {
			continue
		}
		full := result.Full().Span()
		if full.Start.UTF16 != want[index].Start || full.End.UTF16 != want[index].End {
			t.Errorf("Find(%q, %q, %q) span = %d..%d; %s = %d..%d", vector.Pattern, vector.Flags, vector.Input, full.Start.UTF16, full.End.UTF16, engine, want[index].Start, want[index].End)
		}
		captures := result.Captures()
		if want[index].Captures == nil || len(captures) != len(*want[index].Captures) {
			t.Errorf("Find(%q, %q, %q) capture count = %d; Node = %v", vector.Pattern, vector.Flags, vector.Input, len(captures), want[index].Captures)
			continue
		}
		for captureIndex, wantUnits := range *want[index].Captures {
			if wantUnits == nil {
				if captures[captureIndex].Participated() {
					t.Errorf("Find(%q) capture %d participated; Node is undefined", vector.Pattern, captureIndex)
				}
				continue
			}
			if !captures[captureIndex].Participated() || !slices.Equal(captures[captureIndex].Value().Units(), wantUnits) {
				t.Errorf("Find(%q) capture %d = %04X; Node = %04X", vector.Pattern, captureIndex, captures[captureIndex].Value().Units(), wantUnits)
			}
		}
	}
}

func knownJavaScriptEngineDivergence(engine string, vector differentialVector) bool {
	return engine == "bun" && vector.Pattern == "^[a-z]$" &&
		vector.Flags == "ui" && vector.Input == "ſ"
}

func runJSDifferential(t *testing.T, command []string, vectors []differentialVector) []differentialOutcome {
	t.Helper()

	input, err := json.Marshal(vectors)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	script := "const arg=globalThis.Deno?Deno.args[0]:process.argv[1];" +
		"const vectors=JSON.parse(arg);" +
		"const units=s=>Array.from({length:s.length},(_,i)=>s.charCodeAt(i));" +
		"const out=v=>{const m=new RegExp(v.pattern,v.flags).exec(v.input);" +
		"if(m===null)return {matched:false,start:0,end:0,captures:null};" +
		"return {matched:true,start:m.index,end:m.index+m[0].length," +
		"captures:Array.from(m,x=>x===undefined?null:units(x))};};" +
		"console.log(JSON.stringify(vectors.map(out)));"
	arguments := append(append([]string(nil), command[1:]...), script, string(input))
	execution := exec.CommandContext(context.Background(), command[0], arguments...)
	output, err := execution.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			t.Fatalf("JavaScript differential failed: %s", exitError.Stderr)
		}
		t.Fatalf("JavaScript differential failed: %v", err)
	}
	var outcomes []differentialOutcome
	if err := json.Unmarshal(output, &outcomes); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return outcomes
}
