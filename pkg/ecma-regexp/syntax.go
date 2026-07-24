package ecmascript

import "unicode/utf8"

// TokenKind classifies lexical pattern input. Tokens retain source byte spans.
type TokenKind uint8

const (
	TokenCharacter TokenKind = iota + 1
	TokenEscape
	TokenAlternation
	TokenDot
	TokenCaret
	TokenDollar
	TokenLeftParen
	TokenRightParen
	TokenLeftBracket
	TokenRightBracket
	TokenLeftBrace
	TokenRightBrace
	TokenStar
	TokenPlus
	TokenQuestion
	TokenComma
	TokenEOF
)

// Token is an immutable lexical token.
type Token struct {
	kind TokenKind
	text string
	span Span
}

func (t Token) Kind() TokenKind { return t.kind }
func (t Token) Text() string    { return t.text }
func (t Token) Span() Span      { return t.span }

// Tokenize lexes pattern while retaining exact byte spans. Escapes are kept as
// two-code-point lexical units so the parser can interpret them by edition and
// grammar context.
func Tokenize(pattern string, options ParseOptions) ([]Token, error) {
	if err := options.validate(pattern); err != nil {
		return nil, err
	}

	tokens := make([]Token, 0, min(len(pattern)+1, 256))
	for offset := 0; offset < len(pattern); {
		start := offset
		char, size := utf8.DecodeRuneInString(pattern[offset:])
		offset += size
		if char == '\\' {
			if offset == len(pattern) {
				return nil, &SyntaxError{Code: SyntaxUnexpectedEOF, Span: Span{Start: start, End: offset}, Message: "escape has no escaped character"}
			}
			_, escapedSize := utf8.DecodeRuneInString(pattern[offset:])
			offset += escapedSize
			tokens = append(tokens, Token{kind: TokenEscape, text: pattern[start:offset], span: Span{Start: start, End: offset}})
			continue
		}

		tokens = append(tokens, Token{kind: tokenKind(char), text: pattern[start:offset], span: Span{Start: start, End: offset}})
	}
	tokens = append(tokens, Token{kind: TokenEOF, span: Span{Start: len(pattern), End: len(pattern)}})

	return tokens, nil
}

func tokenKind(char rune) TokenKind {
	switch char {
	case '|':
		return TokenAlternation
	case '.':
		return TokenDot
	case '^':
		return TokenCaret
	case '$':
		return TokenDollar
	case '(':
		return TokenLeftParen
	case ')':
		return TokenRightParen
	case '[':
		return TokenLeftBracket
	case ']':
		return TokenRightBracket
	case '{':
		return TokenLeftBrace
	case '}':
		return TokenRightBrace
	case '*':
		return TokenStar
	case '+':
		return TokenPlus
	case '?':
		return TokenQuestion
	case ',':
		return TokenComma
	default:
		return TokenCharacter
	}
}
