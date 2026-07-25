// Command unicodegen generates the exact Unicode property tables used by the
// ECMA-262 regular expression engine.
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

const (
	unicodeVersion           = "16.0.0"
	wantSHA256               = "c86dd81f2b14a43b0cc064aa5f89aa7241386801e35c59c7984e579832634eb2"
	wantEmojiSequencesSHA256 = "3fe3c77e72e8f26df302dc7d99b106c5d08fd808ef7246fb5d4502d659fe659c"
	wantEmojiZWJSHA256       = "9423ec235474356f970a696506737e2d5d65453a67f45df66b8bbe920c3fab83"
	maxCodePoint             = 0x10FFFF
)

type codeRange struct {
	lo rune
	hi rune
}

var binaryProperties = []string{
	"ASCII", "ASCII_Hex_Digit", "Alphabetic", "Any", "Assigned",
	"Bidi_Control", "Bidi_Mirrored", "Case_Ignorable", "Cased",
	"Changes_When_Casefolded", "Changes_When_Casemapped",
	"Changes_When_Lowercased", "Changes_When_NFKC_Casefolded",
	"Changes_When_Titlecased", "Changes_When_Uppercased", "Dash",
	"Default_Ignorable_Code_Point", "Deprecated", "Diacritic", "Emoji",
	"Emoji_Component", "Emoji_Modifier", "Emoji_Modifier_Base",
	"Emoji_Presentation", "Extended_Pictographic", "Extender",
	"Grapheme_Base", "Grapheme_Extend", "Hex_Digit", "IDS_Binary_Operator",
	"IDS_Trinary_Operator", "ID_Continue", "ID_Start", "Ideographic",
	"Join_Control", "Logical_Order_Exception", "Lowercase", "Math",
	"Noncharacter_Code_Point", "Pattern_Syntax", "Pattern_White_Space",
	"Quotation_Mark", "Radical", "Regional_Indicator", "Sentence_Terminal",
	"Soft_Dotted", "Terminal_Punctuation", "Unified_Ideograph", "Uppercase",
	"Variation_Selector", "White_Space", "XID_Continue", "XID_Start",
}

