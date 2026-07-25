package analysis_test

import (
	"go/parser"
	"go/token"
	"testing"
	"time"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
)

func FuzzParseSuppressions(f *testing.F) {
	f.Add("security/no-unsafe -- reviewed bridge; issue=SEC-1")
	f.Add("security/no-unsafe -- temporary; expires=2027-01-31")
	f.Add("security/no-unsafe -- temporary; expires=2020-01-01")
	f.Add("security/no-unsafe -- reviewed; issue=SEC-1; issue=SEC-2")
	f.Add("security/no-unsafe -- temporary; expires=2027-01-31; expires=2028-01-31")
	f.Add("security/no-unsafe -- reviewed; owner=platform")
	f.Add("security/unknown -- reason")
	f.Add("security/no-unsafe")
	f.Add(" -- missing rule")
	f.Add("security/no-unsafe -- ")

	f.Fuzz(func(t *testing.T, directive string) {
		if len(directive) > 64<<10 {
			t.Skip()
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(
			fset,
			"fuzz.go",
			"package fuzz\n//analysis:ignore "+directive+"\nvar Value int\n",
			parser.ParseComments,
		)
		if err != nil {
			return
		}
		_, _ = shared.ParseSuppressions(
			fset,
			file,
			[]string{"security/no-unsafe"},
			time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		)
	})
}
