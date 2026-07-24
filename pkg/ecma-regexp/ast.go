package ecmascript

// NodeKind identifies an immutable syntax-tree node.
type NodeKind uint8

const (
	NodeEmpty NodeKind = iota + 1
	NodeLiteral
	NodeDot
	NodeStartAssertion
	NodeEndAssertion
	NodeWordBoundary
	NodeCharacterClass
	NodeBackreference
	NodeLookaround
	NodeConcatenation
	NodeAlternation
	NodeGroup
	NodeQuantifier
)

// Node is an immutable syntax-tree value. Slice accessors return copies.
type Node struct {
	kind         NodeKind
	span         Span
	text         string
	literalUnits []uint16
	children     []Node
	min          int
	max          int
	greedy       bool
	capturing    bool
	capture      int
	negated      bool
	class        []classTerm
	classStrings [][]uint16
	classOp      classOperation
	name         string
	backrefs     []int
	behind       bool
	enableFlags  uint16
	disableFlags uint16
}

type classOperation uint8

const (
	classOperationNone classOperation = iota
	classOperationUnion
	classOperationIntersection
	classOperationSubtraction
	classOperationComplement
)

func (n Node) Kind() NodeKind { return n.kind }
func (n Node) Span() Span     { return n.span }
func (n Node) Text() string   { return n.text }
func (n Node) Literal() UTF16String {
	return newUTF16String(n.literalUnits)
}
func (n Node) Min() int     { return n.min }
func (n Node) Max() int     { return n.max }
func (n Node) Greedy() bool { return n.greedy }
func (n Node) Capturing() bool {
	return n.capturing
}
func (n Node) CaptureIndex() int { return n.capture }
func (n Node) Negated() bool     { return n.negated }
func (n Node) Name() string      { return n.name }
func (n Node) Lookbehind() bool  { return n.behind }

func (n Node) Children() []Node {
	return append([]Node(nil), n.children...)
}

// CharacterRange is an inclusive code-point range in a character class.
type CharacterRange struct {
	Start rune
	End   rune
}

func (n Node) Ranges() []CharacterRange {
	ranges := make([]CharacterRange, 0, len(n.class))
	for _, term := range n.class {
		if term.builtin == classBuiltinNone {
			ranges = append(ranges, CharacterRange{Start: term.start, End: term.end})
		}
	}

	return ranges
}

func (n Node) ClassStrings() []UTF16String {
	values := make([]UTF16String, len(n.classStrings))
	for index, units := range n.classStrings {
		values[index] = newUTF16String(units)
	}
	return values
}

type classBuiltin uint8

const (
	classBuiltinNone classBuiltin = iota
	classBuiltinDigit
	classBuiltinSpace
	classBuiltinWord
)

type classTerm struct {
	start    rune
	end      rune
	builtin  classBuiltin
	negated  bool
	property uint16
}

// Pattern is an immutable parsed pattern.
type Pattern struct {
	source       string
	edition      Edition
	flags        Flags
	root         Node
	captureCount int
	captureNames map[string][]int
}

func (p *Pattern) Source() string    { return p.source }
func (p *Pattern) Edition() Edition  { return p.edition }
func (p *Pattern) Flags() Flags      { return p.flags }
func (p *Pattern) Root() Node        { return p.root }
func (p *Pattern) CaptureCount() int { return p.captureCount }
func (p *Pattern) CaptureNames() map[string]int {
	names := make(map[string]int, len(p.captureNames))
	for name, indices := range p.captureNames {
		if len(indices) > 0 {
			names[name] = indices[0]
		}
	}
	return names
}

func (p *Pattern) CaptureNameIndices() map[string][]int {
	return cloneCaptureNames(p.captureNames)
}
