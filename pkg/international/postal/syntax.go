package postal

import (
	"regexp"
	"strings"
)

// SyntaxDataset identifies the compatibility data behind ValidSyntax.
const SyntaxDataset = "brick/postcode@0.5.0"

// ValidSyntax reports whether value is accepted by the pinned postal-code
// syntax dataset for context. It normalizes ASCII case and removes ASCII space
// and dash separators exactly as the dataset does. Callers must bound
// untrusted input before calling it. It does not claim that a syntactically
// valid code exists, is deliverable, or belongs to an address.
func ValidSyntax(value string, countryCode string) bool {
	if len(countryCode) != 2 || value == "" {
		return false
	}

	countryCode = upperASCII(countryCode)
	normalized := upperASCII(strings.NewReplacer(" ", "", "-", "").Replace(value))
	trailingLineFeed := strings.HasSuffix(normalized, "\n")
	if trailingLineFeed {
		normalized = strings.TrimSuffix(normalized, "\n")
	}
	if normalized == "" {
		return false
	}
	for index := range normalized {
		character := normalized[index]
		if (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return false
		}
	}

	if trailingLineFeed {
		return validTrailingLineFeedSyntax(countryCode, normalized)
	}
	return validNormalizedSyntax(countryCode, normalized)
}

func validNormalizedSyntax(countryCode string, normalized string) bool {
	if valid, handled := validSpecialSyntax(countryCode, normalized); handled {
		return valid
	}
	pattern, supported := syntaxPatterns[countryCode]
	return supported && pattern.MatchString(normalized)
}

func validTrailingLineFeedSyntax(countryCode string, normalized string) bool {
	switch countryCode {
	case "AS":
		return digits(normalized, 9) && strings.HasPrefix(normalized, "96799")
	case "CY":
		return digits(normalized, 4) && strings.HasPrefix(normalized, "99")
	case "GB":
		return normalized != "GIR0AA" && validUnitedKingdom(normalized)
	case "AI", "AQ", "AX", "BL", "CR", "FK", "FM", "GI", "GS", "GU", "IO",
		"LT", "MF", "MH", "MP", "PM", "PN", "PR", "PW", "SA", "SH", "TC",
		"TW", "US", "VA", "VI":
		return false
	default:
		return validNormalizedSyntax(countryCode, normalized)
	}
}

