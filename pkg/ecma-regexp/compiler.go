package ecmascript

import (
	"fmt"
	"slices"
	"sort"
	"unicode/utf16"
)

// CompileLimits bounds the immutable executable representation.
type CompileLimits struct {
	ProgramInstructions uint64
}

// CompileOptions selects parsing and compilation limits.
type CompileOptions struct {
	Parse  ParseOptions
	Limits CompileLimits
}

func DefaultCompileOptions() CompileOptions {
	return CompileOptions{
		Parse:  DefaultParseOptions(),
		Limits: CompileLimits{ProgramInstructions: 1 << 20},
	}
}

// Program is an immutable executable regular expression program.
type Program struct {
	source       string
	edition      Edition
	flags        Flags
	captures     int
	code         []instruction
	classSets    []characterClass
	guardCount   int
	captureNames map[string][]int
}

func (p *Program) Source() string   { return p.source }
func (p *Program) Edition() Edition { return p.edition }
func (p *Program) Flags() Flags     { return p.flags }
func (p *Program) CaptureCount() int {
	return p.captures
}

func (p *Program) CaptureNameIndices() map[string][]int {
	return cloneCaptureNames(p.captureNames)
}

// Compile parses and compiles a pattern without delegating to Go's regexp
// package or embedding a JavaScript runtime.
func Compile(source, flagSource string, options CompileOptions) (*Program, error) {
	flags, err := ParseFlags(flagSource)
	if err != nil {
		return nil, err
	}
	options.Parse.Flags = flags
	pattern, err := Parse(source, options.Parse)
	if err != nil {
		return nil, err
	}

	compiler := compiler{limit: options.Limits.ProgramInstructions, flags: flags}
	if err := compiler.compile(pattern.root, false); err != nil {
		return nil, err
	}
	if _, err := compiler.emit(instruction{op: opMatch}); err != nil {
		return nil, err
	}

	return &Program{
		source:       source,
		edition:      pattern.edition,
		flags:        flags,
		captures:     pattern.captureCount,
		code:         append([]instruction(nil), compiler.code...),
		classSets:    append([]characterClass(nil), compiler.classes...),
		guardCount:   compiler.guards,
		captureNames: cloneCaptureNames(pattern.captureNames),
	}, nil
}

type opcode uint8

const (
	opChar opcode = iota + 1
	opCodePoint
	opAny
	opStart
	opEnd
	opWordBoundary
	opClass
	opBackreference
	opSplit
	opJump
	opSave
	opClear
	opGuard
	opLook
	opAccept
	opMatch
)

type instruction struct {
	op        opcode
	value     uint16
	runeValue rune
	x         int
	y         int
	slot      int
	slots     []int
	flags     Flags
}

type compiler struct {
	code    []instruction
	limit   uint64
	guards  int
	flags   Flags
	classes []characterClass
}

func (c *compiler) compile(node Node, reverse bool) error {
	switch node.kind {
	case NodeEmpty:
		return nil
	case NodeLiteral:
		if c.flags.Unicode() || c.flags.UnicodeSets() {
			characters := decodePatternUnits(node.literalUnits)
			for index := range characters {
				characterIndex := index
				if reverse {
					characterIndex = len(characters) - 1 - index
				}
				if _, err := c.emit(instruction{op: opCodePoint, runeValue: characters[characterIndex]}); err != nil {
					return err
				}
			}
			return nil
		}
		units := node.literalUnits
		for index := range units {
			unitIndex := index
			if reverse {
				unitIndex = len(units) - 1 - index
			}
			if _, err := c.emit(instruction{op: opChar, value: units[unitIndex]}); err != nil {
				return err
			}
		}
		return nil
	case NodeDot:
		_, err := c.emit(instruction{op: opAny})
		return err
	case NodeStartAssertion:
		_, err := c.emit(instruction{op: opStart})
		return err
	case NodeEndAssertion:
		_, err := c.emit(instruction{op: opEnd})
		return err
	case NodeWordBoundary:
		value := uint16(1)
		if node.negated {
			value = 0
		}
		_, err := c.emit(instruction{op: opWordBoundary, value: value})
		return err
	case NodeCharacterClass:
		return c.compileCharacterClass(node, reverse)
	case NodeBackreference:
		_, err := c.emit(instruction{op: opBackreference, slot: node.capture, slots: append([]int(nil), node.backrefs...)})
		return err
	case NodeConcatenation:
		for index := range node.children {
			childIndex := index
			if reverse {
				childIndex = len(node.children) - 1 - index
			}
			if err := c.compile(node.children[childIndex], reverse); err != nil {
				return err
			}
		}
		return nil
	case NodeAlternation:
		return c.alternation(node.children, reverse)
	case NodeGroup:
		if node.capturing {
			slot := node.capture * 2
			if reverse {
				slot++
			}
			if _, err := c.emit(instruction{op: opSave, slot: slot}); err != nil {
				return err
			}
		}
		previousFlags := c.flags
		c.flags.bits = (c.flags.bits | node.enableFlags) &^ node.disableFlags
		if err := c.compile(node.children[0], reverse); err != nil {
			c.flags = previousFlags
			return err
		}
		c.flags = previousFlags
		if node.capturing {
			slot := node.capture*2 + 1
			if reverse {
				slot--
			}
			_, err := c.emit(instruction{op: opSave, slot: slot})
			return err
		}
		return nil
	case NodeLookaround:
		look, err := c.emit(instruction{op: opLook})
		if err != nil {
			return err
		}
		c.code[look].x = len(c.code)
		if err := c.compile(node.children[0], node.behind); err != nil {
			return err
		}
		if _, err := c.emit(instruction{op: opAccept}); err != nil {
			return err
		}
		c.code[look].y = len(c.code)
		if node.negated {
			c.code[look].value |= 1
		}
		if node.behind {
			c.code[look].value |= 2
		}
		return nil
	case NodeQuantifier:
		return c.quantifier(node, reverse)
	default:
		return fmt.Errorf("internal compiler error: unknown node kind %d", node.kind)
	}
}

