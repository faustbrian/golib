package ecmascript

import (
	"context"
	"time"
	"unicode/utf16"
)

// MatchLimits bounds one Match or Find call, including all Find start
// candidates. Zero is a zero allowance, not unlimited.
type MatchLimits struct {
	InputBytes     uint64
	InputRunes     uint64
	Steps          uint64
	Backtracks     uint64
	StackDepth     uint64
	RecursionDepth uint64
	Allocations    uint64
	Results        uint64
	OutputUTF16    uint64
	WallTime       time.Duration
}

// MatchOptions selects the explicit ECMAScript UTF-16 start position and all
// execution budgets.
type MatchOptions struct {
	StartUTF16 int
	Limits     MatchLimits
}

func DefaultMatchOptions() MatchOptions {
	return MatchOptions{Limits: MatchLimits{
		InputBytes:     16 << 20,
		InputRunes:     16 << 20,
		Steps:          10_000_000,
		Backtracks:     1_000_000,
		StackDepth:     100_000,
		RecursionDepth: 256,
		Allocations:    2_000_000,
		Results:        1_000_000,
		OutputUTF16:    16 << 20,
		WallTime:       time.Second,
	}}
}

// Capture preserves the difference between an unmatched group and a group
// that participated with an empty value.
type Capture struct {
	participated bool
	value        UTF16String
	span         IndexSpan
}

func (c Capture) Participated() bool { return c.participated }
func (c Capture) Value() UTF16String { return newUTF16String(c.value.units) }
func (c Capture) Span() IndexSpan    { return c.span }

// Result is an immutable match result. Capture zero is the complete match.
type Result struct {
	captures []Capture
	names    map[string][]int
}

func (r Result) Named(name string) (Capture, bool) {
	indices := r.names[name]
	if len(indices) == 0 {
		return Capture{}, false
	}
	for _, index := range indices {
		if r.captures[index].participated {
			return r.captures[index], true
		}
	}
	return r.captures[indices[0]], true
}

func (r Result) Full() Capture { return r.captures[0] }
func (r Result) Captures() []Capture {
	return append([]Capture(nil), r.captures...)
}

// Match attempts the program exactly at StartUTF16.
func (p *Program) Match(ctx context.Context, input string, options MatchOptions) (Result, bool, error) {
	view, err := makeInputView(input, options.Limits)
	if err != nil {
		return Result{}, false, err
	}
	executor := newExecutor(ctx, p, view, options.Limits)
	return executor.at(options.StartUTF16)
}

// MatchUTF16 attempts the program against an exact ECMAScript string at
// StartUTF16, including inputs containing lone surrogates.
func (p *Program) MatchUTF16(ctx context.Context, input UTF16String, options MatchOptions) (Result, bool, error) {
	view, err := makeUTF16InputView(input, options.Limits)
	if err != nil {
		return Result{}, false, err
	}
	executor := newExecutor(ctx, p, view, options.Limits)
	return executor.at(options.StartUTF16)
}

// Find returns the first ordered match at or after StartUTF16. Sticky programs
// attempt only the explicit start position.
func (p *Program) Find(ctx context.Context, input string, options MatchOptions) (Result, bool, error) {
	view, err := makeInputView(input, options.Limits)
	if err != nil {
		return Result{}, false, err
	}
	executor := newExecutor(ctx, p, view, options.Limits)
	return executor.find(options.StartUTF16, p.flags.Sticky())
}

// FindUTF16 returns the first ordered match in an exact ECMAScript string.
func (p *Program) FindUTF16(ctx context.Context, input UTF16String, options MatchOptions) (Result, bool, error) {
	view, err := makeUTF16InputView(input, options.Limits)
	if err != nil {
		return Result{}, false, err
	}
	executor := newExecutor(ctx, p, view, options.Limits)
	return executor.find(options.StartUTF16, p.flags.Sticky())
}

func (e *executor) find(start int, sticky bool) (Result, bool, error) {
	if sticky {
		return e.at(start)
	}
	for ; start <= len(e.input.units); start++ {
		if (e.program.flags.unicodeMode()) && !e.input.codePointBoundary[start] {
			continue
		}
		result, matched, err := e.at(start)
		if err != nil || matched {
			return result, matched, err
		}
	}

	return Result{}, false, nil
}

type thread struct {
	pc       int
	position int
	captures []int
	guards   []int
}

