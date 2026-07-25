package datatype

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxPatternSourceBytes     = 1 << 20
	maxTranslatedPatternBytes = 8 << 20
)

// CompilePattern compiles an XML Schema 1.0 regular expression. The
// translation retains Go's linear-time regular-expression execution while
// implementing XML Schema character classes and whole-literal matching.
func CompilePattern(pattern string) (*regexp.Regexp, error) {
	if len(pattern) > maxPatternSourceBytes {
		return nil, fmt.Errorf("xsd: pattern exceeds %d bytes", maxPatternSourceBytes)
	}
	translator := patternTranslator{source: pattern}
	translated, err := translator.translate()
	if err != nil {
		return nil, err
	}
	return regexp.Compile(`^(?:` + translated + `)$`)
}

type patternTranslator struct {
	source string
	index  int
}

func (translator *patternTranslator) translate() (string, error) {
	var translated strings.Builder
	for translator.index < len(translator.source) {
		character, size := utf8.DecodeRuneInString(translator.source[translator.index:])
		if character == utf8.RuneError && size == 1 {
			return "", fmt.Errorf("xsd: pattern is not valid UTF-8 at byte %d", translator.index)
		}
		switch character {
		case '[':
			class, err := translator.parseClass()
			if err != nil {
				return "", err
			}
			translated.WriteString(class.regexp())
		case '\\':
			class, literal, err := translator.parseEscape()
			if err != nil {
				return "", err
			}
			if class != nil {
				translated.WriteString(class.regexp())
			} else {
				translated.WriteString(regexp.QuoteMeta(string(literal)))
			}
		case '.':
			translated.WriteString(xmlUniverse.subtract(runeSet{{'\n', '\n'}, {'\r', '\r'}}).regexp())
			translator.index += size
		case '^', '$':
			translated.WriteByte('\\')
			translated.WriteRune(character)
			translator.index += size
		default:
			translated.WriteRune(character)
			translator.index += size
		}
		if translated.Len() > maxTranslatedPatternBytes {
			return "", fmt.Errorf("xsd: translated pattern exceeds %d bytes", maxTranslatedPatternBytes)
		}
	}
	return translated.String(), nil
}

func (translator *patternTranslator) parseClass() (runeSet, error) {
	start := translator.index
	translator.index++
	negated := translator.consume('^')
	var result runeSet
	hasMember := false
	for translator.index < len(translator.source) && translator.source[translator.index] != ']' {
		if strings.HasPrefix(translator.source[translator.index:], "-[") {
			if !hasMember {
				return nil, fmt.Errorf("xsd: empty character group at byte %d", start)
			}
			translator.index++
			excluded, err := translator.parseClass()
			if err != nil {
				return nil, err
			}
			if translator.index >= len(translator.source) || translator.source[translator.index] != ']' {
				return nil, fmt.Errorf("xsd: character class subtraction at byte %d is not final", start)
			}
			translator.index++
			if negated {
				result = xmlUniverse.subtract(result)
			}
			return result.subtract(excluded), nil
		}

		member, singleton, err := translator.parseClassMember()
		if err != nil {
			return nil, err
		}
		hasMember = true
		if translator.index < len(translator.source) && translator.source[translator.index] == '-' &&
			!strings.HasPrefix(translator.source[translator.index:], "-[") {
			if singleton < 0 {
				return nil, fmt.Errorf("xsd: character range at byte %d has a class endpoint", translator.index)
			}
			translator.index++
			end, endSingleton, rangeErr := translator.parseClassMember()
			if rangeErr != nil {
				return nil, rangeErr
			}
			if endSingleton < 0 || len(end) != 1 || singleton > endSingleton {
				return nil, fmt.Errorf("xsd: invalid character range at byte %d", translator.index)
			}
			member = runeSet{{singleton, endSingleton}}
		}
		result = result.union(member)
	}
	if translator.index >= len(translator.source) || !hasMember {
		return nil, fmt.Errorf("xsd: unterminated or empty character class at byte %d", start)
	}
	translator.index++
	if negated {
		result = xmlUniverse.subtract(result)
	}
	return result, nil
}