func (c *compiler) compileCharacterClass(node Node, reverse bool) error {
	classStrings := c.evaluateClassStrings(node)
	sequences := make([][]uint16, len(classStrings))
	for index, sequence := range classStrings {
		sequences[index] = append([]uint16(nil), sequence...)
	}
	sort.Slice(sequences, func(left, right int) bool {
		if len(sequences[left]) != len(sequences[right]) {
			return len(sequences[left]) > len(sequences[right])
		}
		return slices.Compare(sequences[left], sequences[right]) < 0
	})
	jumps := make([]int, 0, len(sequences))
	for _, sequence := range sequences {
		split, err := c.emit(instruction{op: opSplit})
		if err != nil {
			return err
		}
		c.code[split].x = len(c.code)
		characters := utf16.Decode(sequence)
		for index := range characters {
			characterIndex := index
			if reverse {
				characterIndex = len(characters) - 1 - index
			}
			if _, err := c.emit(instruction{op: opCodePoint, runeValue: characters[characterIndex]}); err != nil {
				return err
			}
		}
		jump, err := c.emit(instruction{op: opJump})
		if err != nil {
			return err
		}
		jumps = append(jumps, jump)
		c.code[split].y = len(c.code)
	}
	classIndex := len(c.classes)
	c.classes = append(c.classes, characterClass{node: cloneNode(node)})
	if _, err := c.emit(instruction{op: opClass, x: classIndex}); err != nil {
		return err
	}
	end := len(c.code)
	for _, jump := range jumps {
		c.code[jump].x = end
	}
	return nil
}

func (c *compiler) evaluateClassStrings(node Node) [][]uint16 {
	candidates := make(map[string][]uint16)
	collectClassStrings(node, candidates)
	result := make([][]uint16, 0, len(candidates))
	for _, value := range candidates {
		if classSequenceMatches(node, value, c.flags) {
			result = append(result, value)
		}
	}
	return result
}

func stringSet(values [][]uint16) map[string][]uint16 {
	result := make(map[string][]uint16, len(values))
	for _, value := range values {
		bytes := make([]byte, len(value)*2)
		for index, unit := range value {
			bytes[index*2] = byte(unit >> 8)
			bytes[index*2+1] = byte(unit)
		}
		result[string(bytes)] = value
	}
	return result
}

func collectClassStrings(node Node, values map[string][]uint16) {
	for key, value := range stringSet(node.classStrings) {
		values[key] = value
	}
	for _, child := range node.children {
		collectClassStrings(child, values)
	}
}

func classSequenceMatches(node Node, value []uint16, flags Flags) bool {
	switch node.classOp {
	case classOperationUnion:
		return classSequenceMatches(node.children[0], value, flags) || classSequenceMatches(node.children[1], value, flags)
	case classOperationIntersection:
		return classSequenceMatches(node.children[0], value, flags) && classSequenceMatches(node.children[1], value, flags)
	case classOperationSubtraction:
		return classSequenceMatches(node.children[0], value, flags) && !classSequenceMatches(node.children[1], value, flags)
	case classOperationComplement:
		return false
	}
	for _, candidate := range node.classStrings {
		if equalUnits(candidate, value) {
			return true
		}
	}
	decoded := utf16.Decode(value)
	if len(decoded) == 1 && len(utf16.Encode(decoded)) == len(value) {
		return classNodeMatches(node, decoded[0], flags)
	}
	return false
}