type executor struct {
	ctx         context.Context
	program     *Program
	input       *inputView
	limits      MatchLimits
	started     time.Time
	steps       uint64
	backtracks  uint64
	allocations uint64
}

func newExecutor(ctx context.Context, program *Program, input *inputView, limits MatchLimits) *executor {
	if ctx == nil {
		ctx = context.Background()
	}

	return &executor{ctx: ctx, program: program, input: input, limits: limits, started: time.Now()}
}

func (e *executor) at(start int) (Result, bool, error) {
	if err := e.check(); err != nil {
		return Result{}, false, err
	}
	if start < 0 || start > len(e.input.units) {
		return Result{}, false, &SyntaxError{Code: SyntaxUnexpectedToken, Message: "start UTF-16 index is outside input"}
	}
	if err := e.allocate(2); err != nil {
		return Result{}, false, err
	}
	captures := make([]int, (e.program.captures+1)*2)
	for index := range captures {
		captures[index] = -1
	}
	captures[0] = start
	guards := make([]int, e.program.guardCount)
	for index := range guards {
		guards[index] = -1
	}
	outcome, matched, err := e.run(0, start, captures, guards, 1, 0)
	if err != nil || !matched {
		return Result{}, matched, err
	}
	outcome.captures[1] = outcome.position
	return e.result(outcome.captures), true, nil
}

type runOutcome struct {
	position int
	captures []int
	guards   []int
}

