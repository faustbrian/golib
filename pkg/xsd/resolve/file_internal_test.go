package resolve

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestFilePathFromURIPathNormalizesOnlyWindowsDrivePaths(t *testing.T) {
	t.Parallel()

	separator := string(filepath.Separator)
	for name, test := range map[string]struct {
		input string
		want  string
	}{
		"drive root":       {input: separator + "C:", want: "C:"},
		"drive path":       {input: separator + "C:" + separator + "schema.xsd", want: "C:" + separator + "schema.xsd"},
		"short path":       {input: separator, want: separator},
		"missing colon":    {input: separator + "CC" + separator + "schema.xsd", want: separator + "CC" + separator + "schema.xsd"},
		"already relative": {input: "C:" + separator + "schema.xsd", want: "C:" + separator + "schema.xsd"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := filePathFromURIPath(test.input); got != test.want {
				t.Fatalf("filePathFromURIPath(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestConfinedRelativeRejectsPathAndPlatformErrors(t *testing.T) {
	t.Parallel()

	platformErr := errors.New("platform relative-path failure")
	for name, test := range map[string]struct {
		relative string
		err      error
		want     string
		ok       bool
	}{
		"platform error": {relative: ".", err: platformErr},
		"parent":         {relative: ".."},
		"nested parent":  {relative: filepath.Join("..", "outside.xsd")},
		"root":           {relative: ".", want: ".", ok: true},
		"nested local":   {relative: filepath.Join("schemas", "value.xsd"), want: filepath.Join("schemas", "value.xsd"), ok: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := confinedRelative(test.relative, test.err)
			if got != test.want || ok != test.ok {
				t.Fatalf("confinedRelative(%q, %v) = %q, %t, want %q, %t", test.relative, test.err, got, ok, test.want, test.ok)
			}
		})
	}
}
