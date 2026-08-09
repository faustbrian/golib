// Package metrics is the exact telemetry sink used by the tenancy analyzer
// fixture. It deliberately accepts any value so the analyzer, not the fixture
// type signature, owns the high-cardinality rejection.
package metrics

// Label records one metric label in the analyzer fixture.
func Label(any) {}
