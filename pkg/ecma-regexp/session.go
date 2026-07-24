package ecmascript

import "context"

// Session is caller-owned mutable RegExp execution state. Programs remain
// immutable and safe for concurrent use; a Session is not concurrency-safe.
type Session struct {
	program   *Program
	lastIndex int
}

func NewSession(program *Program) *Session {
	return &Session{program: program}
}

func (s *Session) LastIndex() int { return s.lastIndex }

func (s *Session) SetLastIndex(index int) {
	if index < 0 {
		index = 0
	}
	s.lastIndex = index
}

// Exec applies RegExpBuiltinExec lastIndex, global, and sticky behavior. Limit
// failures leave lastIndex unchanged; an ordinary failed global or sticky
// match resets it to zero.
func (s *Session) Exec(ctx context.Context, input string, limits MatchLimits) (Result, bool, error) {
	view, err := makeInputView(input, limits)
	if err != nil {
		return Result{}, false, err
	}
	return s.exec(ctx, view, limits)
}

// ExecUTF16 applies stateful RegExp execution to an exact ECMAScript string.
func (s *Session) ExecUTF16(ctx context.Context, input UTF16String, limits MatchLimits) (Result, bool, error) {
	view, err := makeUTF16InputView(input, limits)
	if err != nil {
		return Result{}, false, err
	}
	return s.exec(ctx, view, limits)
}

func (s *Session) exec(ctx context.Context, view *inputView, limits MatchLimits) (Result, bool, error) {
	stateful := s.program.flags.Global() || s.program.flags.Sticky()
	start := 0
	if stateful {
		start = s.lastIndex
		if start > len(view.units) {
			s.lastIndex = 0
			return Result{}, false, nil
		}
	}
	executor := newExecutor(ctx, s.program, view, limits)
	result, matched, err := executor.find(start, s.program.flags.Sticky())
	if err != nil {
		return Result{}, false, err
	}
	if !matched {
		if stateful {
			s.lastIndex = 0
		}
		return Result{}, false, nil
	}
	if stateful {
		s.lastIndex = result.Full().span.End.UTF16
	}

	return result, true, nil
}
