package ruleengine

import "time"

// Limits bounds compilation and evaluation work. Zero values are invalid;
// callers should start with DefaultLimits and reduce values as needed.
type Limits struct {
	MaxRules           int
	MaxFacts           int
	MaxASTDepth        int
	MaxOperands        int
	MaxCollection      int
	MaxStringBytes     int
	MaxDefinitionBytes int
	MaxRegexBytes      int
	MaxIdentifierBytes int
	MaxTags            int
	MaxTagBytes        int
	MaxPathBytes       int
	MaxPathSegments    int
	MaxIterations      int
	MaxDerivedFacts    int
	MaxDiagnostics     int
	MaxExplanation     int
	EvaluationTimeout  time.Duration
}

// DefaultLimits returns conservative process-local limits.
func DefaultLimits() Limits {
	return Limits{
		MaxRules:           1_000,
		MaxFacts:           10_000,
		MaxASTDepth:        64,
		MaxOperands:        10_000,
		MaxCollection:      10_000,
		MaxStringBytes:     1 << 20,
		MaxDefinitionBytes: 4 << 20,
		MaxRegexBytes:      4 << 10,
		MaxIdentifierBytes: 256,
		MaxTags:            64,
		MaxTagBytes:        128,
		MaxPathBytes:       1_024,
		MaxPathSegments:    64,
		MaxIterations:      32,
		MaxDerivedFacts:    1_000,
		MaxDiagnostics:     100,
		MaxExplanation:     1_000,
		EvaluationTimeout:  5 * time.Second,
	}
}

func (l Limits) validate() error {
	if l.MaxRules <= 0 || l.MaxFacts <= 0 || l.MaxASTDepth <= 0 || l.MaxOperands <= 0 ||
		l.MaxCollection <= 0 || l.MaxStringBytes <= 0 || l.MaxRegexBytes <= 0 || l.MaxPathBytes <= 0 ||
		l.MaxDefinitionBytes <= 0 || l.MaxIdentifierBytes <= 0 || l.MaxTags <= 0 || l.MaxTagBytes <= 0 ||
		l.MaxPathSegments <= 0 || l.MaxIterations <= 0 || l.MaxDerivedFacts <= 0 ||
		l.MaxDiagnostics <= 0 || l.MaxExplanation <= 0 || l.EvaluationTimeout <= 0 {
		return newError(CodeInvalidLimit, "all limits must be positive")
	}
	return nil
}
