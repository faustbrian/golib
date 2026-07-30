package environment

import (
	"errors"
	"reflect"
	"runtime"
	"testing"
)

func TestCollectFieldsRejectsSchemaBeyondDepthLimitInternally(t *testing.T) {
	t.Parallel()

	type settings struct{ Value string }
	fields, err := collectFields(
		reflect.TypeFor[settings](),
		[]string{"nested"},
		nil,
		Options{},
		maxSchemaDepth+1,
		make(map[reflect.Type]bool),
	)
	if fields != nil || err == nil {
		t.Fatalf("collectFields() = %#v, %v", fields, err)
	}

	fields, err = collectFields(
		reflect.TypeFor[struct{}](),
		nil,
		nil,
		Options{},
		maxSchemaDepth,
		make(map[reflect.Type]bool),
	)
	if err != nil || len(fields) != 0 {
		t.Fatalf("collectFields(max depth) = %#v, %v", fields, err)
	}

	type nestedAtLimit struct {
		Nested struct {
			Value string `config:"value"`
		} `config:"nested"`
	}
	fields, err = collectFields(
		reflect.TypeFor[nestedAtLimit](),
		nil,
		nil,
		Options{},
		maxSchemaDepth,
		make(map[reflect.Type]bool),
	)
	var schemaErr *SchemaError
	if fields != nil || !errors.As(err, &schemaErr) || schemaErr.Path != "nested" {
		t.Fatalf("collectFields(nested at limit) = %#v, %#v", fields, err)
	}
}

func TestCollectFieldsContinuesAcrossSkippedAndNestedFieldsInternally(t *testing.T) {
	t.Parallel()

	type settings struct {
		_      string
		Nested struct {
			Value string `config:"value"`
		} `config:"nested"`
		Ignored string `config:"ignored" env:"-"`
		Final   string `config:"final"`
	}
	fields, err := collectFields(
		reflect.TypeFor[settings](),
		nil,
		nil,
		Options{Separator: "_"},
		1,
		make(map[reflect.Type]bool),
	)
	if err != nil {
		t.Fatalf("collectFields() error = %v", err)
	}
	if len(fields) != 2 ||
		fields[0].envName != "NESTED_VALUE" ||
		fields[1].envName != "FINAL" {
		t.Fatalf("collectFields() = %#v", fields)
	}
}

func TestNormalizeAndValidNameBoundariesInternally(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		mode CaseMode
		goos string
		want bool
	}{
		"insensitive linux":   {mode: CaseInsensitive, goos: "linux", want: true},
		"insensitive windows": {mode: CaseInsensitive, goos: "windows", want: true},
		"native linux":        {mode: CaseNative, goos: "linux"},
		"native windows":      {mode: CaseNative, goos: "windows", want: true},
		"sensitive windows":   {mode: CaseSensitive, goos: "windows"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := caseInsensitive(test.mode, test.goos); got != test.want {
				t.Fatalf("caseInsensitive() = %t, want %t", got, test.want)
			}
		})
	}

	if normalize("Mixed", CaseInsensitive) != "MIXED" {
		t.Fatal("normalize(case insensitive) did not fold")
	}
	if normalize("Mixed", CaseSensitive) != "Mixed" {
		t.Fatal("normalize(case sensitive) folded")
	}
	native := normalize("Mixed", CaseNative)
	if runtime.GOOS == "windows" {
		if native != "MIXED" {
			t.Fatal("normalize(native Windows) did not fold")
		}
	} else if native != "Mixed" {
		t.Fatal("normalize(native non-Windows) folded")
	}

	for candidate, want := range map[string]bool{
		"_": true, "É_2": true, "A2": true,
		"": false, "2A": false, "A-B": false,
	} {
		if got := validName(candidate); got != want {
			t.Fatalf("validName(%q) = %v, want %v", candidate, got, want)
		}
	}
}
