package temporal

const (
	// HardMaxPeriods bounds the input and output of any set operation.
	HardMaxPeriods = 100_000
	// HardMaxSteps bounds iteration and splitting.
	HardMaxSteps = 1_000_000

	hardMaxParseBytes  = 64 * 1024
	hardMaxPrecision   = 9
	hardMaxErrorBytes  = 1024
	hardMaxFormatBytes = 64 * 1024
	hardMaxParserDepth = 8
)

// Limits controls allocation and work performed by variable-size operations.
// A zero field selects its safe default. Values cannot exceed hard limits.
type Limits struct {
	ParseBytes    int
	Precision     int
	ErrorBytes    int
	FormatBytes   int
	InputPeriods  int
	OutputPeriods int
	Steps         int
	ParserDepth   int
}

// DefaultLimits returns the package's bounded operational defaults.
func DefaultLimits() Limits {
	return Limits{
		ParseBytes:    hardMaxParseBytes,
		Precision:     hardMaxPrecision,
		ErrorBytes:    hardMaxErrorBytes,
		FormatBytes:   hardMaxFormatBytes,
		InputPeriods:  HardMaxPeriods,
		OutputPeriods: HardMaxPeriods,
		Steps:         HardMaxSteps,
		ParserDepth:   hardMaxParserDepth,
	}
}

// Resolve replaces zero fields with safe defaults.
func (l Limits) Resolve() Limits {
	defaults := DefaultLimits()

	if l.ParseBytes == 0 {
		l.ParseBytes = defaults.ParseBytes
	}
	if l.Precision == 0 {
		l.Precision = defaults.Precision
	}
	if l.ErrorBytes == 0 {
		l.ErrorBytes = defaults.ErrorBytes
	}
	if l.FormatBytes == 0 {
		l.FormatBytes = defaults.FormatBytes
	}
	if l.InputPeriods == 0 {
		l.InputPeriods = defaults.InputPeriods
	}
	if l.OutputPeriods == 0 {
		l.OutputPeriods = defaults.OutputPeriods
	}
	if l.Steps == 0 {
		l.Steps = defaults.Steps
	}
	if l.ParserDepth == 0 {
		l.ParserDepth = defaults.ParserDepth
	}

	return l
}

// Validate rejects negative fields and values above package hard limits.
func (l Limits) Validate() error {
	checks := [...]struct {
		name  string
		value int
		max   int
	}{
		{"parse_bytes", l.ParseBytes, hardMaxParseBytes},
		{"precision", l.Precision, hardMaxPrecision},
		{"error_bytes", l.ErrorBytes, hardMaxErrorBytes},
		{"format_bytes", l.FormatBytes, hardMaxFormatBytes},
		{"input_periods", l.InputPeriods, HardMaxPeriods},
		{"output_periods", l.OutputPeriods, HardMaxPeriods},
		{"steps", l.Steps, HardMaxSteps},
		{"parser_depth", l.ParserDepth, hardMaxParserDepth},
	}

	for _, check := range checks {
		if check.value < 0 || check.value > check.max {
			return &LimitError{Field: check.name, Value: check.value, Max: check.max}
		}
	}

	return nil
}