func main() {
	ucdPath := flag.String("ucd", "", "path to the pinned UCD.zip")
	emojiSequencesPath := flag.String("emoji-sequences", "", "path to pinned emoji-sequences.txt")
	emojiZWJPath := flag.String("emoji-zwj-sequences", "", "path to pinned emoji-zwj-sequences.txt")
	outputPath := flag.String("output", "unicode_tables_generated.go", "generated Go output")
	flag.Parse()
	if *ucdPath == "" || *emojiSequencesPath == "" || *emojiZWJPath == "" {
		fatalf("-ucd, -emoji-sequences, and -emoji-zwj-sequences are required")
	}

	archiveBytes, err := os.ReadFile(*ucdPath)
	check(err)
	verifyDigest("UCD", archiveBytes, wantSHA256)
	archive, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	check(err)
	files := readFiles(archive)

	propertyAliases := parsePropertyAliases(files["PropertyAliases.txt"])
	valueAliases := parseValueAliases(files["PropertyValueAliases.txt"])
	sourceProperties := make(map[string][]codeRange)
	for _, name := range []string{"PropList.txt", "DerivedCoreProperties.txt", "DerivedNormalizationProps.txt", "emoji/emoji-data.txt"} {
		for property, ranges := range parseRangeProperties(files[name]) {
			sourceProperties[property] = append(sourceProperties[property], ranges...)
		}
	}

	tables := make(map[string][]codeRange)
	aliases := make(map[string]string)
	general, assigned, mirrored := parseUnicodeData(files["UnicodeData.txt"])
	general["Cn"] = complement(assigned)
	general["C"] = union(general["Cc"], general["Cf"], general["Cn"], general["Co"], general["Cs"])
	general["LC"] = union(general["Ll"], general["Lt"], general["Lu"])
	for canonical, ranges := range general {
		tableID := "gc:" + canonical
		tables[tableID] = ranges
	}
	for alias, canonical := range valueAliases["gc"] {
		aliases["gc:"+alias] = "gc:" + canonical
	}

	scriptAliases := valueAliases["sc"]
	scripts := parseRangeProperties(files["Scripts.txt"])
	scriptBits := make(map[string][]uint64)
	for _, canonical := range uniqueValues(scriptAliases) {
		scriptBits[canonical] = make([]uint64, (maxCodePoint+64)/64)
	}
	for script, ranges := range scripts {
		bits, ok := scriptBits[script]
		if !ok {
			fatalf("script %q has no PropertyValueAliases entry", script)
		}
		setRanges(bits, ranges, true)
	}
	var explicitScripts []codeRange
	for _, ranges := range scripts {
		explicitScripts = append(explicitScripts, ranges...)
	}
	unknownBits, ok := scriptBits["Unknown"]
	if !ok {
		fatalf("Script=Unknown has no PropertyValueAliases entry")
	}
	setRanges(unknownBits, complement(explicitScripts), true)
	scriptExtensionBits := cloneBits(scriptBits)
	for values, ranges := range parseRangeProperties(files["ScriptExtensions.txt"]) {
		for _, interval := range ranges {
			for codePoint := interval.lo; codePoint <= interval.hi; codePoint++ {
				for _, bits := range scriptExtensionBits {
					setBit(bits, codePoint, false)
				}
			}
		}
		for _, short := range strings.Fields(values) {
			canonical, ok := scriptAliases[short]
			if !ok {
				fatalf("script extension alias %q is unknown", short)
			}
			setRanges(scriptExtensionBits[canonical], ranges, true)
		}
	}
	for canonical, bits := range scriptBits {
		tables["sc:"+canonical] = bitRanges(bits)
		tables["scx:"+canonical] = bitRanges(scriptExtensionBits[canonical])
	}
	for alias, canonical := range scriptAliases {
		aliases["sc:"+alias] = "sc:" + canonical
		aliases["scx:"+alias] = "scx:" + canonical
	}

	sourceProperties["ASCII"] = []codeRange{{lo: 0, hi: 0x7F}}
	sourceProperties["Any"] = []codeRange{{lo: 0, hi: maxCodePoint}}
	sourceProperties["Assigned"] = assigned
	sourceProperties["Bidi_Mirrored"] = mirrored
	for _, canonical := range binaryProperties {
		ranges, ok := sourceProperties[canonical]
		if !ok {
			fatalf("binary property %q has no generated source", canonical)
		}
		tableID := "bin:" + canonical
		tables[tableID] = ranges
		if canonical == "ASCII" || canonical == "Any" || canonical == "Assigned" {
			aliases[tableID] = tableID
			continue
		}
		propertyNames, ok := propertyAliases[canonical]
		if !ok {
			fatalf("binary property %q has no aliases", canonical)
		}
		for _, name := range propertyNames {
			aliases["bin:"+name] = tableID
		}
	}

	emojiSequences, err := os.ReadFile(*emojiSequencesPath)
	check(err)
	verifyDigest("emoji-sequences", emojiSequences, wantEmojiSequencesSHA256)
	emojiZWJSequences, err := os.ReadFile(*emojiZWJPath)
	check(err)
	verifyDigest("emoji-zwj-sequences", emojiZWJSequences, wantEmojiZWJSHA256)
	stringProperties := parseEmojiProperties(emojiSequences, emojiZWJSequences)

	folds := parseCaseFolding(files["CaseFolding.txt"])
	legacyUpper := parseLegacyUpper(files["UnicodeData.txt"])
	generated, err := generate(tables, aliases, stringProperties, folds, legacyUpper)
	check(err)
	check(os.WriteFile(*outputPath, generated, 0o644))
}

