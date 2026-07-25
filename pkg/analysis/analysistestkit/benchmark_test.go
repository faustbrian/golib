package analysistestkit

import "testing"

func BenchmarkAnalyzers(b *testing.B) {
	pass, err := buildPass(representativeSource)
	if err != nil {
		b.Fatalf("buildPass() error = %v", err)
	}
	analyzers := configuredAnalyzers(b)
	prepareRequirements(b, pass, analyzers)
	for _, analyzer := range analyzers {
		analyzer := analyzer
		b.Run(analyzer.Name, func(b *testing.B) {
			b.ReportAllocs()
			pass.Analyzer = analyzer
			for b.Loop() {
				if _, err := analyzer.Run(pass); err != nil {
					b.Fatalf("Run() error = %v", err)
				}
			}
		})
	}
}

func BenchmarkAggregate(b *testing.B) {
	pass, err := buildPass(representativeSource)
	if err != nil {
		b.Fatalf("buildPass() error = %v", err)
	}
	analyzers := configuredAnalyzers(b)
	prepareRequirements(b, pass, analyzers)
	b.ReportAllocs()
	for b.Loop() {
		for _, analyzer := range analyzers {
			pass.Analyzer = analyzer
			if _, err := analyzer.Run(pass); err != nil {
				b.Fatalf("%s.Run() error = %v", analyzer.Name, err)
			}
		}
	}
}
