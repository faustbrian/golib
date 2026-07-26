package analysistestkit

import "testing"

func FuzzAnalyzersNoPanic(f *testing.F) {
	f.Add(representativeSource)
	f.Add("package fuzzpkg\nfunc Empty() {}\n")
	f.Add("package fuzzpkg\ntype Box[T any] struct { Value T }\n")

	analyzers := configuredAnalyzers(f)
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 64<<10 {
			t.Skip()
		}
		pass, err := buildPass(source)
		if err != nil {
			return
		}
		prepareRequirements(t, pass, analyzers)
		for _, analyzer := range analyzers {
			pass.Analyzer = analyzer
			if _, err := analyzer.Run(pass); err != nil {
				t.Fatalf("%s.Run() error = %v", analyzer.Name, err)
			}
		}
	})
}