func validSpecialSyntax(code string, value string) (bool, bool) {
	switch code {
	case "AD":
		return digits(stripPrefix(value, "AD"), 3), true
	case "AF":
		return digits(value, 4) && value[:2] >= "10" && value[:2] <= "43", true
	case "AI":
		return value == "2640" || value == "AI2640", true
	case "AQ":
		return value == "BIQQ1ZZ", true
	case "AS":
		return (len(value) == 5 || len(value) == 9) && strings.HasPrefix(value, "96799") && digits(value, len(value)), true
	case "AT":
		value = stripPrefix(value, "A")
		return digits(value, 4) && value[0] != '0', true
	case "AX":
		value = stripPrefix(value, "AX")
		return digits(value, 5) && strings.HasPrefix(value, "22"), true
	case "AZ":
		return digits(stripPrefix(value, "AZ"), 4), true
	case "BB":
		return digits(stripPrefix(value, "BB"), 5), true
	case "BE":
		return digits(stripPrefix(value, "B"), 4), true
	case "BH":
		return validBahrain(value), true
	case "BL":
		return value == "97133", true
	case "CA":
		return validCanada(value), true
	case "CN":
		return digits(value, 6) && value[:2] != "00", true
	case "CO":
		return validColombia(value), true
	case "CR":
		return digits(value, 5) || digits(value, 9), true
	case "CY":
		return digits(value, 4) || digits(value, 5) && strings.HasPrefix(value, "99"), true
	case "CZ":
		return digits(value, 5) && value[0] >= '1' && value[0] <= '7', true
	case "FK":
		return value == "FIQQ1ZZ", true
	case "FM":
		return validUSRange(value, "96941", "96944"), true
	case "GB":
		return validUnitedKingdom(value), true
	case "GF":
		return digits(value, 5) && strings.HasPrefix(value, "973"), true
	case "GI":
		return value == "GX111AA", true
	case "GS":
		return value == "SIQQ1ZZ", true
	case "GU":
		return validUSRange(value, "96910", "96932"), true
	case "HU", "ID", "IN", "IS":
		length := map[string]int{"HU": 4, "ID": 5, "IN": 6, "IS": 3}[code]
		return digits(value, length) && value[0] != '0', true
	case "IE":
		return irelandPattern.MatchString(value), true
	case "IM":
		return isleOfManPattern.MatchString(value), true
	case "IO":
		return value == "BBND1ZZ", true
	case "JE":
		return jerseyPattern.MatchString(value), true
	case "KY":
		return len(value) == 7 && strings.HasPrefix(value, "KY") && value[2] >= '1' && value[2] <= '3' && digits(value[2:], 5), true
	case "LI":
		return digits(value, 4) && value >= "9485" && value <= "9498", true
	case "LT":
		return digits(stripPrefix(value, "LT"), 5), true
	case "LU":
		return digits(stripPrefix(value, "L"), 4), true
	case "LV":
		return digits(stripPrefix(value, "LV"), 4), true
	case "MC":
		return digits(value, 5) && strings.HasPrefix(value, "980"), true
	case "MD":
		return digits(stripPrefix(value, "MD"), 4), true
	case "MF":
		return value == "97150", true
	case "MH":
		return validUSRange(value, "96960", "96970"), true
	case "MP":
		return validUSRange(value, "96950", "96952"), true
	case "MQ":
		return digits(value, 5) && value >= "97200" && value <= "97290", true
	case "MS":
		value = stripPrefix(value, "MSR")
		return digits(value, 4) && value >= "1110" && value <= "1350", true
	case "NC":
		return digits(value, 5) && value >= "98800" && value <= "98890", true
	case "NL":
		return validNetherlands(value), true
	case "PF":
		return digits(value, 5) && strings.HasPrefix(value, "987"), true
	case "PM":
		return value == "97500", true
	case "PN":
		return value == "PCRN1ZZ", true
	case "PR":
		return validPuertoRico(value), true
	case "PW":
		return validUSRange(value, "96940", "96940"), true
	case "RE":
		return digits(value, 5) && strings.HasPrefix(value, "974"), true
	case "SE":
		return digits(value, 5) && value >= "10000" && value <= "98499", true
	case "SH":
		return value == "STHL1ZZ" || value == "ASCN1ZZ" || value == "TDCU1ZZ", true
	case "SK":
		return digits(value, 5) && strings.ContainsRune("890", rune(value[0])), true
	case "SM":
		return digits(value, 5) && strings.HasPrefix(value, "4789"), true
	case "TC":
		return value == "TKCA1ZZ", true
	case "TF":
		return digits(value, 5) && strings.HasPrefix(value, "984"), true
	case "TW":
		return digits(value, 3) || digits(value, 5), true
	case "US", "SA":
		return digits(value, 5) || digits(value, 9), true
	case "VA":
		return value == "00120", true
	case "VC":
		return digits(stripPrefix(value, "VC"), 4), true
	case "VG":
		value = stripPrefix(value, "VG")
		return digits(value, 4) && value >= "1110" && value <= "1160", true
	case "VI":
		return validUSRange(value, "00801", "00851"), true
	case "WF":
		return digits(value, 5) && value >= "98600" && value <= "98690", true
	case "WS":
		return digits(stripPrefix(value, "WS"), 4), true
	case "YT":
		return digits(value, 5) && value >= "97600" && value <= "97690", true
	default:
		return false, false
	}
}

