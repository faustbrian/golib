package analysis

// Diagnostic is the stable reporting form of one policy violation.
type Diagnostic struct {
	Rule        string `json:"rule"`
	Package     string `json:"package,omitempty"`
	Filename    string `json:"filename"`
	Line        int    `json:"line"`
	Column      int    `json:"column,omitempty"`
	Message     string `json:"message"`
	Rationale   string `json:"rationale,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}