func (e *executor) run(pc, start int, captures, guards []int, direction, depth int) (runOutcome, bool, error) {
	current := thread{pc: pc, captures: captures, guards: guards, position: start}
	stack := make([]thread, 0, min(len(e.program.code), 64))

	for {
		if err := e.step(); err != nil {
			return runOutcome{}, false, err
		}
		instruction := e.program.code[current.pc]
		switch instruction.op {
		case opChar:
			unitPosition := current.position
			if direction == -1 {
				unitPosition--
			}
			if unitPosition < 0 || unitPosition >= len(e.input.units) || !e.equal(e.input.units[unitPosition], instruction.value, instruction.flags) {
				resumed, err := e.backtrack(&current, &stack)
				if err != nil {
					return runOutcome{}, false, err
				}
				if !resumed {
					return runOutcome{}, false, nil
				}
			} else {
				current.position += direction
				current.pc++
			}
		case opCodePoint:
			char, width, ok := e.codePointAt(current.position)
			if direction == -1 {
				char, width, ok = e.codePointBefore(current.position)
			}
			if !ok || !e.equalCodePoint(char, instruction.runeValue, instruction.flags) {
				resumed, err := e.backtrack(&current, &stack)
				if err != nil {
					return runOutcome{}, false, err
				}
				if !resumed {
					return runOutcome{}, false, nil
				}
			} else {
				current.position += direction * width
				current.pc++
			}
		case opAny:
			width := e.anyWidth(current.position, direction, instruction.flags)
			if width == 0 {
				resumed, err := e.backtrack(&current, &stack)
				if err != nil {
					return runOutcome{}, false, err
				}
				if !resumed {
					return runOutcome{}, false, nil
				}
			} else {
				current.position += direction * width
				current.pc++
			}
		case opStart:
			if !e.atStart(current.position, instruction.flags) {
				resumed, err := e.backtrack(&current, &stack)
				if err != nil {
					return runOutcome{}, false, err
				}
				if !resumed {
					return runOutcome{}, false, nil
				}
			} else {
				current.pc++
			}
		case opEnd:
			if !e.atEnd(current.position, instruction.flags) {
				resumed, err := e.backtrack(&current, &stack)
				if err != nil {
					return runOutcome{}, false, err
				}
				if !resumed {
					return runOutcome{}, false, nil
				}
			} else {
				current.pc++
			}
		case opWordBoundary:
			boundary := e.wordAt(current.position-1, true, instruction.flags) != e.wordAt(current.position, false, instruction.flags)
			if boundary != (instruction.value == 1) {
				resumed, err := e.backtrack(&current, &stack)
				if err != nil {
					return runOutcome{}, false, err
				}
				if !resumed {
					return runOutcome{}, false, nil
				}
			} else {
				current.pc++
			}
		case opClass:
			width := e.classWidth(current.position, direction, e.program.classSets[instruction.x], instruction.flags)
			if width == 0 {
				resumed, err := e.backtrack(&current, &stack)
				if err != nil {
					return runOutcome{}, false, err
				}
				if !resumed {
					return runOutcome{}, false, nil
				}
			} else {
				current.position += direction * width
				current.pc++
			}
		case opBackreference:
			width, matches := e.backreference(current, instruction.slots, direction, instruction.flags)
			if !matches {
				resumed, err := e.backtrack(&current, &stack)
				if err != nil {
					return runOutcome{}, false, err
				}
				if !resumed {
					return runOutcome{}, false, nil
				}
			} else {
				current.position += direction * width
				current.pc++
			}
		case opLook:
			lookDepth := depth + 1
			if uint64(lookDepth) > e.limits.RecursionDepth {
				return runOutcome{}, false, &LimitError{Kind: LimitRecursionDepth, Limit: e.limits.RecursionDepth, Used: uint64(lookDepth)}
			}
			if err := e.allocate(2); err != nil {
				return runOutcome{}, false, err
			}
			lookDirection := 1
			if instruction.value&2 != 0 {
				lookDirection = -1
			}
			lookOutcome, lookMatched, err := e.run(
				instruction.x,
				current.position,
				append([]int(nil), current.captures...),
				append([]int(nil), current.guards...),
				lookDirection,
				lookDepth,
			)
			if err != nil {
				return runOutcome{}, false, err
			}
			negative := instruction.value&1 != 0
			if lookMatched == negative {
				resumed, err := e.backtrack(&current, &stack)
				if err != nil {
					return runOutcome{}, false, err
				}
				if !resumed {
					return runOutcome{}, false, nil
				}
			} else {
				if !negative {
					current.captures = lookOutcome.captures
					current.guards = lookOutcome.guards
				}
				current.pc = instruction.y
			}
		case opSplit:
			if uint64(len(stack)+1) > e.limits.StackDepth {
				return runOutcome{}, false, &LimitError{Kind: LimitStackDepth, Limit: e.limits.StackDepth, Used: uint64(len(stack) + 1)}
			}
			if err := e.allocate(2); err != nil {
				return runOutcome{}, false, err
			}
			stack = append(stack, thread{
				pc:       instruction.y,
				position: current.position,
				captures: append([]int(nil), current.captures...),
				guards:   append([]int(nil), current.guards...),
			})
			current.pc = instruction.x
		case opJump:
			current.pc = instruction.x
		case opSave:
			current.captures[instruction.slot] = current.position
			current.pc++
		case opClear:
			for capture := instruction.x; capture <= instruction.y; capture++ {
				clearCapture(current.captures, capture)
			}
			current.pc++
		case opGuard:
			if current.guards[instruction.slot] == current.position {
				resumed, err := e.backtrack(&current, &stack)
				if err != nil {
					return runOutcome{}, false, err
				}
				if !resumed {
					return runOutcome{}, false, nil
				}
			} else {
				current.guards[instruction.slot] = current.position
				current.pc++
			}
		case opAccept, opMatch:
			return runOutcome{position: current.position, captures: current.captures, guards: current.guards}, true, nil
		}
	}
}

func clearCapture(captures []int, capture int) {
	startSlot := capture * 2
	captures[startSlot] = -1
	captures[startSlot+1] = -1
}

func (e *executor) backtrack(current *thread, stack *[]thread) (bool, error) {
	if len(*stack) == 0 {
		return false, nil
	}
	e.backtracks++
	if e.backtracks > e.limits.Backtracks {
		return false, &LimitError{Kind: LimitBacktracks, Limit: e.limits.Backtracks, Used: e.backtracks}
	}
	*current = (*stack)[len(*stack)-1]
	*stack = (*stack)[:len(*stack)-1]

	return true, nil
}

func (e *executor) result(indices []int) Result {
	captures := make([]Capture, len(indices)/2)
	for index := range captures {
		start, end := indices[index*2], indices[index*2+1]
		if captureParticipated(start, end) {
			captures[index] = Capture{participated: true, value: newUTF16String(e.input.units[start:end]), span: e.input.span(start, end)}
		}
	}

	return Result{captures: captures, names: cloneCaptureNames(e.program.captureNames)}
}

func captureParticipated(start, end int) bool {
	if start < 0 {
		return false
	}
	return end >= 0
}