func readFiles(archive *zip.Reader) map[string][]byte {
	result := make(map[string][]byte)
	for _, file := range archive.File {
		reader, err := file.Open()
		check(err)
		contents, err := io.ReadAll(reader)
		check(err)
		check(reader.Close())
		result[file.Name] = contents
	}
	return result
}

func parsePropertyAliases(contents []byte) map[string][]string {
	result := make(map[string][]string)
	for _, fields := range dataLines(contents) {
		if len(fields) < 2 {
			continue
		}
		canonical := fields[1]
		result[canonical] = append([]string(nil), fields...)
	}
	return result
}

func parseValueAliases(contents []byte) map[string]map[string]string {
	result := make(map[string]map[string]string)
	for _, fields := range dataLines(contents) {
		if len(fields) < 3 || fields[0] != "gc" && fields[0] != "sc" {
			continue
		}
		if result[fields[0]] == nil {
			result[fields[0]] = make(map[string]string)
		}
		canonical := fields[1]
		if fields[0] == "sc" {
			canonical = fields[2]
		}
		for _, alias := range fields[1:] {
			result[fields[0]][alias] = canonical
		}
	}
	return result
}

func dataLines(contents []byte) [][]string {
	result := make([][]string, 0)
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	for scanner.Scan() {
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
		if line == "" {
			continue
		}
		parts := strings.Split(line, ";")
		fields := make([]string, 0, len(parts))
		for _, part := range parts {
			value := strings.TrimSpace(part)
			if value != "" {
				fields = append(fields, value)
			}
		}
		result = append(result, fields)
	}
	check(scanner.Err())
	return result
}

func parseRangeProperties(contents []byte) map[string][]codeRange {
	result := make(map[string][]codeRange)
	for _, fields := range dataLines(contents) {
		if len(fields) < 2 {
			continue
		}
		result[fields[1]] = append(result[fields[1]], parseRange(fields[0]))
	}
	return result
}

func parseRange(value string) codeRange {
	parts := strings.Split(value, "..")
	lo, err := strconv.ParseInt(parts[0], 16, 32)
	check(err)
	hi := lo
	if len(parts) == 2 {
		hi, err = strconv.ParseInt(parts[1], 16, 32)
		check(err)
	}
	return codeRange{lo: rune(lo), hi: rune(hi)}
}

func parseUnicodeData(contents []byte) (map[string][]codeRange, []codeRange, []codeRange) {
	general := make(map[string][]codeRange)
	assigned := make([]codeRange, 0)
	mirrored := make([]codeRange, 0)
	var pending *struct {
		lo       rune
		category string
		mirrored bool
	}
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ";")
		code, err := strconv.ParseInt(fields[0], 16, 32)
		check(err)
		name := fields[1]
		category := fields[2]
		isMirrored := fields[9] == "Y"
		if strings.HasSuffix(name, ", First>") {
			pending = &struct {
				lo       rune
				category string
				mirrored bool
			}{lo: rune(code), category: category, mirrored: isMirrored}
			continue
		}
		interval := codeRange{lo: rune(code), hi: rune(code)}
		if strings.HasSuffix(name, ", Last>") {
			if pending == nil || pending.category != category {
				fatalf("invalid UnicodeData First/Last range at %s", fields[0])
			}
			interval.lo = pending.lo
			isMirrored = pending.mirrored
			pending = nil
		}
		general[category] = append(general[category], interval)
		general[category[:1]] = append(general[category[:1]], interval)
		assigned = append(assigned, interval)
		if isMirrored {
			mirrored = append(mirrored, interval)
		}
	}
	check(scanner.Err())
	return general, merge(assigned), merge(mirrored)
}

