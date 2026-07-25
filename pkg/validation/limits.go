package validation

import "fmt"

// Limits bounds validation work performed on untrusted input.
type Limits struct {
	MaxDepth               int
	MaxCollectionSize      int
	MaxStringLength        int
	MaxViolations          int
	MaxPathLength          int
	MaxMetadataEntries     int
	MaxMetadataKeyLength   int
	MaxMetadataValueLength int
	MaxRegexPatternLength  int
	MaxCustomConcurrency   int
	MaxStructFields        int
	MaxTagLength           int
	MaxCompiledPlans       int
}

// DefaultLimits returns conservative limits suitable for application input.
func DefaultLimits() Limits {
	return Limits{
		MaxDepth: 32, MaxCollectionSize: 10_000, MaxStringLength: 65_536,
		MaxViolations: 100,
		MaxPathLength: 1_024, MaxMetadataEntries: 16,
		MaxMetadataKeyLength: 64, MaxMetadataValueLength: 256,
		MaxRegexPatternLength: 1_024, MaxCustomConcurrency: 8,
		MaxStructFields: 256, MaxTagLength: 1_024, MaxCompiledPlans: 256,
	}
}

func (l Limits) validate() error {
	values := []int{l.MaxDepth, l.MaxCollectionSize, l.MaxStringLength,
		l.MaxViolations,
		l.MaxPathLength, l.MaxMetadataEntries, l.MaxMetadataKeyLength,
		l.MaxMetadataValueLength, l.MaxRegexPatternLength,
		l.MaxCustomConcurrency, l.MaxStructFields, l.MaxTagLength,
		l.MaxCompiledPlans}
	for _, value := range values {
		if value <= 0 {
			return fmt.Errorf("%w: limits must be positive", ErrInvalidLimit)
		}
	}
	return nil
}
