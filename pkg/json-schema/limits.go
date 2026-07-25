package jsonschema

import "fmt"

// Limits bounds JSON ingestion work. Additional compile and evaluation limits
// will be added as their corresponding evaluator components are introduced.
type Limits struct {
	MaxInputBytes             int
	MaxNestingDepth           int
	MaxTotalValues            int
	MaxObjectMembers          int
	MaxArrayItems             int
	MaxNumberBytes            int
	MaxSchemaResources        int
	MaxTotalSchemaBytes       int
	MaxEvaluationOps          int
	MaxUniqueComparisons      int
	MaxFormatChecks           int
	MaxSchemaNodes            int
	MaxReferenceDepth         int
	MaxDynamicScopeDepth      int
	MaxCombinatorBranches     int
	MaxRegexCount             int
	MaxRegexBytes             int
	MaxRegexBacktracking      int
	MaxRegexMatchMilliseconds int
	MaxOutputUnits            int
	MaxCustomKeywordCompiles  int
	MaxCustomKeywordCalls     int
	MaxAnnotationBytes        int
}

// DefaultLimits returns conservative standalone defaults.
func DefaultLimits() Limits {
	return Limits{
		MaxInputBytes:             16 << 20,
		MaxNestingDepth:           256,
		MaxTotalValues:            1_000_000,
		MaxObjectMembers:          1_000_000,
		MaxArrayItems:             1_000_000,
		MaxNumberBytes:            4096,
		MaxSchemaResources:        1024,
		MaxTotalSchemaBytes:       64 << 20,
		MaxEvaluationOps:          1_000_000,
		MaxUniqueComparisons:      1_000_000,
		MaxFormatChecks:           100_000,
		MaxSchemaNodes:            100_000,
		MaxReferenceDepth:         256,
		MaxDynamicScopeDepth:      256,
		MaxCombinatorBranches:     100_000,
		MaxRegexCount:             10_000,
		MaxRegexBytes:             1 << 20,
		MaxRegexBacktracking:      100_000,
		MaxRegexMatchMilliseconds: 100,
		MaxOutputUnits:            100_000,
		MaxCustomKeywordCompiles:  100_000,
		MaxCustomKeywordCalls:     100_000,
		MaxAnnotationBytes:        1 << 20,
	}
}

func (limits Limits) validate() error {
	values := []struct {
		name  string
		value int
	}{
		{name: "MaxInputBytes", value: limits.MaxInputBytes},
		{name: "MaxNestingDepth", value: limits.MaxNestingDepth},
		{name: "MaxTotalValues", value: limits.MaxTotalValues},
		{name: "MaxObjectMembers", value: limits.MaxObjectMembers},
		{name: "MaxArrayItems", value: limits.MaxArrayItems},
		{name: "MaxNumberBytes", value: limits.MaxNumberBytes},
		{name: "MaxSchemaResources", value: limits.MaxSchemaResources},
		{name: "MaxTotalSchemaBytes", value: limits.MaxTotalSchemaBytes},
		{name: "MaxEvaluationOps", value: limits.MaxEvaluationOps},
		{name: "MaxUniqueComparisons", value: limits.MaxUniqueComparisons},
		{name: "MaxFormatChecks", value: limits.MaxFormatChecks},
		{name: "MaxSchemaNodes", value: limits.MaxSchemaNodes},
		{name: "MaxReferenceDepth", value: limits.MaxReferenceDepth},
		{name: "MaxDynamicScopeDepth", value: limits.MaxDynamicScopeDepth},
		{name: "MaxCombinatorBranches", value: limits.MaxCombinatorBranches},
		{name: "MaxRegexCount", value: limits.MaxRegexCount},
		{name: "MaxRegexBytes", value: limits.MaxRegexBytes},
		{name: "MaxRegexBacktracking", value: limits.MaxRegexBacktracking},
		{name: "MaxRegexMatchMilliseconds", value: limits.MaxRegexMatchMilliseconds},
		{name: "MaxOutputUnits", value: limits.MaxOutputUnits},
		{name: "MaxCustomKeywordCompiles", value: limits.MaxCustomKeywordCompiles},
		{name: "MaxCustomKeywordCalls", value: limits.MaxCustomKeywordCalls},
		{name: "MaxAnnotationBytes", value: limits.MaxAnnotationBytes},
	}

	for _, item := range values {
		if item.value <= 0 {
			return fmt.Errorf("%w: %s must be positive", ErrLimitExceeded, item.name)
		}
	}

	return nil
}