func complement(ranges []codeRange) []codeRange {
	merged := merge(ranges)
	result := make([]codeRange, 0)
	next := rune(0)
	for _, interval := range merged {
		if next < interval.lo {
			result = append(result, codeRange{lo: next, hi: interval.lo - 1})
		}
		next = interval.hi + 1
	}
	if next <= maxCodePoint {
		result = append(result, codeRange{lo: next, hi: maxCodePoint})
	}
	return result
}

func union(groups ...[]codeRange) []codeRange {
	var result []codeRange
	for _, group := range groups {
		result = append(result, group...)
	}
	return merge(result)
}

func merge(ranges []codeRange) []codeRange {
	result := append([]codeRange(nil), ranges...)
	sort.Slice(result, func(left, right int) bool {
		return result[left].lo < result[right].lo || result[left].lo == result[right].lo && result[left].hi < result[right].hi
	})
	merged := result[:0]
	for _, interval := range result {
		if len(merged) == 0 || interval.lo > merged[len(merged)-1].hi+1 {
			merged = append(merged, interval)
			continue
		}
		if interval.hi > merged[len(merged)-1].hi {
			merged[len(merged)-1].hi = interval.hi
		}
	}
	return merged
}

func uniqueValues(values map[string]string) []string {
	seen := make(map[string]struct{})
	for _, value := range values {
		seen[value] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func cloneBits(source map[string][]uint64) map[string][]uint64 {
	result := make(map[string][]uint64, len(source))
	for name, bits := range source {
		result[name] = append([]uint64(nil), bits...)
	}
	return result
}

func setRanges(bits []uint64, ranges []codeRange, value bool) {
	for _, interval := range ranges {
		for codePoint := interval.lo; codePoint <= interval.hi; codePoint++ {
			setBit(bits, codePoint, value)
		}
	}
}

func setBit(bits []uint64, codePoint rune, value bool) {
	word := int(codePoint) / 64
	mask := uint64(1) << (uint(codePoint) % 64)
	if value {
		bits[word] |= mask
	} else {
		bits[word] &^= mask
	}
}

func bitRanges(bits []uint64) []codeRange {
	result := make([]codeRange, 0)
	start := rune(-1)
	for codePoint := rune(0); codePoint <= maxCodePoint; codePoint++ {
		set := bits[int(codePoint)/64]&(uint64(1)<<(uint(codePoint)%64)) != 0
		if set && start < 0 {
			start = codePoint
		} else if !set && start >= 0 {
			result = append(result, codeRange{lo: start, hi: codePoint - 1})
			start = -1
		}
	}
	if start >= 0 {
		result = append(result, codeRange{lo: start, hi: maxCodePoint})
	}
	return result
}

func generate(tables map[string][]codeRange, aliases map[string]string, stringProperties map[string][][]uint16, folds, legacyUpper map[rune]rune) ([]byte, error) {
	tableNames := make([]string, 0, len(tables))
	for name := range tables {
		tableNames = append(tableNames, name)
	}
	sort.Strings(tableNames)
	tableIndex := make(map[string]int, len(tableNames))
	var source strings.Builder
	source.WriteString("// Code generated by internal/cmd/unicodegen; DO NOT EDIT.\n\npackage ecmascript\n\n")
	fmt.Fprintf(&source, "const UnicodeVersion = %q\n\n", unicodeVersion)
	source.WriteString("var generatedUnicodeRanges = [...]unicodeRange{\n")
	offsets := make([]int, len(tableNames))
	lengths := make([]int, len(tableNames))
	offset := 0
	for index, name := range tableNames {
		tableIndex[name] = index
		ranges := merge(tables[name])
		offsets[index] = offset
		lengths[index] = len(ranges)
		for _, interval := range ranges {
			fmt.Fprintf(&source, "{lo: 0x%X, hi: 0x%X},\n", interval.lo, interval.hi)
		}
		offset += len(ranges)
	}
	source.WriteString("}\n\nvar generatedUnicodeTables = [...]unicodeTable{\n")
	for index, name := range tableNames {
		fmt.Fprintf(&source, "{name: %q, offset: %d, length: %d},\n", name, offsets[index], lengths[index])
	}
	source.WriteString("}\n\nvar generatedUnicodeAliases = [...]unicodeAlias{\n")
	aliasNames := make([]string, 0, len(aliases))
	for name := range aliases {
		aliasNames = append(aliasNames, name)
	}
	sort.Strings(aliasNames)
	for _, alias := range aliasNames {
		index, ok := tableIndex[aliases[alias]]
		if !ok {
			return nil, fmt.Errorf("alias %q refers to unknown table %q", alias, aliases[alias])
		}
		fmt.Fprintf(&source, "{name: %q, table: %d},\n", alias, index)
	}
	source.WriteString("}\n")
	source.WriteString("\nvar generatedUnicodeStringUnits = [...]uint16{\n")
	propertyNames := make([]string, 0, len(stringProperties))
	for name := range stringProperties {
		propertyNames = append(propertyNames, name)
	}
	sort.Strings(propertyNames)
	type stringRecord struct{ offset, length int }
	records := make([]stringRecord, 0)
	tableOffsets := make([]int, len(propertyNames))
	tableLengths := make([]int, len(propertyNames))
	unitOffset := 0
	for propertyIndex, name := range propertyNames {
		sequences := stringProperties[name]
		tableOffsets[propertyIndex] = len(records)
		tableLengths[propertyIndex] = len(sequences)
		for _, sequence := range sequences {
			records = append(records, stringRecord{offset: unitOffset, length: len(sequence)})
			for _, unit := range sequence {
				fmt.Fprintf(&source, "0x%X,\n", unit)
			}
			unitOffset += len(sequence)
		}
	}
	source.WriteString("}\n\nvar generatedUnicodeStrings = [...]unicodeString{\n")
	for _, record := range records {
		fmt.Fprintf(&source, "{offset: %d, length: %d},\n", record.offset, record.length)
	}
	source.WriteString("}\n\nvar generatedUnicodeStringTables = [...]unicodeStringTable{\n")
	for index, name := range propertyNames {
		fmt.Fprintf(&source, "{name: %q, offset: %d, length: %d},\n", name, tableOffsets[index], tableLengths[index])
	}
	source.WriteString("}\n")
	writeRuneMap(&source, "generatedUnicodeFolds", "unicodeFold", folds)
	writeRuneMap(&source, "generatedLegacyUpper", "unicodeFold", legacyUpper)
	foldGroups := make(map[rune]map[rune]struct{})
	for from, canonical := range folds {
		if foldGroups[canonical] == nil {
			foldGroups[canonical] = make(map[rune]struct{})
		}
		foldGroups[canonical][from] = struct{}{}
		foldGroups[canonical][canonical] = struct{}{}
	}
	canonicals := make([]rune, 0, len(foldGroups))
	for canonical := range foldGroups {
		canonicals = append(canonicals, canonical)
	}
	sort.Slice(canonicals, func(left, right int) bool { return canonicals[left] < canonicals[right] })
	source.WriteString("\nvar generatedUnicodeFoldValues = [...]rune{\n")
	type foldGroupRecord struct {
		canonical      rune
		offset, length int
	}
	groups := make([]foldGroupRecord, 0, len(canonicals))
	valueOffset := 0
	for _, canonical := range canonicals {
		values := make([]rune, 0, len(foldGroups[canonical]))
		for value := range foldGroups[canonical] {
			values = append(values, value)
		}
		sort.Slice(values, func(left, right int) bool { return values[left] < values[right] })
		groups = append(groups, foldGroupRecord{canonical: canonical, offset: valueOffset, length: len(values)})
		for _, value := range values {
			fmt.Fprintf(&source, "0x%X,\n", value)
		}
		valueOffset += len(values)
	}
	source.WriteString("}\n\nvar generatedUnicodeFoldGroups = [...]unicodeFoldGroup{\n")
	for _, group := range groups {
		fmt.Fprintf(&source, "{canonical: 0x%X, offset: %d, length: %d},\n", group.canonical, group.offset, group.length)
	}
	source.WriteString("}\n")
	return format.Source([]byte(source.String()))
}

func writeRuneMap(source *strings.Builder, variable, recordType string, values map[rune]rune) {
	keys := make([]rune, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool { return keys[left] < keys[right] })
	fmt.Fprintf(source, "\nvar %s = [...]%s{\n", variable, recordType)
	for _, key := range keys {
		fmt.Fprintf(source, "{from: 0x%X, to: 0x%X},\n", key, values[key])
	}
	source.WriteString("}\n")
}

func parseCaseFolding(contents []byte) map[rune]rune {
	result := make(map[rune]rune)
	for _, fields := range dataLines(contents) {
		if len(fields) < 3 || fields[1] != "C" && fields[1] != "S" {
			continue
		}
		from, err := strconv.ParseInt(fields[0], 16, 32)
		check(err)
		to, err := strconv.ParseInt(fields[2], 16, 32)
		check(err)
		result[rune(from)] = rune(to)
	}
	return result
}

func parseLegacyUpper(contents []byte) map[rune]rune {
	result := make(map[rune]rune)
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ";")
		if fields[12] == "" {
			continue
		}
		from, err := strconv.ParseInt(fields[0], 16, 32)
		check(err)
		to, err := strconv.ParseInt(fields[12], 16, 32)
		check(err)
		result[rune(from)] = rune(to)
	}
	check(scanner.Err())
	return result
}

