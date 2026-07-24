package ecmascript

import (
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ParseLimits bounds parser-controlled resources. Zero is not unlimited.
type ParseLimits struct {
	PatternBytes     uint64
	ASTDepth         uint64
	Captures         uint64
	CharacterClasses uint64
	ASTNodes         uint64
}

// ParseOptions selects the closed edition, flags, and parser limits.
type ParseOptions struct {
	Edition Edition
	Flags   Flags
	AnnexB  bool
	Limits  ParseLimits
}

func DefaultParseOptions() ParseOptions {
	return ParseOptions{
		Edition: Edition2025,
		AnnexB:  true,
		Limits: ParseLimits{
			PatternBytes:     1 << 20,
			ASTDepth:         256,
			Captures:         1 << 12,
			CharacterClasses: 1 << 12,
			ASTNodes:         1 << 20,
		},
	}
}

func (o ParseOptions) validate(pattern string) error {
	if o.Edition != Edition2025 {
		return &SyntaxError{Code: SyntaxUnsupported, Message: "unsupported ECMAScript edition"}
	}
	if uint64(len(pattern)) > o.Limits.PatternBytes {
		return &LimitError{Kind: LimitPatternBytes, Limit: o.Limits.PatternBytes, Used: uint64(len(pattern))}
	}

	return nil
}

// Parse parses a pattern according to the selected closed edition.
func Parse(source string, options ParseOptions) (*Pattern, error) {
	tokens, err := Tokenize(source, options)
	if err != nil {
		return nil, err
	}

	totalCaptures, namedCaptureGroups := scanCaptureMetadata(tokens, options.Flags.UnicodeSets())
	parser := parser{
		source:             source,
		options:            options,
		tokens:             tokens,
		captureNames:       make(map[string][]int),
		totalCaptures:      totalCaptures,
		namedCaptureGroups: namedCaptureGroups,
	}
	root, err := parser.disjunction(0, false)
	if err != nil {
		return nil, err
	}
	if err := validateDuplicateCaptureNames(root); err != nil {
		return nil, err
	}
	if err := resolveBackreferences(&root, parser.captures, parser.captureNames); err != nil {
		return nil, err
	}

	return &Pattern{source: source, edition: options.Edition, flags: options.Flags, root: root, captureCount: parser.captures, captureNames: parser.captureNames}, nil
}

type parser struct {
	source             string
	options            ParseOptions
	tokens             []Token
	position           int
	captures           int
	classes            int
	nodes              uint64
	captureNames       map[string][]int
	totalCaptures      int
	namedCaptureGroups bool
}

func scanCaptureMetadata(tokens []Token, unicodeSets bool) (int, bool) {
	captures := 0
	named := false
	classDepth := 0
	for index, token := range tokens {
		switch token.kind {
		case TokenLeftBracket:
			if classDepth == 0 || unicodeSets {
				classDepth++
			}
		case TokenRightBracket:
			if classDepth > 0 {
				classDepth--
			}
		case TokenLeftParen:
			if classDepth > 0 {
				continue
			}
			if index+1 >= len(tokens) || tokens[index+1].kind != TokenQuestion {
				captures++
				continue
			}
			if index+3 < len(tokens) && tokens[index+2].kind == TokenCharacter && tokens[index+2].text == "<" &&
				(tokens[index+3].kind != TokenCharacter ||
					(tokens[index+3].text != "=" && tokens[index+3].text != "!")) {
				captures++
				named = true
			}
		}
	}
	return captures, named
}

func (p *parser) disjunction(depth uint64, inGroup bool) (Node, error) {
	start := p.current().span.Start
	branches := make([]Node, 0, 2)
	for {
		branch, err := p.alternative(depth, inGroup)
		if err != nil {
			return Node{}, err
		}
		branches = append(branches, branch)
		if p.current().kind != TokenAlternation {
			break
		}
		p.advance()
	}
	end := branches[len(branches)-1].span.End
	if len(branches) == 1 {
		return branches[0], nil
	}

	return p.node(Node{kind: NodeAlternation, span: Span{Start: start, End: end}, children: branches})
}

func (p *parser) alternative(depth uint64, inGroup bool) (Node, error) {
	start := p.current().span.Start
	terms := make([]Node, 0, 4)
	for p.current().kind != TokenEOF && p.current().kind != TokenAlternation && (!inGroup || p.current().kind != TokenRightParen) {
		term, err := p.term(depth)
		if err != nil {
			return Node{}, err
		}
		if len(terms) > 0 && terms[len(terms)-1].kind == NodeLiteral && term.kind == NodeLiteral {
			previous := &terms[len(terms)-1]
			previous.span.End = term.span.End
			previous.text += term.text
			previous.literalUnits = append(previous.literalUnits, term.literalUnits...)
			continue
		}
		terms = append(terms, term)
	}
	if len(terms) == 0 {
		return p.node(Node{kind: NodeEmpty, span: Span{Start: start, End: start}})
	}
	if len(terms) == 1 {
		return terms[0], nil
	}

	return p.node(Node{kind: NodeConcatenation, span: Span{Start: terms[0].span.Start, End: terms[len(terms)-1].span.End}, children: terms})
}

func (p *parser) term(depth uint64) (Node, error) {
	atom, err := p.atom(depth)
	if err != nil {
		return Node{}, err
	}

	minCount, maxCount, end, quantified, err := p.quantifier()
	if err != nil {
		return Node{}, err
	}
	if !quantified {
		return atom, nil
	}
	if atom.kind == NodeLookaround && (atom.behind || p.options.Flags.Unicode() || p.options.Flags.UnicodeSets()) {
		return Node{}, p.syntax(SyntaxInvalidQuantifier, atom.span, "assertion cannot be quantified")
	}
	greedy := true
	if p.current().kind == TokenQuestion {
		greedy = false
		end = p.current().span.End
		p.advance()
	}

	return p.node(Node{kind: NodeQuantifier, span: Span{Start: atom.span.Start, End: end}, children: []Node{atom}, min: minCount, max: maxCount, greedy: greedy})
}

func (p *parser) atom(depth uint64) (Node, error) {
	token := p.current()
	switch token.kind {
	case TokenCharacter, TokenComma:
		p.advance()
		return p.literalText(token.span, token.text)
	case TokenEscape:
		return p.escape(false)
	case TokenDot:
		p.advance()
		return p.node(Node{kind: NodeDot, span: token.span})
	case TokenCaret:
		p.advance()
		return p.node(Node{kind: NodeStartAssertion, span: token.span})
	case TokenDollar:
		p.advance()
		return p.node(Node{kind: NodeEndAssertion, span: token.span})
	case TokenLeftParen:
		return p.group(depth)
	case TokenRightParen:
		return Node{}, p.syntax(SyntaxUnexpectedToken, token.span, "unmatched closing parenthesis")
	case TokenStar, TokenPlus, TokenQuestion:
		return Node{}, p.syntax(SyntaxInvalidQuantifier, token.span, "quantifier has no preceding atom")
	case TokenLeftBrace:
		if p.options.AnnexB && !p.options.Flags.Unicode() && !p.options.Flags.UnicodeSets() && !p.looksLikeInvalidBracedQuantifier() {
			p.advance()
			return p.literalText(token.span, token.text)
		}
		return Node{}, p.syntax(SyntaxInvalidQuantifier, token.span, "quantifier has no preceding atom")
	case TokenRightBrace:
		if p.options.AnnexB && !p.options.Flags.Unicode() && !p.options.Flags.UnicodeSets() {
			p.advance()
			return p.literalText(token.span, token.text)
		}
		return Node{}, p.syntax(SyntaxUnexpectedToken, token.span, "unexpected token")
	case TokenRightBracket:
		if p.options.AnnexB && !p.options.Flags.Unicode() && !p.options.Flags.UnicodeSets() {
			p.advance()
			return p.literalText(token.span, token.text)
		}
		return Node{}, p.syntax(SyntaxUnexpectedToken, token.span, "unexpected token")
	case TokenLeftBracket:
		return p.characterClass(depth)
	default:
		return Node{}, p.syntax(SyntaxUnexpectedToken, token.span, "unexpected token")
	}
}

func (p *parser) escape(inClass bool) (Node, error) {
	token := p.current()
	p.advance()
	escaped := strings.TrimPrefix(token.text, "\\")
	char, _ := utf8.DecodeRuneInString(escaped)
	switch char {
	case 'p', 'P':
		return p.propertyEscape(token, char == 'P')
	case 'd', 'D', 's', 'S', 'w', 'W':
		builtin := classBuiltinDigit
		switch char {
		case 's', 'S':
			builtin = classBuiltinSpace
		case 'w', 'W':
			builtin = classBuiltinWord
		}
		return p.node(Node{kind: NodeCharacterClass, span: token.span, class: []classTerm{{builtin: builtin, negated: char == 'D' || char == 'S' || char == 'W'}}})
	case 'b':
		if inClass {
			return p.literal(token.span, '\b')
		}
		return p.node(Node{kind: NodeWordBoundary, span: token.span})
	case 'B':
		if inClass {
			return Node{}, p.syntax(SyntaxInvalidEscape, token.span, "invalid character class escape")
		}
		return p.node(Node{kind: NodeWordBoundary, span: token.span, negated: true})
	case 'f':
		return p.literal(token.span, '\f')
	case 'n':
		return p.literal(token.span, '\n')
	case 'r':
		return p.literal(token.span, '\r')
	case 't':
		return p.literal(token.span, '\t')
	case 'v':
		return p.literal(token.span, '\v')
	case '0':
		if !p.isDecimal(p.current()) {
			return p.literal(token.span, 0)
		}
		if p.options.Flags.Unicode() || p.options.Flags.UnicodeSets() || !p.options.AnnexB {
			return Node{}, p.syntax(SyntaxInvalidEscape, token.span, "legacy octal escape is not enabled")
		}
		return p.legacyOctalEscape(token, '0')
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		if inClass && (p.options.Flags.Unicode() || p.options.Flags.UnicodeSets()) {
			return Node{}, p.syntax(SyntaxInvalidEscape, token.span, "decimal escapes are invalid in a character class")
		}
		valueText := string(char)
		for offset := p.position; offset < len(p.tokens) && p.isDecimal(p.tokens[offset]); offset++ {
			valueText += p.tokens[offset].text
		}
		value, valueErr := strconv.ParseUint(valueText, 10, 64)
		if !inClass && valueErr == nil && value > 0 && value <= uint64(p.totalCaptures) {
			end := p.consumeDecimalEscape(token.span.End)
			return p.node(Node{kind: NodeBackreference, span: Span{Start: token.span.Start, End: end}, capture: int(value)})
		}
		if p.options.Flags.Unicode() || p.options.Flags.UnicodeSets() {
			end := p.consumeDecimalEscape(token.span.End)
			if valueErr != nil || value > uint64(^uint(0)>>1) {
				return Node{}, p.syntax(SyntaxInvalidBackreference, Span{Start: token.span.Start, End: end}, "backreference is too large")
			}
			return p.node(Node{kind: NodeBackreference, span: Span{Start: token.span.Start, End: end}, capture: int(value)})
		}
		if !p.options.AnnexB {
			end := p.consumeDecimalEscape(token.span.End)
			return Node{}, p.syntax(SyntaxInvalidBackreference, Span{Start: token.span.Start, End: end}, "backreference is too large")
		}
		if char == '8' || char == '9' {
			return p.literal(token.span, char)
		}
		return p.legacyOctalEscape(token, char)
	case 'x':
		return p.hexEscape(token, 2)
	case 'u':
		if p.current().kind == TokenLeftBrace {
			if !p.options.Flags.Unicode() && !p.options.Flags.UnicodeSets() {
				return Node{}, p.syntax(SyntaxInvalidEscape, token.span, "code point escape requires u or v mode")
			}
			return p.codePointEscape(token)
		}
		return p.hexEscape(token, 4)
	case 'c':
		validControl := p.current().kind == TokenCharacter && len(p.current().text) == 1 &&
			((p.current().text[0] >= 'A' && p.current().text[0] <= 'Z') ||
				(p.current().text[0] >= 'a' && p.current().text[0] <= 'z'))
		if inClass && p.options.AnnexB && !p.options.Flags.Unicode() && !p.options.Flags.UnicodeSets() &&
			p.current().kind == TokenCharacter && len(p.current().text) == 1 &&
			(p.isDecimal(p.current()) || p.current().text == "_") {
			validControl = true
		}
		if !validControl {
			if p.options.AnnexB && !p.options.Flags.Unicode() && !p.options.Flags.UnicodeSets() {
				p.insertEscapedCharacter(token, 'c')
				return p.literal(Span{Start: token.span.Start, End: token.span.Start + 1}, '\\')
			}
			return Node{}, p.syntax(SyntaxInvalidEscape, token.span, "control escape requires an ASCII letter")
		}
		letter := p.current()
		p.advance()
		return p.literal(Span{Start: token.span.Start, End: letter.span.End}, rune(letter.text[0]&31))
	case 'k':
		if !inClass && p.current().kind == TokenCharacter && p.current().text == "<" &&
			(p.namedCaptureGroups || p.options.Flags.Unicode() || p.options.Flags.UnicodeSets()) {
			name, end, err := p.captureName()
			if err != nil {
				return Node{}, err
			}
			return p.node(Node{kind: NodeBackreference, span: Span{Start: token.span.Start, End: end}, name: name})
		}
		if p.options.AnnexB && !p.options.Flags.Unicode() && !p.options.Flags.UnicodeSets() && !p.namedCaptureGroups {
			return p.literal(token.span, 'k')
		}
		if inClass || p.current().kind != TokenCharacter || p.current().text != "<" {
			return Node{}, p.syntax(SyntaxInvalidBackreference, token.span, "named backreference requires an identifier")
		}
		return Node{}, p.syntax(SyntaxInvalidBackreference, token.span, "named backreference is not enabled")
	default:
		unicodeIdentity := strings.ContainsRune("^$\\.*+?()[]{}|/", char)
		unicodeSetIdentity := inClass && p.options.Flags.UnicodeSets() && strings.ContainsRune("!#%&,-:;<=>@`~", char)
		if (p.options.Flags.Unicode() || p.options.Flags.UnicodeSets()) && !unicodeIdentity && !unicodeSetIdentity {
			return Node{}, p.syntax(SyntaxInvalidEscape, token.span, "identity escape is invalid in Unicode mode")
		}
		return p.literal(token.span, char)
	}
}

func (p *parser) insertEscapedCharacter(token Token, char rune) {
	insert := Token{
		kind: TokenCharacter,
		text: string(char),
		span: Span{Start: token.span.End - 1, End: token.span.End},
	}
	p.tokens = append(p.tokens, Token{})
	copy(p.tokens[p.position+1:], p.tokens[p.position:])
	p.tokens[p.position] = insert
}

func (p *parser) looksLikeInvalidBracedQuantifier() bool {
	position := p.position + 1
	start := position
	for position < len(p.tokens) && p.isDecimal(p.tokens[position]) {
		position++
	}
	if position == start {
		return false
	}
	if p.tokens[position].kind == TokenRightBrace {
		return true
	}
	if p.tokens[position].kind != TokenComma {
		return false
	}
	position++
	for position < len(p.tokens) && p.isDecimal(p.tokens[position]) {
		position++
	}
	return position < len(p.tokens) && p.tokens[position].kind == TokenRightBrace
}

func (p *parser) consumeDecimalEscape(end int) int {
	for p.isDecimal(p.current()) {
		end = p.current().span.End
		p.advance()
	}
	return end
}

func (p *parser) legacyOctalEscape(prefix Token, first rune) (Node, error) {
	digits := []byte{byte(first)}
	maximum := 2
	if first <= '3' {
		maximum = 3
	}
	end := prefix.span.End
	for len(digits) < maximum && p.current().kind == TokenCharacter && len(p.current().text) == 1 &&
		p.current().text[0] >= '0' && p.current().text[0] <= '7' {
		digits = append(digits, p.current().text[0])
		end = p.current().span.End
		p.advance()
	}
	value, _ := strconv.ParseUint(string(digits), 8, 8)
	return p.literal(Span{Start: prefix.span.Start, End: end}, rune(value))
}

func (p *parser) characterClass(depth uint64) (Node, error) {
	if p.options.Flags.UnicodeSets() {
		return p.unicodeSetClass(depth)
	}
	open := p.current()
	p.advance()
	p.classes++
	if uint64(p.classes) > p.options.Limits.CharacterClasses {
		return Node{}, &LimitError{Kind: LimitCharacterClasses, Limit: p.options.Limits.CharacterClasses, Used: uint64(p.classes)}
	}
	negated := false
	if p.current().kind == TokenCaret {
		negated = true
		p.advance()
	}
	terms := make([]classTerm, 0, 4)
	for p.current().kind != TokenRightBracket {
		if p.current().kind == TokenEOF {
			return Node{}, p.syntax(SyntaxUnexpectedEOF, Span{Start: open.span.Start, End: p.current().span.End}, "character class is not closed")
		}
		term, span, err := p.classItem()
		if err != nil {
			return Node{}, err
		}
		if p.current().kind == TokenCharacter && p.current().text == "-" && p.peek().kind != TokenRightBracket {
			p.advance()
			endTerm, endSpan, err := p.classItem()
			if err != nil {
				return Node{}, err
			}
			startIsSet := term.builtin != classBuiltinNone || term.property > 0
			endIsSet := endTerm.builtin != classBuiltinNone || endTerm.property > 0
			if startIsSet || endIsSet {
				if !p.options.AnnexB || p.options.Flags.Unicode() || p.options.Flags.UnicodeSets() {
					return Node{}, p.syntax(SyntaxInvalidEscape, Span{Start: span.Start, End: endSpan.End}, "character class range contains a set escape")
				}
				terms = append(terms, term, classTerm{start: '-', end: '-'}, endTerm)
				continue
			}
			if term.start > endTerm.start {
				return Node{}, p.syntax(SyntaxInvalidEscape, Span{Start: span.Start, End: endSpan.End}, "invalid character class range")
			}
			term.end = endTerm.start
		}
		terms = append(terms, term)
	}
	close := p.current()
	p.advance()

	return p.node(Node{kind: NodeCharacterClass, span: Span{Start: open.span.Start, End: close.span.End}, negated: negated, class: terms})
}

func (p *parser) unicodeSetClass(depth uint64) (Node, error) {
	open := p.current()
	p.advance()
	depth++
	if depth > p.options.Limits.ASTDepth {
		return Node{}, &LimitError{Kind: LimitASTDepth, Limit: p.options.Limits.ASTDepth, Used: depth}
	}
	p.classes++
	if uint64(p.classes) > p.options.Limits.CharacterClasses {
		return Node{}, &LimitError{Kind: LimitCharacterClasses, Limit: p.options.Limits.CharacterClasses, Used: uint64(p.classes)}
	}
	complement := false
	if p.current().kind == TokenCaret {
		complement = true
		p.advance()
	}
	left, count, err := p.unicodeSetUnion(depth)
	if err != nil {
		return Node{}, err
	}
	var operation classOperation
	for currentOperation := p.unicodeSetOperator(); currentOperation != classOperationNone; currentOperation = p.unicodeSetOperator() {
		if operation != classOperationNone && operation != currentOperation {
			return Node{}, p.syntax(SyntaxUnexpectedToken, p.current().span, "Unicode Sets intersection and subtraction cannot be mixed")
		}
		operation = currentOperation
		p.advance()
		p.advance()
		right, rightCount, err := p.unicodeSetUnion(depth)
		if err != nil {
			return Node{}, err
		}
		if count == 0 || rightCount == 0 {
			return Node{}, p.syntax(SyntaxUnexpectedToken, p.current().span, "Unicode Sets operator requires two operands")
		}
		left, err = p.node(Node{kind: NodeCharacterClass, span: Span{Start: left.span.Start, End: right.span.End}, classOp: operation, children: []Node{left, right}})
		if err != nil {
			return Node{}, err
		}
		count = 1
	}
	if p.current().kind != TokenRightBracket {
		return Node{}, p.syntax(SyntaxUnexpectedToken, p.current().span, "invalid Unicode Sets character")
	}
	close := p.current()
	p.advance()
	if count == 0 {
		left, err = p.node(Node{kind: NodeCharacterClass, span: Span{Start: open.span.Start, End: close.span.End}})
		if err != nil {
			return Node{}, err
		}
	}
	left.span = Span{Start: open.span.Start, End: close.span.End}
	if complement {
		if classHasStrings(left) {
			return Node{}, p.syntax(SyntaxInvalidEscape, left.span, "a Unicode string set cannot be complemented")
		}
		left, err = p.node(Node{kind: NodeCharacterClass, span: left.span, classOp: classOperationComplement, children: []Node{left}})
		if err != nil {
			return Node{}, err
		}
	}
	return left, nil
}

func (p *parser) unicodeSetUnion(depth uint64) (Node, int, error) {
	items := make([]Node, 0, 4)
	for p.current().kind != TokenEOF && p.current().kind != TokenRightBracket && p.unicodeSetOperator() == classOperationNone {
		if p.isUnicodeSetDoubleReserved() {
			return Node{}, 0, p.syntax(SyntaxUnexpectedToken, Span{Start: p.current().span.Start, End: p.peek().span.End}, "reserved Unicode Sets punctuator must be escaped")
		}
		item, err := p.unicodeSetAtom(depth)
		if err != nil {
			return Node{}, 0, err
		}
		if p.current().kind == TokenCharacter && p.current().text == "-" && p.peek().text != "-" {
			if !singleClassCharacter(item) {
				return Node{}, 0, p.syntax(SyntaxUnexpectedToken, p.current().span, "Unicode Sets range requires character endpoints")
			}
			p.advance()
			end, err := p.unicodeSetAtom(depth)
			if err != nil {
				return Node{}, 0, err
			}
			if !singleClassCharacter(end) || item.class[0].start > end.class[0].start {
				return Node{}, 0, p.syntax(SyntaxUnexpectedToken, Span{Start: item.span.Start, End: end.span.End}, "invalid Unicode Sets range")
			}
			item.class[0].end = end.class[0].start
			item.span.End = end.span.End
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return Node{}, 0, nil
	}
	result := items[0]
	for _, item := range items[1:] {
		var err error
		result, err = p.node(Node{kind: NodeCharacterClass, span: Span{Start: result.span.Start, End: item.span.End}, classOp: classOperationUnion, children: []Node{result, item}})
		if err != nil {
			return Node{}, 0, err
		}
	}
	return result, len(items), nil
}

func (p *parser) unicodeSetAtom(depth uint64) (Node, error) {
	token := p.current()
	if token.kind == TokenLeftBracket {
		return p.unicodeSetClass(depth)
	}
	if token.kind == TokenEscape {
		if token.text == `\q` {
			return p.classStringDisjunction()
		}
		node, err := p.escape(true)
		if err != nil {
			return Node{}, err
		}
		if node.kind == NodeLiteral {
			char := nodeLiteralRune(node)
			return p.node(Node{kind: NodeCharacterClass, span: node.span, class: []classTerm{{start: char, end: char}}})
		}
		return node, nil
	}
	if isUnicodeSetReservedSingle(token.text) {
		return Node{}, p.syntax(SyntaxUnexpectedToken, token.span, "reserved Unicode Sets punctuator must be escaped")
	}
	if token.kind == TokenRightBracket || token.kind == TokenEOF {
		return Node{}, p.syntax(SyntaxUnexpectedToken, token.span, "expected Unicode Sets operand")
	}
	p.advance()
	char, _ := utf8.DecodeRuneInString(token.text)
	return p.node(Node{kind: NodeCharacterClass, span: token.span, class: []classTerm{{start: char, end: char}}})
}

func (p *parser) classStringDisjunction() (Node, error) {
	prefix := p.current()
	p.advance()
	if p.current().kind != TokenLeftBrace {
		return Node{}, p.syntax(SyntaxInvalidEscape, prefix.span, "class string disjunction requires braces")
	}
	p.advance()
	values := make([][]uint16, 0, 2)
	current := make([]uint16, 0, 4)
	for p.current().kind != TokenRightBrace {
		if p.current().kind == TokenEOF {
			return Node{}, p.syntax(SyntaxUnexpectedEOF, Span{Start: prefix.span.Start, End: p.current().span.End}, "class string disjunction is not closed")
		}
		if p.current().kind == TokenAlternation {
			values = append(values, current)
			current = nil
			p.advance()
			continue
		}
		if isUnicodeSetReservedSingle(p.current().text) && p.current().kind != TokenEscape {
			return Node{}, p.syntax(SyntaxUnexpectedToken, p.current().span, "reserved class string punctuator must be escaped")
		}
		if p.current().kind == TokenEscape {
			node, err := p.escape(true)
			if err != nil {
				return Node{}, err
			}
			if node.kind != NodeLiteral {
				return Node{}, p.syntax(SyntaxInvalidEscape, node.span, "class string escape must denote one character")
			}
			current = append(current, node.literalUnits...)
		} else {
			current = append(current, utf16.Encode([]rune(p.current().text))...)
			p.advance()
		}
	}
	close := p.current()
	p.advance()
	values = append(values, current)
	return p.node(Node{kind: NodeCharacterClass, span: Span{Start: prefix.span.Start, End: close.span.End}, classStrings: values})
}

func (p *parser) unicodeSetOperator() classOperation {
	if p.current().kind != TokenCharacter || p.peek().kind != TokenCharacter || p.current().text != p.peek().text {
		return classOperationNone
	}
	if p.current().text == "&" {
		return classOperationIntersection
	}
	if p.current().text == "-" {
		return classOperationSubtraction
	}
	return classOperationNone
}

func (p *parser) isUnicodeSetDoubleReserved() bool {
	return p.current().text == p.peek().text && strings.Contains("!#$%&*+,.:;<=>?@^`~", p.current().text)
}

func isUnicodeSetReservedSingle(text string) bool {
	return len(text) == 1 && strings.Contains("()[]{}/-|", text)
}

func singleClassCharacter(node Node) bool {
	return node.classOp == classOperationNone && len(node.class) == 1 && node.class[0].builtin == classBuiltinNone && node.class[0].property == 0 && len(node.classStrings) == 0
}

func classHasStrings(node Node) bool {
	if len(node.classStrings) > 0 {
		return true
	}
	for _, child := range node.children {
		if classHasStrings(child) {
			return true
		}
	}
	return false
}

func (p *parser) propertyEscape(prefix Token, negated bool) (Node, error) {
	if !p.options.Flags.Unicode() && !p.options.Flags.UnicodeSets() {
		return Node{}, p.syntax(SyntaxInvalidEscape, prefix.span, "Unicode property escape requires u or v mode")
	}
	if p.current().kind != TokenLeftBrace {
		return Node{}, p.syntax(SyntaxInvalidEscape, prefix.span, "Unicode property escape requires braces")
	}
	p.advance()
	var expression strings.Builder
	for p.current().kind != TokenRightBrace {
		token := p.current()
		if token.kind == TokenEOF || token.kind == TokenEscape || len(token.text) != 1 {
			return Node{}, p.syntax(SyntaxInvalidEscape, Span{Start: prefix.span.Start, End: token.span.End}, "invalid Unicode property expression")
		}
		expression.WriteString(token.text)
		p.advance()
	}
	if expression.Len() == 0 {
		return Node{}, p.syntax(SyntaxInvalidEscape, Span{Start: prefix.span.Start, End: p.current().span.End}, "empty Unicode property expression")
	}
	end := p.current().span.End
	p.advance()
	table, ok := lookupUnicodeProperty(expression.String())
	if !ok {
		stringsInProperty, stringProperty := lookupUnicodeStringProperty(expression.String())
		if !stringProperty || !p.options.Flags.UnicodeSets() {
			return Node{}, p.syntax(SyntaxInvalidEscape, Span{Start: prefix.span.Start, End: end}, "unsupported Unicode property or value")
		}
		if negated {
			return Node{}, p.syntax(SyntaxInvalidEscape, Span{Start: prefix.span.Start, End: end}, "a Unicode string property cannot be complemented")
		}
		p.classes++
		if uint64(p.classes) > p.options.Limits.CharacterClasses {
			return Node{}, &LimitError{Kind: LimitCharacterClasses, Limit: p.options.Limits.CharacterClasses, Used: uint64(p.classes)}
		}
		return p.node(Node{kind: NodeCharacterClass, span: Span{Start: prefix.span.Start, End: end}, classStrings: stringsInProperty})
	}
	p.classes++
	if uint64(p.classes) > p.options.Limits.CharacterClasses {
		return Node{}, &LimitError{Kind: LimitCharacterClasses, Limit: p.options.Limits.CharacterClasses, Used: uint64(p.classes)}
	}

	return p.node(Node{kind: NodeCharacterClass, span: Span{Start: prefix.span.Start, End: end}, class: []classTerm{{property: uint16(table + 1), negated: negated}}})
}

func (p *parser) classItem() (classTerm, Span, error) {
	token := p.current()
	if token.kind == TokenEscape {
		node, err := p.escape(true)
		if err != nil {
			return classTerm{}, Span{}, err
		}
		if node.kind == NodeCharacterClass {
			return node.class[0], node.span, nil
		}
		if (p.options.Flags.Unicode() || p.options.Flags.UnicodeSets()) &&
			len(node.literalUnits) == 1 && isHighSurrogate(node.literalUnits[0]) &&
			p.current().kind == TokenEscape && p.current().text == `\u` {
			position := p.position
			low, lowErr := p.escape(true)
			if lowErr != nil {
				return classTerm{}, Span{}, lowErr
			}
			if low.kind == NodeLiteral && len(low.literalUnits) == 1 &&
				isLowSurrogate(low.literalUnits[0]) {
				char := utf16.DecodeRune(
					rune(node.literalUnits[0]),
					rune(low.literalUnits[0]),
				)
				return classTerm{start: char, end: char},
					Span{Start: node.span.Start, End: low.span.End}, nil
			}
			p.position = position
		}
		char := nodeLiteralRune(node)
		return classTerm{start: char, end: char}, node.span, nil
	}
	if token.kind == TokenRightBracket || token.kind == TokenEOF {
		return classTerm{}, Span{}, p.syntax(SyntaxUnexpectedToken, token.span, "expected character class item")
	}
	p.advance()
	char, _ := utf8.DecodeRuneInString(token.text)
	return classTerm{start: char, end: char}, token.span, nil
}

func (p *parser) hexEscape(prefix Token, digits int) (Node, error) {
	start := prefix.span.Start
	end := prefix.span.End
	var text strings.Builder
	for range digits {
		token := p.current()
		if token.kind != TokenCharacter || len(token.text) != 1 || !isHex(token.text[0]) {
			return Node{}, p.syntax(SyntaxInvalidEscape, Span{Start: start, End: end}, "hex escape has invalid digits")
		}
		text.WriteString(token.text)
		end = token.span.End
		p.advance()
	}
	value, _ := strconv.ParseUint(text.String(), 16, 32)

	return p.literal(Span{Start: start, End: end}, rune(value))
}

func (p *parser) codePointEscape(prefix Token) (Node, error) {
	start := prefix.span.Start
	p.advance()
	var text strings.Builder
	for p.current().kind != TokenRightBrace {
		token := p.current()
		if token.kind != TokenCharacter || len(token.text) != 1 || !isHex(token.text[0]) {
			return Node{}, p.syntax(SyntaxInvalidEscape, Span{Start: start, End: token.span.End}, "invalid Unicode code point escape")
		}
		text.WriteString(token.text)
		p.advance()
	}
	if text.Len() == 0 {
		return Node{}, p.syntax(SyntaxInvalidEscape, Span{Start: start, End: p.current().span.End}, "empty Unicode code point escape")
	}
	end := p.current().span.End
	p.advance()
	value, valueErr := strconv.ParseUint(text.String(), 16, 32)
	if valueErr != nil || value > utf8.MaxRune || value >= 0xD800 && value <= 0xDFFF {
		return Node{}, p.syntax(SyntaxInvalidEscape, Span{Start: start, End: end}, "Unicode code point escape is out of range")
	}

	return p.literal(Span{Start: start, End: end}, rune(value))
}

func (p *parser) literal(span Span, char rune) (Node, error) {
	units := []uint16{uint16(char)}
	if char > 0xFFFF {
		units = utf16.Encode([]rune{char})
	}
	return p.node(Node{kind: NodeLiteral, span: span, text: string(utf16.Decode(units)), literalUnits: units})
}

func (p *parser) literalText(span Span, text string) (Node, error) {
	return p.node(Node{kind: NodeLiteral, span: span, text: text, literalUnits: utf16.Encode([]rune(text))})
}

func nodeLiteralRune(node Node) rune {
	if len(node.literalUnits) == 1 {
		return rune(node.literalUnits[0])
	}
	decoded := utf16.Decode(node.literalUnits)
	if len(decoded) == 1 {
		return decoded[0]
	}
	return utf8.RuneError
}

func (p *parser) isDecimal(token Token) bool {
	return token.kind == TokenCharacter && token.text >= "0" && token.text <= "9"
}

func (p *parser) peek() Token {
	if p.position+1 >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[p.position+1]
}

func isHex(char byte) bool {
	return char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F'
}

func resolveBackreferences(node *Node, captures int, names map[string][]int) error {
	if node.kind == NodeBackreference {
		if node.name != "" {
			indices := names[node.name]
			if len(indices) == 0 {
				return &SyntaxError{Code: SyntaxInvalidBackreference, Span: node.span, Message: "named backreference does not identify a capture"}
			}
			node.capture = indices[0]
			node.backrefs = append([]int(nil), indices...)
		} else if node.capture > captures {
			return &SyntaxError{Code: SyntaxInvalidBackreference, Span: node.span, Message: "backreference does not identify a capture"}
		} else {
			node.backrefs = []int{node.capture}
		}
	}
	for index := range node.children {
		if err := resolveBackreferences(&node.children[index], captures, names); err != nil {
			return err
		}
	}
	return nil
}

type namedCaptureOccurrence struct {
	span    Span
	choices map[int]int
}

func validateDuplicateCaptureNames(root Node) error {
	byName := make(map[string][]namedCaptureOccurrence)
	nextAlternation := 0
	collectNamedCaptures(root, nil, &nextAlternation, byName)
	for _, occurrences := range byName {
		for left := 0; left < len(occurrences); left++ {
			for right := left + 1; right < len(occurrences); right++ {
				if namedCapturesMightBothParticipate(occurrences[left], occurrences[right]) {
					return &SyntaxError{
						Code:    SyntaxUnexpectedToken,
						Span:    occurrences[right].span,
						Message: "duplicate capture names might both participate",
					}
				}
			}
		}
	}
	return nil
}

func collectNamedCaptures(node Node, choices map[int]int, nextAlternation *int, byName map[string][]namedCaptureOccurrence) {
	if node.kind == NodeGroup && node.name != "" {
		byName[node.name] = append(byName[node.name], namedCaptureOccurrence{span: node.span, choices: cloneChoices(choices)})
	}
	if node.kind == NodeAlternation {
		alternation := *nextAlternation
		(*nextAlternation)++
		for branch, child := range node.children {
			branchChoices := cloneChoices(choices)
			branchChoices[alternation] = branch
			collectNamedCaptures(child, branchChoices, nextAlternation, byName)
		}
		return
	}
	for _, child := range node.children {
		collectNamedCaptures(child, choices, nextAlternation, byName)
	}
}

func namedCapturesMightBothParticipate(left, right namedCaptureOccurrence) bool {
	for alternation, leftBranch := range left.choices {
		if rightBranch, ok := right.choices[alternation]; ok && leftBranch != rightBranch {
			return false
		}
	}
	return true
}

func cloneChoices(source map[int]int) map[int]int {
	result := make(map[int]int, len(source))
	for alternation, branch := range source {
		result[alternation] = branch
	}
	return result
}

func (p *parser) group(depth uint64) (Node, error) {
	open := p.current()
	p.advance()
	depth++
	if depth > p.options.Limits.ASTDepth {
		return Node{}, &LimitError{Kind: LimitASTDepth, Limit: p.options.Limits.ASTDepth, Used: depth}
	}

	capturing := true
	lookaround := false
	negative := false
	behind := false
	name := ""
	var enableFlags, disableFlags uint16
	if p.current().kind == TokenQuestion {
		question := p.current()
		p.advance()
		if p.current().kind != TokenCharacter {
			return Node{}, p.syntax(SyntaxUnexpectedToken, Span{Start: question.span.Start, End: p.current().span.End}, "invalid group extension")
		}
		switch p.current().text {
		case ":":
			capturing = false
			p.advance()
		case "=", "!":
			capturing = false
			lookaround = true
			negative = p.current().text == "!"
			p.advance()
		case "<":
			p.advance()
			if p.current().kind == TokenCharacter && (p.current().text == "=" || p.current().text == "!") {
				capturing = false
				lookaround = true
				behind = true
				negative = p.current().text == "!"
				p.advance()
			} else {
				var err error
				name, _, err = p.captureNameBody()
				if err != nil {
					return Node{}, err
				}
			}
		default:
			if strings.Contains("ims-", p.current().text) {
				var err error
				enableFlags, disableFlags, err = p.modifierFlags()
				if err != nil {
					return Node{}, err
				}
				capturing = false
			} else {
				return Node{}, p.syntax(SyntaxUnsupported, Span{Start: question.span.Start, End: p.current().span.End}, "group extension is not supported")
			}
		}
	}
	capture := 0
	if capturing {
		p.captures++
		capture = p.captures
		if uint64(p.captures) > p.options.Limits.Captures {
			return Node{}, &LimitError{Kind: LimitCaptures, Limit: p.options.Limits.Captures, Used: uint64(p.captures)}
		}
		if name != "" {
			p.captureNames[name] = append(p.captureNames[name], capture)
		}
	}

	child, err := p.disjunction(depth, true)
	if err != nil {
		return Node{}, err
	}
	if p.current().kind != TokenRightParen {
		return Node{}, p.syntax(SyntaxUnclosedGroup, Span{Start: open.span.Start, End: p.current().span.End}, "group is not closed")
	}
	close := p.current()
	p.advance()

	kind := NodeGroup
	if lookaround {
		kind = NodeLookaround
	}
	return p.node(Node{kind: kind, span: Span{Start: open.span.Start, End: close.span.End}, children: []Node{child}, capturing: capturing, capture: capture, name: name, negated: negative, behind: behind, enableFlags: enableFlags, disableFlags: disableFlags})
}

func (p *parser) modifierFlags() (uint16, uint16, error) {
	var enabled, disabled uint16
	disabling := false
	hasEnabled, hasDisabled := false, false
	for p.current().kind != TokenCharacter || p.current().text != ":" {
		token := p.current()
		if token.kind != TokenCharacter || len(token.text) != 1 {
			return 0, 0, p.syntax(SyntaxUnexpectedToken, token.span, "invalid inline modifier")
		}
		if token.text == "-" {
			if disabling {
				return 0, 0, p.syntax(SyntaxUnexpectedToken, token.span, "duplicate inline modifier separator")
			}
			disabling = true
			p.advance()
			continue
		}
		if !strings.Contains("ims", token.text) {
			return 0, 0, p.syntax(SyntaxUnsupported, token.span, "flag cannot be changed by an inline modifier")
		}
		bit, _ := flagBit(rune(token.text[0]))
		if enabled&bit != 0 || disabled&bit != 0 {
			return 0, 0, p.syntax(SyntaxUnexpectedToken, token.span, "duplicate inline modifier flag")
		}
		if disabling {
			disabled |= bit
			hasDisabled = true
		} else {
			enabled |= bit
			hasEnabled = true
		}
		p.advance()
	}
	if !hasEnabled && !hasDisabled {
		return 0, 0, p.syntax(SyntaxUnexpectedToken, p.current().span, "inline modifier has no flags")
	}
	p.advance()
	return enabled, disabled, nil
}

func (p *parser) captureName() (string, int, error) {
	p.advance()
	return p.captureNameBody()
}

func (p *parser) captureNameBody() (string, int, error) {
	start := p.current().span.Start
	end := start
	units := make([]uint16, 0, 8)
	for p.current().kind != TokenEOF &&
		(p.current().kind != TokenCharacter || p.current().text != ">") {
		token := p.current()
		if token.kind == TokenEscape {
			escaped, escapeEnd, err := p.regexpIdentifierEscape()
			if err != nil {
				return "", end, err
			}
			units = append(units, escaped...)
			end = escapeEnd
			continue
		}
		char, _ := utf8.DecodeRuneInString(token.text)
		units = append(units, utf16.Encode([]rune{char})...)
		end = token.span.End
		p.advance()
	}
	if len(units) == 0 || p.current().kind == TokenEOF {
		return "", end, p.syntax(SyntaxUnexpectedEOF, Span{Start: start, End: end}, "capture name is not closed")
	}
	characters := decodePatternUnits(units)
	for index, char := range characters {
		if char >= 0xD800 && char <= 0xDFFF ||
			index == 0 && !unicodeIdentifierStart(char) || index > 0 && !unicodeIdentifierContinue(char) {
			return "", end, p.syntax(SyntaxInvalidEscape, Span{Start: start, End: end}, "invalid capture name")
		}
	}
	end = p.current().span.End
	p.advance()
	return string(characters), end, nil
}

func (p *parser) regexpIdentifierEscape() ([]uint16, int, error) {
	prefix := p.current()
	if prefix.text != `\u` {
		return nil, prefix.span.End, p.syntax(SyntaxInvalidEscape, prefix.span, "capture name escape must be Unicode")
	}
	p.advance()
	end := prefix.span.End
	var digits strings.Builder
	if p.current().kind == TokenLeftBrace {
		p.advance()
		for p.current().kind != TokenRightBrace {
			token := p.current()
			if token.kind != TokenCharacter || len(token.text) != 1 || !isHex(token.text[0]) {
				return nil, end, p.syntax(SyntaxInvalidEscape, token.span, "invalid capture name Unicode escape")
			}
			digits.WriteString(token.text)
			end = token.span.End
			p.advance()
		}
		if digits.Len() == 0 {
			return nil, end, p.syntax(SyntaxInvalidEscape, p.current().span, "empty capture name Unicode escape")
		}
		end = p.current().span.End
		p.advance()
	} else {
		for range 4 {
			token := p.current()
			if token.kind != TokenCharacter || len(token.text) != 1 || !isHex(token.text[0]) {
				return nil, end, p.syntax(SyntaxInvalidEscape, token.span, "invalid capture name Unicode escape")
			}
			digits.WriteString(token.text)
			end = token.span.End
			p.advance()
		}
	}
	value, valueErr := strconv.ParseUint(digits.String(), 16, 32)
	if valueErr != nil || value > utf8.MaxRune {
		return nil, end, p.syntax(SyntaxInvalidEscape, Span{Start: prefix.span.Start, End: end}, "capture name Unicode escape is out of range")
	}
	if value <= 0xFFFF {
		return []uint16{uint16(value)}, end, nil
	}
	return utf16.Encode([]rune{rune(value)}), end, nil
}

func (p *parser) quantifier() (int, int, int, bool, error) {
	token := p.current()
	switch token.kind {
	case TokenStar:
		p.advance()
		return 0, -1, token.span.End, true, nil
	case TokenPlus:
		p.advance()
		return 1, -1, token.span.End, true, nil
	case TokenQuestion:
		p.advance()
		return 0, 1, token.span.End, true, nil
	case TokenLeftBrace:
		return p.bracedQuantifier()
	default:
		return 0, 0, 0, false, nil
	}
}

func (p *parser) bracedQuantifier() (int, int, int, bool, error) {
	startPosition := p.position
	open := p.current()
	p.advance()
	minimum, ok := p.decimal()
	if !ok {
		p.position = startPosition
		return 0, 0, 0, false, nil
	}
	maximum := minimum
	if p.current().kind == TokenComma {
		p.advance()
		var hasMaximum bool
		maximum, hasMaximum = p.decimal()
		if !hasMaximum {
			maximum = -1
		}
	}
	if p.current().kind != TokenRightBrace {
		return 0, 0, 0, false, p.syntax(SyntaxInvalidQuantifier, Span{Start: open.span.Start, End: p.current().span.End}, "quantifier is not closed")
	}
	close := p.current()
	p.advance()
	if maximum >= 0 && minimum > maximum {
		return 0, 0, 0, false, p.syntax(SyntaxInvalidQuantifier, Span{Start: open.span.Start, End: close.span.End}, "quantifier minimum exceeds maximum")
	}

	return minimum, maximum, close.span.End, true, nil
}

func (p *parser) decimal() (int, bool) {
	start := p.position
	var text strings.Builder
	for p.current().kind == TokenCharacter && p.current().text >= "0" && p.current().text <= "9" {
		text.WriteString(p.current().text)
		p.advance()
	}
	if p.position == start {
		return 0, false
	}
	value, err := strconv.ParseUint(text.String(), 10, 31)
	if err != nil {
		return int(^uint(0) >> 1), true
	}

	return int(value), true
}

func (p *parser) node(node Node) (Node, error) {
	p.nodes++
	if p.nodes > p.options.Limits.ASTNodes {
		return Node{}, &LimitError{Kind: LimitASTNodes, Limit: p.options.Limits.ASTNodes, Used: p.nodes}
	}

	return node, nil
}

func (p *parser) current() Token { return p.tokens[p.position] }
func (p *parser) advance()       { p.position++ }

func (p *parser) syntax(code SyntaxCode, span Span, message string) error {
	if len(message) > 160 {
		message = message[:160]
	}

	return &SyntaxError{Code: code, Span: span, Message: message}
}
