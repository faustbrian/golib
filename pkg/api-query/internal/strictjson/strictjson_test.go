package strictjson

import (
	"strings"
	"testing"
)

func TestDecodeStrictSemanticMatrix(t *testing.T) {
	t.Parallel()

	type target struct {
		Name   string `json:"name"`
		Values []int  `json:"values"`
	}
	var decoded target
	if err := Decode([]byte(`{"name":"ok","values":[1,2]}`), 100, &decoded); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.Name != "ok" || len(decoded.Values) != 2 {
		t.Fatalf("decoded = %#v", decoded)
	}

	invalid := [][]byte{
		nil,
		[]byte(`{"name":"x"}`),
		[]byte(`{"name":"x","name":"y"}`),
		[]byte(`{"unknown":1}`),
		[]byte(`{"name":`),
		[]byte(`{"name":"x"} true`),
		[]byte(`{"values":[{"x":1,"x":2}]}`),
		[]byte(`}`),
		[]byte(`{"name`),
		[]byte(`{"name":"x"`),
		[]byte(`{"name":"x"} #`),
	}
	for index, data := range invalid {
		max := 100
		if index == 1 {
			max = 2
		}
		if err := Decode(data, max, &target{}); err == nil {
			t.Fatalf("Decode(case %d) accepted %q", index, data)
		}
	}
}

func TestDecodeRejectsExcessiveNesting(t *testing.T) {
	t.Parallel()

	data := strings.Repeat("[", maxDepth+1) + "0" + strings.Repeat("]", maxDepth+1)
	var target any
	if err := Decode([]byte(data), len(data), &target); err == nil {
		t.Fatal("Decode accepted excessive nesting")
	}
}