func digits(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for index := range value {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func stripPrefix(value string, prefix string) string {
	return strings.TrimPrefix(value, prefix)
}

func validUSRange(value string, lower string, upper string) bool {
	if !digits(value, 5) && !digits(value, 9) {
		return false
	}
	return value[:5] >= lower && value[:5] <= upper
}

func validBahrain(value string) bool {
	if !digits(value, 3) && !digits(value, 4) {
		return false
	}
	municipality := value[:len(value)-2]
	validMunicipality := len(municipality) == 1 && municipality >= "1" && municipality <= "9" ||
		len(municipality) == 2 && municipality >= "10" && municipality <= "12"
	return validMunicipality && value[len(value)-2:] != "00"
}

func validCanada(value string) bool {
	if !canadaPattern.MatchString(value) {
		return false
	}
	return value[0] != 'W' && value[0] != 'Z'
}

func validColombia(value string) bool {
	if !digits(value, 6) || value[2:] == "0000" {
		return false
	}
	return strings.Contains(
		" 05 08 11 13 15 17 18 19 20 23 25 27 41 44 47 50 52 54 63 66 68 70 73 76 81 85 86 88 91 94 95 97 99 ",
		" "+value[:2]+" ",
	)
}

func validNetherlands(value string) bool {
	if !netherlandsPattern.MatchString(value) {
		return false
	}
	suffix := value[4:]
	return suffix != "SS" && suffix != "SD" && suffix != "SA"
}

func validPuertoRico(value string) bool {
	if !digits(value, 5) && !digits(value, 9) {
		return false
	}
	zip := value[:5]
	return zip >= "00600" && zip <= "00799" || zip >= "00900" && zip <= "00999"
}

var (
	canadaPattern      = regexp.MustCompile(`^([ABCEGHJ-NPRSTV-Z][0-9]){3}$`)
	netherlandsPattern = regexp.MustCompile(`^[1-9][0-9]{3}[A-Z]{2}$`)

	irelandPattern = regexp.MustCompile(
		`^(A41|A42|A45|A63|A67|A75|A81|A82|A83|A84|A85|A86|A91|A92|A94|A96|A98|C15|D01|D02|D03|D04|D05|D06|D6W|D07|D08|D09|D10|D11|D12|D13|D14|D15|D16|D17|D18|D20|D22|D24|E21|E25|E32|E34|E41|E45|E53|E91|F12|F23|F26|F28|F31|F35|F42|F45|F52|F56|F91|F92|F93|F94|H12|H14|H16|H18|H23|H53|H54|H62|H65|H71|H91|K32|K34|K36|K45|K56|K67|K78|N37|N39|N41|N91|P12|P14|P17|P24|P25|P31|P32|P36|P43|P47|P51|P56|P61|P67|P72|P75|P81|P85|R14|R21|R32|R35|R42|R45|R51|R56|R93|R95|T12|T23|T34|T45|T56|V14|V15|V23|V31|V35|V42|V92|V93|V94|V95|W12|W23|W34|W91|X35|X42|X91|Y14|Y21|Y25|Y34|Y35)[ACDEFHKNPRTVWXY0-9]{4}$`,
	)
	isleOfManPattern = regexp.MustCompile(`^IM[0-9][0-9]?[0-9][A-Z]{2}$`)
	jerseyPattern    = regexp.MustCompile(`^JE[0-9][0-9]?[0-9][A-Z]{2}$`)

	unitedKingdomPattern = regexp.MustCompile(
		`^(?:[ABCDEFGHIJKLMNOPRSTUWYZ][0-9](?:[ABCDEFGHJKPSTUW]|[0-9])?|[ABCDEFGHIJKLMNOPRSTUWYZ][ABCDEFGHKLMNOPQRSTUVWXY][0-9](?:[ABEHMNPRVWXY]|[0-9])?)[0-9][ABCDEFGHJLNPQRSTUWXYZ]{2}$`,
	)

	syntaxPatterns = map[string]*regexp.Regexp{
		"AL": regexp.MustCompile(`^[0-9]{4}$`),
		"AM": regexp.MustCompile(`^[0-9]{4}$`),
		"AR": regexp.MustCompile(`^(([0-9]{4})|([A-Z][0-9]{4}[A-Z]{3}))$`),
		"AU": regexp.MustCompile(`^[0-9]{4}$`),
		"BA": regexp.MustCompile(`^[0-9]{5}$`),
		"BD": regexp.MustCompile(`^[0-9]{4}$`),
		"BG": regexp.MustCompile(`^[0-9]{4}$`),
		"BM": regexp.MustCompile(`^([A-Z]{2})([A-Z]{2}|[0-9]{2})$`),
		"BN": regexp.MustCompile(`^[A-Z]{2}[0-9]{4}$`),
		"BR": regexp.MustCompile(`^[0-9]{8}$`),
		"BT": regexp.MustCompile(`^[0-9]{5}$`),
		"BY": regexp.MustCompile(`^[0-9]{6}$`),
		"CC": regexp.MustCompile(`^[0-9]{4}$`),
		"CH": regexp.MustCompile(`^[0-9]{4}$`),
		"CL": regexp.MustCompile(`^[0-9]{7}$`),
		"CU": regexp.MustCompile(`^[0-9]{5}$`),
		"CV": regexp.MustCompile(`^[0-9]{4}$`),
		"CX": regexp.MustCompile(`^[0-9]{4}$`),
		"DE": regexp.MustCompile(`^[0-9]{5}$`),
		"DK": regexp.MustCompile(`^[0-9]{4}$`),
		"DO": regexp.MustCompile(`^[0-9]{5}$`),
		"DZ": regexp.MustCompile(`^[0-9]{5}$`),
		"EC": regexp.MustCompile(`^[0-9]{6}$`),
		"EE": regexp.MustCompile(`^[0-9]{5}$`),
		"EG": regexp.MustCompile(`^[0-9]{5}([0-9]{2})?$`),
		"ES": regexp.MustCompile(`^[0-9]{5}$`),
		"ET": regexp.MustCompile(`^[0-9]{4}$`),
		"FI": regexp.MustCompile(`^[0-9]{5}$`),
		"FO": regexp.MustCompile(`^[0-9]{3}$`),
		"FR": regexp.MustCompile(`^[0-9]{5}$`),
		"GE": regexp.MustCompile(`^[0-9]{4}$`),
		"GG": regexp.MustCompile(`^(GY[0-9]{1,2})([0-9][A-Z][A-Z])$`),
		"GL": regexp.MustCompile(`^[0-9]{4}$`),
		"GN": regexp.MustCompile(`^[0-9]{3}$`),
		"GP": regexp.MustCompile(`^971[0-9]{2}$`),
		"GR": regexp.MustCompile(`^[0-9]{5}$`),
		"GT": regexp.MustCompile(`^[0-9]{5}$`),
		"GW": regexp.MustCompile(`^[0-9]{4}$`),
		"HN": regexp.MustCompile(`^[0-9]{5}$`),
		"HR": regexp.MustCompile(`^[0-9]{5}$`),
		"HT": regexp.MustCompile(`^[0-9]{4}$`),
		"IC": regexp.MustCompile(`^(35|38)[0-9]{3}$`),
		"IL": regexp.MustCompile(`^[0-9]{7}$`),
		"IQ": regexp.MustCompile(`^[0-9]{5}$`),
		"IR": regexp.MustCompile(`^[0-9]{10}$`),
		"IT": regexp.MustCompile(`^[0-9]{5}$`),
		"JO": regexp.MustCompile(`^[0-9]{5}$`),
		"JP": regexp.MustCompile(`^[0-9]{7}$`),
		"KE": regexp.MustCompile(`^[0-9]{5}$`),
		"KG": regexp.MustCompile(`^[0-9]{6}$`),
		"KH": regexp.MustCompile(`^[0-9]{5}$`),
		"KR": regexp.MustCompile(`^[0-9]{5}$`),
		"KW": regexp.MustCompile(`^[0-9]{5}$`),
		"KZ": regexp.MustCompile(`^[0-9]{6}$`),
		"LA": regexp.MustCompile(`^[0-9]{5}$`),
		"LB": regexp.MustCompile(`^[0-9]{8}$`),
		"LC": regexp.MustCompile(`^(LC[0-9]{2})([0-9]{3})$`),
		"LK": regexp.MustCompile(`^[0-9]{5}$`),
		"LR": regexp.MustCompile(`^[0-9]{4}$`),
		"LS": regexp.MustCompile(`^[0-9]{3}$`),
		"MA": regexp.MustCompile(`^[0-9]{5}$`),
		"ME": regexp.MustCompile(`^[0-9]{5}$`),
		"MG": regexp.MustCompile(`^[0-9]{3}$`),
		"MK": regexp.MustCompile(`^[0-9]{4}$`),
		"MM": regexp.MustCompile(`^[0-9]{5}$`),
		"MN": regexp.MustCompile(`^[0-9]{5}$`),
		"MT": regexp.MustCompile(`^([A-Z]{3})([0-9]{4})$`),
		"MU": regexp.MustCompile(`^[0-9]{5}$`),
		"MV": regexp.MustCompile(`^[0-9]{5}$`),
		"MX": regexp.MustCompile(`^[0-9]{5}$`),
		"MY": regexp.MustCompile(`^[0-9]{5}$`),
		"MZ": regexp.MustCompile(`^[0-9]{4}$`),
		"NE": regexp.MustCompile(`^[0-9]{4}$`),
		"NF": regexp.MustCompile(`^[0-9]{4}$`),
		"NG": regexp.MustCompile(`^[0-9]{6}$`),
		"NI": regexp.MustCompile(`^[0-9]{5}$`),
		"NO": regexp.MustCompile(`^[0-9]{4}$`),
		"NP": regexp.MustCompile(`^[0-9]{5}$`),
		"NZ": regexp.MustCompile(`^[0-9]{4}$`),
		"OM": regexp.MustCompile(`^[0-9]{3}$`),
		"PA": regexp.MustCompile(`^[0-9]{4}$`),
		"PE": regexp.MustCompile(`^[0-9]{5}$`),
		"PG": regexp.MustCompile(`^[0-9]{3}$`),
		"PH": regexp.MustCompile(`^[0-9]{4}$`),
		"PK": regexp.MustCompile(`^[0-9]{5}$`),
		"PL": regexp.MustCompile(`^[0-9]{5}$`),
		"PS": regexp.MustCompile(`^[0-9]{3}$`),
		"PT": regexp.MustCompile(`^[0-9]{7}$`),
		"PY": regexp.MustCompile(`^[0-9]{4}$`),
		"RO": regexp.MustCompile(`^[0-9]{6}$`),
		"RS": regexp.MustCompile(`^[0-9]{5}$`),
		"RU": regexp.MustCompile(`^[0-9]{6}$`),
		"SA": regexp.MustCompile(`^[0-9]+$`),
		"SD": regexp.MustCompile(`^[0-9]{5}$`),
		"SG": regexp.MustCompile(`^[0-9]{6}$`),
		"SI": regexp.MustCompile(`^[0-9]{4}$`),
		"SJ": regexp.MustCompile(`^[0-9]{4}$`),
		"SN": regexp.MustCompile(`^[0-9]{5}$`),
		"SV": regexp.MustCompile(`^[0-9]{4}$`),
		"SZ": regexp.MustCompile(`^[A-Z][0-9]{3}$`),
		"TH": regexp.MustCompile(`^[0-9]{5}$`),
		"TJ": regexp.MustCompile(`^[0-9]{6}$`),
		"TM": regexp.MustCompile(`^[0-9]{6}$`),
		"TN": regexp.MustCompile(`^[0-9]{4}$`),
		"TR": regexp.MustCompile(`^[0-9]{5}$`),
		"TT": regexp.MustCompile(`^[0-9]{6}$`),
		"TZ": regexp.MustCompile(`^[0-9]{5}$`),
		"UA": regexp.MustCompile(`^[0-9]{5}$`),
		"UY": regexp.MustCompile(`^[0-9]{5}$`),
		"UZ": regexp.MustCompile(`^[0-9]{6}$`),
		"VE": regexp.MustCompile(`^[0-9]{4}$`),
		"VN": regexp.MustCompile(`^[0-9]{6}$`),
		"ZA": regexp.MustCompile(`^[0-9]{4}$`),
		"ZM": regexp.MustCompile(`^[0-9]{5}$`),
	}
)

func validUnitedKingdom(value string) bool {
	if value == "GIR0AA" {
		return true
	}
	if !unitedKingdomPattern.MatchString(value) {
		return false
	}
	areaLength := 0
	for areaLength < len(value) && value[areaLength] >= 'A' && value[areaLength] <= 'Z' {
		areaLength++
	}
	area := value[:areaLength]
	return strings.Contains(
		" AB AL B BA BB BD BH BL BN BR BS BT CA CB CF CH CM CO CR CT CV CW DA DD DE DG DH DL DN DT DY E EC EH EN EX FK FY G GL GU HA HD HG HP HR HS HU HX IG IP IV KA KT KW KY L LA LD LE LL LN LS LU M ME MK ML N NE NG NN NP NR NW OL OX PA PE PH PL PO PR RG RH RM S SA SE SG SK SL SM SN SO SP SR SS ST SW SY TA TD TF TN TQ TR TS TW UB W WA WC WD WF WN WR WS WV YO ZE BF BX XX ",
		" "+area+" ",
	)
}