func (e *executor) allocate(count uint64) error {
	e.allocations += count
	if e.allocations > e.limits.Allocations {
		return &LimitError{Kind: LimitAllocations, Limit: e.limits.Allocations, Used: e.allocations}
	}

	return nil
}

func (e *executor) step() error {
	e.steps++
	if e.steps > e.limits.Steps {
		return &LimitError{Kind: LimitMatchSteps, Limit: e.limits.Steps, Used: e.steps}
	}
	if shouldCheckExecution(e.steps) {
		return e.check()
	}

	return nil
}

func shouldCheckExecution(steps uint64) bool {
	if steps == 1 {
		return true
	}
	if steps < 256 {
		return false
	}
	return steps%256 == 0
}

func (e *executor) check() error {
	if err := e.ctx.Err(); err != nil {
		return err
	}
	elapsed := time.Since(e.started)
	if wallTimeExceeded(elapsed, e.limits.WallTime) {
		return &TimeoutError{Limit: e.limits.WallTime, Elapsed: elapsed}
	}

	return nil
}

func wallTimeExceeded(elapsed, limit time.Duration) bool {
	return elapsed > limit
}

func (e *executor) equal(left, right uint16, flags Flags) bool {
	if left == right {
		return true
	}
	if !flags.IgnoreCase() {
		return false
	}
	if utf16.IsSurrogate(rune(left)) {
		return false
	}
	if utf16.IsSurrogate(rune(right)) {
		return false
	}

	return legacyCanonical(rune(left)) == legacyCanonical(rune(right))
}

func (e *executor) equalCodePoint(left, right rune, flags Flags) bool {
	if left == right {
		return true
	}
	if !flags.IgnoreCase() {
		return false
	}
	return unicodeCanonical(left) == unicodeCanonical(right)
}

func (e *executor) anyWidth(position, direction int, flags Flags) int {
	unitPosition := position
	if direction == -1 {
		unitPosition--
	}
	if unitPosition < 0 {
		return 0
	}
	if unitPosition >= len(e.input.units) {
		return 0
	}
	if !flags.DotAll() && isLineTerminator(e.input.units[unitPosition]) {
		return 0
	}
	if e.program.flags.unicodeMode() {
		if direction == 1 && codePointWidthAt(e.input.units, position) == 2 {
			return 2
		}
		if direction == -1 && codePointWidthBefore(e.input.units, position) == 2 {
			return 2
		}
	}

	return 1
}

func (e *executor) classWidth(position, direction int, class characterClass, flags Flags) int {
	char, width, ok := e.codePointAt(position)
	if direction == -1 {
		char, width, ok = e.codePointBefore(position)
	}
	if !ok {
		return 0
	}
	matched := class.matches(char, flags)
	if !matched {
		return 0
	}

	return width
}

func (e *executor) backreference(current thread, captures []int, direction int, flags Flags) (int, bool) {
	capture := participatingCapture(current.captures, captures)
	if capture == 0 {
		return 0, true
	}
	start := current.captures[capture*2]
	end := current.captures[capture*2+1]
	if e.program.flags.unicodeMode() {
		return e.unicodeBackreference(current.position, start, end, direction, flags)
	}
	width := end - start
	inputStart := current.position
	if direction == -1 {
		inputStart -= width
	}
	if inputStart < 0 {
		return 0, false
	}
	available := e.input.units[inputStart:]
	if width > len(available) {
		return 0, false
	}
	for offset := 0; offset < width; offset++ {
		if !e.equal(e.input.units[start+offset], e.input.units[inputStart+offset], flags) {
			return 0, false
		}
	}

	return width, true
}

func participatingCapture(captureSlots, candidates []int) int {
	for _, candidate := range candidates {
		if captureParticipated(captureSlots[candidate*2], captureSlots[candidate*2+1]) {
			return candidate
		}
	}
	return 0
}