func parseEmojiProperties(sequences, zwjSequences []byte) map[string][][]uint16 {
	properties := make(map[string][][]uint16)
	parse := func(contents []byte) {
		for _, fields := range dataLines(contents) {
			if len(fields) < 2 {
				continue
			}
			codePoints := strings.Fields(fields[0])
			if len(codePoints) == 1 && strings.Contains(codePoints[0], "..") {
				interval := parseRange(codePoints[0])
				for codePoint := interval.lo; codePoint <= interval.hi; codePoint++ {
					properties[fields[1]] = append(properties[fields[1]], utf16.Encode([]rune{codePoint}))
				}
				continue
			}
			runes := make([]rune, 0, len(codePoints))
			for _, value := range codePoints {
				codePoint, err := strconv.ParseInt(value, 16, 32)
				check(err)
				runes = append(runes, rune(codePoint))
			}
			properties[fields[1]] = append(properties[fields[1]], utf16.Encode(runes))
		}
	}
	parse(sequences)
	parse(zwjSequences)
	for _, property := range []string{"Basic_Emoji", "Emoji_Keycap_Sequence", "RGI_Emoji_Modifier_Sequence", "RGI_Emoji_Flag_Sequence", "RGI_Emoji_Tag_Sequence", "RGI_Emoji_ZWJ_Sequence"} {
		properties["RGI_Emoji"] = append(properties["RGI_Emoji"], properties[property]...)
	}
	for property, values := range properties {
		seen := make(map[string][]uint16)
		for _, value := range values {
			seen[unitsKey(value)] = value
		}
		values = values[:0]
		for _, value := range seen {
			values = append(values, value)
		}
		sort.Slice(values, func(left, right int) bool { return unitsKey(values[left]) < unitsKey(values[right]) })
		properties[property] = values
	}
	return properties
}

func unitsKey(units []uint16) string {
	var result strings.Builder
	for _, unit := range units {
		fmt.Fprintf(&result, "%04X", unit)
	}
	return result.String()
}

func verifyDigest(name string, contents []byte, want string) {
	digest := sha256.Sum256(contents)
	if hex.EncodeToString(digest[:]) != want {
		fatalf("%s digest mismatch: got %x", name, digest)
	}
}

func check(err error) {
	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(1)
}
