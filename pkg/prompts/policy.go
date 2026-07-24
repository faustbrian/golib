package prompts

// InteractionMode defines whether execution may acquire interactive input.
type InteractionMode uint8

const (
	InteractiveRequired InteractionMode = iota
	InteractivePreferred
	NonInteractiveOnly
	AutoDetect
)

// AutoRules are caller-selected conditions for AutoDetect. Terminal detection
// is never sufficient without PermitInteraction.
type AutoRules struct {
	RequireInputTerminal  bool
	RequireOutputTerminal bool
}

// InteractionPolicy records the caller's explicit authority to interact.
type InteractionPolicy struct {
	Mode                   InteractionMode
	PermitInteraction      bool
	PermitDefaults         bool
	PermitUnlimitedRetries bool
	Auto                   AutoRules
}

// HeadlessBehavior defines how a prompt behaves without interactive input.
type HeadlessBehavior uint8

const (
	HeadlessForbidden HeadlessBehavior = iota
	HeadlessUseDefault
	HeadlessUseFallback
)

// ColorProfile describes the color output explicitly supported by a terminal.
type ColorProfile uint8

const (
	ColorNone ColorProfile = iota
	ColorANSI16
	ColorANSI256
	ColorTrueColor
)

// Capabilities are supplied by the caller or an explicit application adapter.
type Capabilities struct {
	InputTerminal  bool
	OutputTerminal bool
	Width          int
	Height         int
	Color          ColorProfile
	CursorMovement bool
	Hyperlinks     bool
	Animation      bool
	Unicode        bool
}
