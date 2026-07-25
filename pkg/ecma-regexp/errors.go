package ecmascript

import (
	"fmt"
	"time"
)

// SyntaxCode classifies a pattern error without requiring message matching.
type SyntaxCode uint8

const (
	SyntaxUnexpectedToken SyntaxCode = iota + 1
	SyntaxUnexpectedEOF
	SyntaxUnclosedGroup
	SyntaxInvalidQuantifier
	SyntaxInvalidEscape
	SyntaxInvalidBackreference
	SyntaxUnsupported
)

// SyntaxError reports a bounded diagnostic at a byte span in the pattern.
type SyntaxError struct {
	Code    SyntaxCode
	Span    Span
	Message string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("invalid pattern at bytes %d..%d: %s", e.Span.Start, e.Span.End, e.Message)
}

// LimitKind identifies a finite parser or execution resource.
type LimitKind uint8

const (
	LimitPatternBytes LimitKind = iota + 1
	LimitASTDepth
	LimitCaptures
	LimitCharacterClasses
	LimitASTNodes
	LimitProgramInstructions
	LimitInputBytes
	LimitInputRunes
	LimitMatchSteps
	LimitBacktracks
	LimitStackDepth
	LimitRecursionDepth
	LimitAllocations
	LimitResults
	LimitOutputUTF16
)

// LimitError distinguishes resource exhaustion from invalid syntax and a
// successful no-match result.
type LimitError struct {
	Kind  LimitKind
	Limit uint64
	Used  uint64
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("regular expression resource limit exceeded: kind=%d limit=%d used=%d", e.Kind, e.Limit, e.Used)
}

// TimeoutError reports synchronous wall-time budget exhaustion. The matcher
// never creates a worker goroutine to enforce time.
type TimeoutError struct {
	Limit   time.Duration
	Elapsed time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("regular expression wall-time limit exceeded: limit=%s elapsed=%s", e.Limit, e.Elapsed)
}
