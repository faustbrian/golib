package router

// Limits bounds all construction and URL-generation inputs. Zero values are
// invalid; start with DefaultLimits and adjust individual budgets.
type Limits struct {
	MaxRoutes             int
	MaxGroups             int
	MaxGroupDepth         int
	MaxMethodsPerRoute    int
	MaxMethodBytes        int
	MaxWildcardsPerRoute  int
	MaxWildcardNameBytes  int
	MaxPatternBytes       int
	MaxHostBytes          int
	MaxNameBytes          int
	MaxSourceBytes        int
	MaxOperationBytes     int
	MaxDocumentationBytes int
	MaxMetadataEntries    int
	MaxMetadataKeyBytes   int
	MaxMetadataValueBytes int
	MaxMiddleware         int
	MaxRequestTargetBytes int
	MaxURLParameters      int
	MaxURLParameterBytes  int
	MaxQueryValues        int
	MaxQueryBytes         int
	MaxGeneratedURLBytes  int
}

// DefaultLimits returns conservative production budgets.
func DefaultLimits() Limits {
	return Limits{
		MaxRoutes:             1_024,
		MaxGroups:             64,
		MaxGroupDepth:         8,
		MaxMethodsPerRoute:    16,
		MaxMethodBytes:        32,
		MaxWildcardsPerRoute:  16,
		MaxWildcardNameBytes:  64,
		MaxPatternBytes:       2_048,
		MaxHostBytes:          255,
		MaxNameBytes:          128,
		MaxSourceBytes:        256,
		MaxOperationBytes:     128,
		MaxDocumentationBytes: 4_096,
		MaxMetadataEntries:    32,
		MaxMetadataKeyBytes:   64,
		MaxMetadataValueBytes: 256,
		MaxMiddleware:         32,
		MaxRequestTargetBytes: 8_192,
		MaxURLParameters:      32,
		MaxURLParameterBytes:  4_096,
		MaxQueryValues:        128,
		MaxQueryBytes:         4_096,
		MaxGeneratedURLBytes:  8_192,
	}
}

func (l Limits) valid() bool {
	return l.MaxRoutes > 0 && l.MaxGroups > 0 && l.MaxGroupDepth > 0 &&
		l.MaxMethodsPerRoute > 0 && l.MaxMethodBytes > 0 &&
		l.MaxWildcardsPerRoute > 0 && l.MaxWildcardNameBytes > 0 &&
		l.MaxPatternBytes > 0 && l.MaxHostBytes > 0 &&
		l.MaxNameBytes > 0 && l.MaxSourceBytes > 0 &&
		l.MaxOperationBytes > 0 && l.MaxDocumentationBytes > 0 &&
		l.MaxMetadataEntries > 0 &&
		l.MaxMetadataKeyBytes > 0 && l.MaxMetadataValueBytes > 0 &&
		l.MaxMiddleware > 0 && l.MaxRequestTargetBytes > 0 &&
		l.MaxURLParameters > 0 &&
		l.MaxURLParameterBytes > 0 && l.MaxQueryValues > 0 &&
		l.MaxQueryBytes > 0 && l.MaxGeneratedURLBytes > 0
}