func (translator *patternTranslator) parseClassMember() (runeSet, rune, error) {
	if translator.index >= len(translator.source) || translator.source[translator.index] == ']' {
		return nil, -1, fmt.Errorf("xsd: missing character class member at byte %d", translator.index)
	}
	if translator.source[translator.index] == '[' || translator.source[translator.index] == '-' {
		return nil, -1, fmt.Errorf("xsd: unescaped character at byte %d", translator.index)
	}
	if translator.source[translator.index] == '\\' {
		class, literal, err := translator.parseEscape()
		if err != nil {
			return nil, -1, err
		}
		if class != nil {
			return class, -1, nil
		}
		return runeSet{{literal, literal}}, literal, nil
	}
	character, size := utf8.DecodeRuneInString(translator.source[translator.index:])
	if character == utf8.RuneError && size == 1 {
		return nil, -1, fmt.Errorf("xsd: pattern is not valid UTF-8 at byte %d", translator.index)
	}
	translator.index += size
	return runeSet{{character, character}}, character, nil
}

func (translator *patternTranslator) parseEscape() (runeSet, rune, error) {
	start := translator.index
	translator.index++
	if translator.index >= len(translator.source) {
		return nil, -1, fmt.Errorf("xsd: incomplete escape at byte %d", start)
	}
	escape, size := utf8.DecodeRuneInString(translator.source[translator.index:])
	translator.index += size
	switch escape {
	case 'n':
		return nil, '\n', nil
	case 'r':
		return nil, '\r', nil
	case 't':
		return nil, '\t', nil
	case '\\', '|', '.', '-', '^', '?', '*', '+', '(', ')', '{', '}', '[', ']':
		return nil, escape, nil
	case 's':
		return runeSet{{'\t', '\n'}, {'\r', '\r'}, {' ', ' '}}, -1, nil
	case 'S':
		return xmlUniverse.subtract(runeSet{{'\t', '\n'}, {'\r', '\r'}, {' ', ' '}}), -1, nil
	case 'i':
		return xmlNameStartSet, -1, nil
	case 'I':
		return xmlUniverse.subtract(xmlNameStartSet), -1, nil
	case 'c':
		return xmlNameChar, -1, nil
	case 'C':
		return xmlUniverse.subtract(xmlNameChar), -1, nil
	case 'd':
		return categorySet("Nd"), -1, nil
	case 'D':
		return xmlUniverse.subtract(categorySet("Nd")), -1, nil
	case 'w':
		return xmlWord, -1, nil
	case 'W':
		return xmlUniverse.subtract(xmlWord), -1, nil
	case 'p', 'P':
		property, err := translator.parseProperty(start)
		if err != nil {
			return nil, -1, err
		}
		if escape == 'P' {
			property = xmlUniverse.subtract(property)
		}
		return property, -1, nil
	default:
		return nil, -1, fmt.Errorf("xsd: invalid escape \\%c at byte %d", escape, start)
	}
}

func (translator *patternTranslator) parseProperty(start int) (runeSet, error) {
	if !translator.consume('{') {
		return nil, fmt.Errorf("xsd: property escape at byte %d is missing '{'", start)
	}
	end := strings.IndexByte(translator.source[translator.index:], '}')
	if end < 0 {
		return nil, fmt.Errorf("xsd: unterminated property escape at byte %d", start)
	}
	name := translator.source[translator.index : translator.index+end]
	translator.index += end + 1
	if strings.HasPrefix(name, "Is") {
		block, ok := xsdUnicodeBlocks[name[2:]]
		if !ok {
			return nil, fmt.Errorf("xsd: unknown Unicode block %q at byte %d", name[2:], start)
		}
		return block, nil
	}
	category, ok := unicode.Categories[name]
	if !ok {
		return nil, fmt.Errorf("xsd: unknown Unicode category %q at byte %d", name, start)
	}
	return rangeTableSet(category), nil
}

func (translator *patternTranslator) consume(character byte) bool {
	if translator.index < len(translator.source) && translator.source[translator.index] == character {
		translator.index++
		return true
	}
	return false
}

type runeRange struct {
	first rune
	last  rune
}

type runeSet []runeRange

var xmlUniverse = runeSet{
	{0x9, 0xA}, {0xD, 0xD}, {0x20, 0xD7FF}, {0xE000, 0xFFFD}, {0x10000, utf8.MaxRune},
}