func equalUnits(left, right []uint16) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cloneNode(node Node) Node {
	result := node
	result.children = make([]Node, len(node.children))
	for index, child := range node.children {
		result.children[index] = cloneNode(child)
	}
	result.class = append([]classTerm(nil), node.class...)
	result.literalUnits = append([]uint16(nil), node.literalUnits...)
	result.backrefs = append([]int(nil), node.backrefs...)
	result.classStrings = make([][]uint16, len(node.classStrings))
	for index, value := range node.classStrings {
		result.classStrings[index] = append([]uint16(nil), value...)
	}
	return result
}

func decodePatternUnits(units []uint16) []rune {
	characters := make([]rune, 0, len(units))
	for index := 0; index < len(units); index++ {
		unit := units[index]
		if isHighSurrogate(unit) && index+1 < len(units) && isLowSurrogate(units[index+1]) {
			characters = append(characters, utf16.DecodeRune(rune(unit), rune(units[index+1])))
			index++
			continue
		}
		characters = append(characters, rune(unit))
	}
	return characters
}

func (c *compiler) alternation(branches []Node, reverse bool) error {
	jumps := make([]int, 0, len(branches)-1)
	for index, branch := range branches {
		if index == len(branches)-1 {
			if err := c.compile(branch, reverse); err != nil {
				return err
			}
			break
		}
		split, err := c.emit(instruction{op: opSplit})
		if err != nil {
			return err
		}
		c.code[split].x = len(c.code)
		if err := c.compile(branch, reverse); err != nil {
			return err
		}
		jump, err := c.emit(instruction{op: opJump})
		if err != nil {
			return err
		}
		jumps = append(jumps, jump)
		c.code[split].y = len(c.code)
	}
	end := len(c.code)
	for _, jump := range jumps {
		c.code[jump].x = end
	}

	return nil
}

func (c *compiler) quantifier(node Node, reverse bool) error {
	child := node.children[0]
	for count := 0; count < node.min; count++ {
		if err := c.compileIteration(child, reverse); err != nil {
			return err
		}
	}
	if node.max == node.min {
		return nil
	}
	if node.max < 0 {
		return c.unbounded(child, node.greedy, reverse)
	}
	for count := node.min; count < node.max; count++ {
		split, err := c.emit(instruction{op: opSplit})
		if err != nil {
			return err
		}
		childStart := len(c.code)
		if err := c.compileIteration(child, reverse); err != nil {
			return err
		}
		after := len(c.code)
		c.patchSplit(split, childStart, after, node.greedy)
	}

	return nil
}

func (c *compiler) unbounded(child Node, greedy, reverse bool) error {
	split, err := c.emit(instruction{op: opSplit})
	if err != nil {
		return err
	}
	childStart := len(c.code)
	if err := c.compileIteration(child, reverse); err != nil {
		return err
	}
	guard := c.guards
	c.guards++
	if _, err := c.emit(instruction{op: opGuard, slot: guard}); err != nil {
		return err
	}
	if _, err := c.emit(instruction{op: opJump, x: split}); err != nil {
		return err
	}
	after := len(c.code)
	c.patchSplit(split, childStart, after, greedy)

	return nil
}

func (c *compiler) compileIteration(child Node, reverse bool) error {
	minimum, maximum, ok := captureBounds(child)
	if ok {
		if _, err := c.emit(instruction{op: opClear, x: minimum, y: maximum}); err != nil {
			return err
		}
	}
	return c.compile(child, reverse)
}

func captureBounds(node Node) (int, int, bool) {
	minimum, maximum := 0, 0
	found := false
	if node.kind == NodeGroup && node.capturing {
		minimum, maximum, found = node.capture, node.capture, true
	}
	for _, child := range node.children {
		childMinimum, childMaximum, childFound := captureBounds(child)
		if !childFound {
			continue
		}
		if !found || childMinimum < minimum {
			minimum = childMinimum
		}
		if !found || childMaximum > maximum {
			maximum = childMaximum
		}
		found = true
	}
	return minimum, maximum, found
}

func cloneCaptureNames(source map[string][]int) map[string][]int {
	result := make(map[string][]int, len(source))
	for name, indices := range source {
		result[name] = append([]int(nil), indices...)
	}
	return result
}

func (c *compiler) patchSplit(at, preferred, alternate int, greedy bool) {
	if greedy {
		c.code[at].x, c.code[at].y = preferred, alternate
		return
	}
	c.code[at].x, c.code[at].y = alternate, preferred
}

func (c *compiler) emit(instruction instruction) (int, error) {
	used := uint64(len(c.code) + 1)
	if used > c.limit {
		return 0, &LimitError{Kind: LimitProgramInstructions, Limit: c.limit, Used: used}
	}
	instruction.flags = c.flags
	c.code = append(c.code, instruction)

	return len(c.code) - 1, nil
}