func (e *executor) unicodeBackreference(inputPosition, captureStart, captureEnd, direction int, flags Flags) (int, bool) {
	inputCursor := inputPosition
	if direction == 1 {
		captureCursor := captureStart
		for captureCursor < captureEnd {
			captureChar, captureWidth, ok := codePointAtUnits(e.input.units, captureCursor)
			if !ok {
				return 0, false
			}
			if captureCursor+captureWidth > captureEnd {
				return 0, false
			}
			inputChar, inputWidth, ok := codePointAtUnits(e.input.units, inputCursor)
			if !ok {
				return 0, false
			}
			if !e.equalCodePoint(captureChar, inputChar, flags) {
				return 0, false
			}
			captureCursor += captureWidth
			inputCursor += inputWidth
		}
		return inputCursor - inputPosition, true
	}
	captureCursor := captureEnd
	for captureCursor > captureStart {
		captureChar, captureWidth, ok := codePointBeforeUnits(e.input.units, captureCursor)
		if !ok {
			return 0, false
		}
		if captureCursor-captureWidth < captureStart {
			return 0, false
		}
		inputChar, inputWidth, ok := codePointBeforeUnits(e.input.units, inputCursor)
		if !ok {
			return 0, false
		}
		if !e.equalCodePoint(captureChar, inputChar, flags) {
			return 0, false
		}
		captureCursor -= captureWidth
		inputCursor = inputCursor - inputWidth
	}
	return inputPosition - inputCursor, true
}

func codePointAtUnits(units []uint16, position int) (rune, int, bool) {
	if position < 0 {
		return 0, 0, false
	}
	if position >= len(units) {
		return 0, 0, false
	}
	if codePointWidthAt(units, position) == 2 {
		return utf16.DecodeRune(rune(units[position]), rune(units[position+1])), 2, true
	}
	return rune(units[position]), 1, true
}

func codePointBeforeUnits(units []uint16, position int) (rune, int, bool) {
	if position <= 0 {
		return 0, 0, false
	}
	if position > len(units) {
		return 0, 0, false
	}
	if codePointWidthBefore(units, position) == 2 {
		return utf16.DecodeRune(rune(units[position-2]), rune(units[position-1])), 2, true
	}
	return rune(units[position-1]), 1, true
}

func codePointWidthAt(units []uint16, position int) int {
	if position < 0 || position >= len(units) {
		return 0
	}
	if position+1 == len(units) {
		return 1
	}
	if isHighSurrogate(units[position]) && isLowSurrogate(units[position+1]) {
		return 2
	}
	return 1
}

func codePointWidthBefore(units []uint16, position int) int {
	if position <= 0 || position > len(units) {
		return 0
	}
	if position == 1 {
		return 1
	}
	if isHighSurrogate(units[position-2]) && isLowSurrogate(units[position-1]) {
		return 2
	}
	return 1
}

func (e *executor) wordAt(position int, previous bool, flags Flags) bool {
	if previous {
		if position < 0 {
			return false
		}
		return builtinMatches(classBuiltinWord, rune(e.input.units[position]), flags.IgnoreCase(), flags.unicodeMode())
	}
	char, _, ok := e.codePointAt(position)
	if !ok {
		return false
	}
	return builtinMatches(classBuiltinWord, char, flags.IgnoreCase(), flags.unicodeMode())
}

func (e *executor) codePointAt(position int) (rune, int, bool) {
	if position < 0 {
		return 0, 0, false
	}
	if position >= len(e.input.units) {
		return 0, 0, false
	}
	if e.program.flags.unicodeMode() {
		return codePointAtUnits(e.input.units, position)
	}

	return rune(e.input.units[position]), 1, true
}

func (e *executor) codePointBefore(position int) (rune, int, bool) {
	if position <= 0 {
		return 0, 0, false
	}
	if position > len(e.input.units) {
		return 0, 0, false
	}
	if e.program.flags.unicodeMode() {
		return codePointBeforeUnits(e.input.units, position)
	}
	return rune(e.input.units[position-1]), 1, true
}

func (e *executor) atStart(position int, flags Flags) bool {
	if position == 0 {
		return true
	}
	if !flags.Multiline() {
		return false
	}
	return isLineTerminator(e.input.units[position-1])
}

func (e *executor) atEnd(position int, flags Flags) bool {
	if position == len(e.input.units) {
		return true
	}
	if !flags.Multiline() {
		return false
	}
	return isLineTerminator(e.input.units[position])
}

func isLineTerminator(unit uint16) bool {
	switch unit {
	case '\n', '\r', 0x2028, 0x2029:
		return true
	default:
		return false
	}
}

func isHighSurrogate(unit uint16) bool { return unit&0xFC00 == 0xD800 }
func isLowSurrogate(unit uint16) bool  { return unit&0xFC00 == 0xDC00 }

type characterClass struct {
	node Node
}

func (c characterClass) matches(char rune, flags Flags) bool {
	return classNodeMatches(c.node, char, flags)
}

