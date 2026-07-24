package breaker

// State is the policy-driven state of a circuit breaker.
type State uint8

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Mode is an explicit administrative override.
type Mode uint8

const (
	ModeNormal Mode = iota
	ModeForceOpen
	ModeDisabled
	ModeIsolated
)

func (m Mode) String() string {
	switch m {
	case ModeNormal:
		return "normal"
	case ModeForceOpen:
		return "force-open"
	case ModeDisabled:
		return "disabled"
	case ModeIsolated:
		return "isolated"
	default:
		return "unknown"
	}
}

// Outcome is the mutually exclusive classification of one completion.
type Outcome uint8

const (
	OutcomeSuccess Outcome = iota
	OutcomeFailure
	OutcomeIgnored
)

func (o Outcome) String() string {
	switch o {
	case OutcomeSuccess:
		return "success"
	case OutcomeFailure:
		return "failure"
	case OutcomeIgnored:
		return "ignored"
	default:
		return "unknown"
	}
}
