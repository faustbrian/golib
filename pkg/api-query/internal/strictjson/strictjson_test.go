package strictjson

import (
	"encoding/json"
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

func TestDecodeHonorsExactSizeAndDepthBoundaries(t *testing.T) {
	t.Parallel()

	exact := []byte("{\"name\":\"ok\"}")
	var decoded struct {
		Name string
	}
	if err := Decode(exact, len(exact), &decoded); err != nil || decoded.Name != "ok" {
		t.Fatalf("Decode(exact size) = %#v, error = %v", decoded, err)
	}
	if err := Decode(exact, 0, &decoded); err == nil {
		t.Fatal("Decode accepted a disabled size limit")
	}

	exactArrays := strings.Repeat("[", maxDepth-1) + "0" + strings.Repeat("]", maxDepth-1)
	var value any
	if err := Decode([]byte(exactArrays), len(exactArrays), &value); err != nil {
		t.Fatalf("Decode(exact array depth) error = %v", err)
	}
	excessiveObjects := strings.Repeat("{\"x\":", maxDepth) + "0" + strings.Repeat("}", maxDepth)
	if err := Decode([]byte(excessiveObjects), len(excessiveObjects), &value); err == nil {
		t.Fatal("Decode accepted excessive object nesting")
	}
}

func TestRequireEndDistinguishesTrailingValuesFromMalformedData(t *testing.T) {
	t.Parallel()

	if err := requireEnd(json.NewDecoder(strings.NewReader("true"))); err == nil ||
		!strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("requireEnd(trailing value) error = %v", err)
	}
	if err := requireEnd(json.NewDecoder(strings.NewReader("#"))); err == nil ||
		!strings.Contains(err.Error(), "finish JSON") {
		t.Fatalf("requireEnd(malformed data) error = %v", err)
	}
}
