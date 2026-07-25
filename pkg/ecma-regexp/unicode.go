package ecmascript

//go:generate ./scripts/sync-unicode.sh

import (
	"sort"
	"strings"
)

type unicodeRange struct {
	lo rune
	hi rune
}

type unicodeTable struct {
	name   string
	offset uint32
	length uint32
}

type unicodeAlias struct {
	name  string
	table uint16
}

type unicodeString struct {
	offset uint32
	length uint16
}

type unicodeStringTable struct {
	name   string
	offset uint32
	length uint32
}

type unicodeFold struct {
	from rune
	to   rune
}

type unicodeFoldGroup struct {
	canonical rune
	offset    uint16
	length    uint8
}

func lookupUnicodeProperty(expression string) (int, bool) {
	if property, value, found := strings.Cut(expression, "="); found {
		prefix := ""
		switch property {
		case "General_Category", "gc":
			prefix = "gc:"
		case "Script", "sc":
			prefix = "sc:"
		case "Script_Extensions", "scx":
			prefix = "scx:"
		default:
			return 0, false
		}
		return lookupUnicodeAlias(prefix + value)
	}
	if table, ok := lookupUnicodeAlias("gc:" + expression); ok {
		return table, true
	}
	return lookupUnicodeAlias("bin:" + expression)
}

func lookupUnicodeAlias(name string) (int, bool) {
	index := sort.Search(len(generatedUnicodeAliases), func(index int) bool {
		return generatedUnicodeAliases[index].name >= name
	})
	if index == len(generatedUnicodeAliases) || generatedUnicodeAliases[index].name != name {
		return 0, false
	}
	return int(generatedUnicodeAliases[index].table), true
}

func unicodePropertyContains(tableIndex int, char rune) bool {
	table := generatedUnicodeTables[tableIndex]
	ranges := generatedUnicodeRanges[table.offset : table.offset+table.length]
	index := sort.Search(len(ranges), func(index int) bool {
		return ranges[index].hi >= char
	})
	return index < len(ranges) && ranges[index].lo <= char
}

func unicodeGeneralCategoryContains(category string, char rune) bool {
	table, ok := lookupUnicodeAlias("gc:" + category)
	return ok && unicodePropertyContains(table, char)
}

func unicodeIdentifierStart(char rune) bool {
	if char == '$' || char == '_' {
		return true
	}
	table, ok := lookupUnicodeAlias("bin:ID_Start")
	return ok && unicodePropertyContains(table, char)
}

func unicodeIdentifierContinue(char rune) bool {
	if char == '$' || char == '_' || char == 0x200C || char == 0x200D {
		return true
	}
	table, ok := lookupUnicodeAlias("bin:ID_Continue")
	return ok && unicodePropertyContains(table, char)
}

func lookupUnicodeStringProperty(name string) ([][]uint16, bool) {
	index := sort.Search(len(generatedUnicodeStringTables), func(index int) bool {
		return generatedUnicodeStringTables[index].name >= name
	})
	if index == len(generatedUnicodeStringTables) || generatedUnicodeStringTables[index].name != name {
		return nil, false
	}
	table := generatedUnicodeStringTables[index]
	stringsInProperty := generatedUnicodeStrings[table.offset : table.offset+table.length]
	result := make([][]uint16, len(stringsInProperty))
	for stringIndex, value := range stringsInProperty {
		result[stringIndex] = append([]uint16(nil), generatedUnicodeStringUnits[value.offset:uint32(value.offset)+uint32(value.length)]...)
	}
	return result, true
}

func unicodeCanonical(char rune) rune {
	return lookupFold(generatedUnicodeFolds[:], char)
}

func legacyCanonical(char rune) rune {
	upper := lookupFold(generatedLegacyUpper[:], char)
	if char >= 0x80 && upper < 0x80 {
		return char
	}
	return upper
}

func lookupFold(folds []unicodeFold, char rune) rune {
	index := sort.Search(len(folds), func(index int) bool { return folds[index].from >= char })
	if index < len(folds) && folds[index].from == char {
		return folds[index].to
	}
	return char
}

func unicodeFoldVariants(char rune) []rune {
	canonical := unicodeCanonical(char)
	index := sort.Search(len(generatedUnicodeFoldGroups), func(index int) bool {
		return generatedUnicodeFoldGroups[index].canonical >= canonical
	})
	if index == len(generatedUnicodeFoldGroups) || generatedUnicodeFoldGroups[index].canonical != canonical {
		return []rune{char}
	}
	group := generatedUnicodeFoldGroups[index]
	return generatedUnicodeFoldValues[group.offset : uint32(group.offset)+uint32(group.length)]
}