func classNodeMatches(node Node, char rune, flags Flags) bool {
	switch node.classOp {
	case classOperationUnion:
		return classNodeMatches(node.children[0], char, flags) || classNodeMatches(node.children[1], char, flags)
	case classOperationIntersection:
		return classNodeMatches(node.children[0], char, flags) && classNodeMatches(node.children[1], char, flags)
	case classOperationSubtraction:
		return classNodeMatches(node.children[0], char, flags) && !classNodeMatches(node.children[1], char, flags)
	case classOperationComplement:
		return !classNodeMatches(node.children[0], char, flags)
	}
	for _, value := range node.classStrings {
		if classStringMatches(value, char) {
			return !node.negated
		}
	}
	for _, term := range node.class {
		if classTermMatches(term, char, flags) {
			return !node.negated
		}
	}
	return node.negated
}

func classStringMatches(value []uint16, char rune) bool {
	decoded := utf16.Decode(value)
	if len(decoded) != 1 {
		return false
	}
	if len(utf16.Encode(decoded)) != len(value) {
		return false
	}
	return decoded[0] == char
}

func classTermMatches(term classTerm, char rune, flags Flags) bool {
	if term.property > 0 {
		if term.negated {
			if flags.IgnoreCase() {
				if flags.Unicode() {
					return negatedUnicodePropertyMatches(propertyTableIndex(term.property), char)
				}
			}
		}
		matched := unicodePropertyMatches(propertyTableIndex(term.property), char, flags.IgnoreCase())
		return matched != term.negated
	}
	matched := false
	if term.builtin != classBuiltinNone {
		matched = builtinMatches(term.builtin, char, flags.IgnoreCase(), flags.unicodeMode())
	} else {
		matched = rangeMatches(term.start, term.end, char, flags)
	}
	return matched != term.negated
}

func propertyTableIndex(property uint16) int {
	return int(property) - 1
}

func negatedUnicodePropertyMatches(table int, char rune) bool {
	for _, variant := range unicodeFoldVariants(char) {
		if !unicodePropertyContains(table, variant) {
			return true
		}
	}
	return false
}

func unicodePropertyMatches(table int, char rune, ignoreCase bool) bool {
	if unicodePropertyContains(table, char) {
		return true
	}
	if !ignoreCase {
		return false
	}
	for _, folded := range unicodeFoldVariants(char) {
		if unicodePropertyContains(table, folded) {
			return true
		}
	}
	return false
}

func builtinMatches(builtin classBuiltin, char rune, ignoreCase, unicodeMode bool) bool {
	switch builtin {
	case classBuiltinDigit:
		return isASCIIDigit(char)
	case classBuiltinSpace:
		return isECMAScriptSpace(char)
	case classBuiltinWord:
		if isASCIIWord(char) {
			return true
		}
		if !ignoreCase {
			return false
		}
		if !unicodeMode {
			return false
		}
		for _, folded := range unicodeFoldVariants(char) {
			if isASCIIWord(folded) {
				return true
			}
		}
	}

	return false
}

func isASCIIDigit(char rune) bool { return char >= '0' && char <= '9' }

func isASCIIWord(char rune) bool {
	if char == '_' || isASCIIDigit(char) {
		return true
	}
	if char >= 'A' && char <= 'Z' {
		return true
	}
	return char >= 'a' && char <= 'z'
}

func isECMAScriptSpace(char rune) bool {
	switch char {
	case '\t', '\n', '\v', '\f', '\r', 0xFEFF, 0x2028, 0x2029:
		return true
	default:
		return unicodeGeneralCategoryContains("Space_Separator", char)
	}
}

func rangeMatches(start, end, char rune, flags Flags) bool {
	if runeInRange(char, start, end) {
		return true
	}
	if !flags.IgnoreCase() {
		return false
	}
	if flags.unicodeMode() {
		for _, folded := range unicodeFoldVariants(char) {
			if runeInRange(folded, start, end) {
				return true
			}
		}
		return false
	}
	canonical := legacyCanonical(char)
	if runeInRange(canonical, start, end) {
		return true
	}
	for _, fold := range generatedLegacyUpper {
		if fold.to == canonical && runeInRange(fold.from, start, end) {
			return true
		}
	}

	return false
}

func runeInRange(char, start, end rune) bool {
	return char >= start && char <= end
}
