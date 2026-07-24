package match

// Error is a stable privacy-safe matching error identity.
type Error string

// Error implements error.
func (e Error) Error() string { return string(e) }

const (
	ErrCandidateLimit     Error = "localized match: candidate limit exceeded"
	ErrDepthLimit         Error = "localized match: fallback depth exceeded"
	ErrDuplicateCandidate Error = "localized match: duplicate candidate"
	ErrFallbackCycle      Error = "localized match: fallback cycle"
	ErrInvalidCandidate   Error = "localized match: invalid fallback candidate"
	ErrInvalidWeight      Error = "localized match: invalid weight"
)