var xmlNameStartSet = runeSet{
	{':', ':'}, {'A', 'Z'}, {'_', '_'}, {'a', 'z'}, {0xC0, 0xD6}, {0xD8, 0xF6},
	{0xF8, 0x2FF}, {0x370, 0x37D}, {0x37F, 0x1FFF}, {0x200C, 0x200D},
	{0x2070, 0x218F}, {0x2C00, 0x2FEF}, {0x3001, 0xD7FF}, {0xF900, 0xFDCF},
	{0xFDF0, 0xFFFD}, {0x10000, 0xEFFFF},
}

var xmlNameChar = xmlNameStartSet.union(runeSet{
	{'-', '.'}, {'0', '9'}, {0xB7, 0xB7}, {0x300, 0x36F}, {0x203F, 0x2040},
})

var xmlWord = xmlUniverse.subtract(categorySet("P").union(categorySet("Z")).union(categorySet("C")))

func categorySet(name string) runeSet {
	return rangeTableSet(unicode.Categories[name])
}

func rangeTableSet(table *unicode.RangeTable) runeSet {
	result := make(runeSet, 0, len(table.R16)+len(table.R32))
	for _, item := range table.R16 {
		low, high, stride := uint32(item.Lo), uint32(item.Hi), uint32(item.Stride)
		count, ok := boundedRangeCount(low, high, stride, 1<<16)
		if !ok {
			return nil
		}
		for offset := range count {
			value := low + offset*stride
			result = append(result, runeRange{rune(value), rune(value)})
		}
	}
	for _, item := range table.R32 {
		count, ok := boundedRangeCount(
			item.Lo,
			item.Hi,
			item.Stride,
			utf8.MaxRune+1,
		)
		if !ok {
			return nil
		}
		for offset := range count {
			value := item.Lo + offset*item.Stride
			result = append(result, runeRange{rune(value), rune(value)})
		}
	}
	return result.normalized().intersect(xmlUniverse)
}

func boundedRangeCount(low, high, stride, limit uint32) (uint32, bool) {
	if stride == 0 || high < low {
		return 0, false
	}
	count := (high-low)/stride + 1
	if count > limit {
		return 0, false
	}
	return count, true
}

func (set runeSet) union(other runeSet) runeSet {
	combined := append(append(runeSet(nil), set...), other...)
	return combined.normalized()
}

func (set runeSet) intersect(other runeSet) runeSet {
	set = set.normalized()
	other = other.normalized()
	result := make(runeSet, 0)
	for left, right := 0, 0; left < len(set) && right < len(other); {
		first := max(set[left].first, other[right].first)
		last := min(set[left].last, other[right].last)
		if first <= last {
			result = append(result, runeRange{first, last})
		}
		if set[left].last < other[right].last {
			left++
		} else {
			right++
		}
	}
	return result
}

func (set runeSet) subtract(other runeSet) runeSet {
	set = set.normalized()
	other = other.normalized()
	result := make(runeSet, 0, len(set))
	for _, candidate := range set {
		cursor := candidate.first
		for _, excluded := range other {
			if excluded.last < cursor {
				continue
			}
			if excluded.first > candidate.last {
				break
			}
			if excluded.first > cursor {
				result = append(result, runeRange{cursor, excluded.first - 1})
			}
			if excluded.last >= candidate.last {
				cursor = candidate.last + 1
				break
			}
			cursor = excluded.last + 1
		}
		if cursor <= candidate.last {
			result = append(result, runeRange{cursor, candidate.last})
		}
	}
	return result
}

func (set runeSet) normalized() runeSet {
	if len(set) < 2 {
		return append(runeSet(nil), set...)
	}
	result := append(runeSet(nil), set...)
	sort.Slice(result, func(left, right int) bool {
		return result[left].first < result[right].first ||
			(result[left].first == result[right].first && result[left].last < result[right].last)
	})
	merged := result[:0]
	for _, item := range result {
		if len(merged) == 0 || int64(item.first) > int64(merged[len(merged)-1].last)+1 {
			merged = append(merged, item)
			continue
		}
		if item.last > merged[len(merged)-1].last {
			merged[len(merged)-1].last = item.last
		}
	}
	return merged
}

func (set runeSet) regexp() string {
	var expression strings.Builder
	expression.WriteByte('[')
	for _, item := range set.normalized() {
		fmt.Fprintf(&expression, `\x{%X}`, item.first)
		if item.last != item.first {
			fmt.Fprintf(&expression, `-\x{%X}`, item.last)
		}
	}
	expression.WriteByte(']')
	return expression.String()
}
