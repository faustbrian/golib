package gs1

import (
	"strconv"
	"strings"
	"time"
)

func validateLinter(name, value string) bool {
	switch name {
	case "csum":
		return ValidateCheckDigit(value) == nil
	case "yymmd0":
		return validDate(value, 2, true)
	case "yymmdd":
		return validDate(value, 2, false)
	case "yyyymmdd":
		return validDate(value, 4, false)
	case "hh":
		return validRange(value, 0, 23)
	case "mi", "ss":
		return validRange(value, 0, 59)
	case "hhmi":
		return len(value) == 4 && validRange(value[:2], 0, 23) && validRange(value[2:], 0, 59)
	case "hyphen":
		return value != "" && strings.Trim(value, "-") == ""
	case "hasnondigit":
		if value == "" {
			return false
		}
		return !allDigits(value)
	case "nonzero":
		return value != "" && strings.Trim(value, "0") != ""
	case "zero":
		return value != "" && strings.Trim(value, "0") == ""
	case "nozeroprefix":
		return value != "" && value[0] != '0'
	case "yesno":
		return value == "0" || value == "1"
	case "winding":
		return value == "0" || value == "1" || value == "9"
	case "iso5218":
		return value == "0" || value == "1" || value == "2" || value == "9"
	case "latitude":
		return validMaximum(value, 10, 1_800_000_000)
	case "longitude":
		return validMaximum(value, 10, 3_600_000_000)
	case "pcenc":
		return validPercentEncoding(value)
	case "pieceoftotal":
		return validPieceOfTotal(value)
	case "posinseqslash":
		return validPositionInSequence(value)
	case "importeridx":
		return len(value) == 1 && strings.Contains("-0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz", value)
	case "mediatype":
		media, err := strconv.Atoi(value)
		if err != nil {
			return false
		}
		if len(value) != 2 {
			return false
		}
		if media >= 1 && media <= 10 {
			return true
		}
		return media >= 80 && media <= 99
	case "iban":
		return validIBAN(value)
	case "csumalpha":
		return validAlphaCheckPair(value)
	case "iso3166":
		return validNumericAllocation(value, iso3166Codes)
	case "iso3166999":
		return value == "999" || validNumericAllocation(value, iso3166Codes)
	case "iso3166alpha2":
		return validAlpha2Allocation(value)
	case "iso4217":
		return validNumericAllocation(value, iso4217Codes)
	case "packagetype":
		return validPackageType(value)
	case "couponposoffer":
		return validCouponPositiveOffer(value)
	case "couponcode":
		return validCouponCode(value)
	case "gcppos1", "gcppos2":
		// Company-prefix allocation lookup is an optional online hook in the
		// reference implementation; structural position remains valid offline.
		return true
	default:
		return false
	}
}

func validNumericAllocation(value string, allocations []uint64) bool {
	if len(value) != 3 {
		return false
	}
	if !allDigits(value) {
		return false
	}
	code, _ := strconv.Atoi(value)
	return allocations[code/64]&(uint64(1)<<(63-code%64)) != 0
}

func validAlpha2Allocation(value string) bool {
	if len(value) != 2 {
		return false
	}
	if !isUpperASCII(rune(value[0])) {
		return false
	}
	if !isUpperASCII(rune(value[1])) {
		return false
	}
	code := int(value[0]-'A')*26 + int(value[1]-'A')
	return iso3166Alpha2Codes[code/64]&(uint64(1)<<(63-code%64)) != 0
}

func validAlphaCheckPair(value string) bool {
	if len(value) < 2 {
		return false
	}
	if len(value) > len(alphaCheckPrimes)+2 {
		return false
	}
	if len(value) == 2 {
		return value == "22"
	}
	const checkCharacters = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	const dataCharacters = "!\"%&'()*+,-./0123456789:;<=>?ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz"
	sum := 0
	for index := 0; index < len(value)-2; index++ {
		weight := strings.IndexByte(dataCharacters, value[index])
		if weight == -1 {
			return false
		}
		sum += weight * alphaCheckPrimes[len(value)-3-index]
	}
	sum = sum % 1021
	return value[len(value)-2] == checkCharacters[sum>>5] &&
		value[len(value)-1] == checkCharacters[sum&31]
}

func validCouponPositiveOffer(value string) bool {
	if !allDigits(value) {
		return false
	}
	if len(value) == 1 {
		return false
	}
	if value[0] > '1' {
		return false
	}
	if value[1] > '6' {
		return false
	}
	serialPosition := 2 + int(value[1]-'0') + 6 + 6
	if len(value) <= serialPosition {
		return false
	}
	serialLength := int(value[serialPosition]-'0') + 6
	return len(value) == serialPosition+1+serialLength
}

func validCouponCode(value string) bool {
	if !allDigits(value) {
		return false
	}
	position := 0
	consume := func(length int) bool {
		if len(value)-position < length {
			return false
		}
		position += length
		return true
	}
	consumeVLI := func(minimum, maximum, offset int) bool {
		if position == len(value) {
			return false
		}
		indicator := int(value[position] - '0')
		if indicator < minimum {
			return false
		}
		if indicator > maximum {
			return false
		}
		length := indicator + offset
		position++
		return consume(length)
	}
	consumeRequirement := func() bool {
		if !consumeVLI(1, 5, 0) {
			return false
		}
		if position == len(value) {
			return false
		}
		if value[position] > '4' {
			if value[position] == '9' {
				position++
				return consume(3)
			}
			return false
		}
		position++
		return consume(3)
	}
	consumeGCP := func(allowAbsent bool) bool {
		if position == len(value) {
			return false
		}
		if allowAbsent && value[position] == '9' {
			position++
			return true
		}
		return consumeVLI(0, 6, 6)
	}

	if !consumeGCP(false) {
		return false
	}
	if !consume(6) {
		return false
	}
	if !consumeVLI(1, 5, 0) {
		return false
	}
	if !consumeRequirement() {
		return false
	}
	if position < len(value) && value[position] == '1' {
		position++
		if position == len(value) {
			return false
		}
		if value[position] > '3' {
			return false
		}
		position++
		if !consumeRequirement() || !consumeGCP(true) {
			return false
		}
	}
	if position < len(value) && value[position] == '2' {
		position++
		if !consumeRequirement() || !consumeGCP(true) {
			return false
		}
	}
	var expiration string
	if position < len(value) && value[position] == '3' {
		position++
		if !consume(6) {
			return false
		}
		expiration = value[position-6 : position]
		if !validDate(expiration, 2, false) {
			return false
		}
	}
	if position < len(value) && value[position] == '4' {
		position++
		if !consume(6) {
			return false
		}
		start := value[position-6 : position]
		if !validDate(start, 2, false) {
			return false
		}
		if expiration != "" {
			if start > expiration {
				return false
			}
		}
	}
	if position < len(value) && value[position] == '5' {
		position++
		if !consumeVLI(0, 9, 6) {
			return false
		}
	}
	if position < len(value) && value[position] == '6' {
		position++
		if !consumeVLI(1, 7, 6) {
			return false
		}
	}
	if position < len(value) && value[position] == '9' {
		position++
		if !consume(4) {
			return false
		}
		miscellaneous := value[position-4 : position]
		if !strings.Contains("01256", miscellaneous[0:1]) {
			return false
		}
		if miscellaneous[1] > '2' {
			return false
		}
		if miscellaneous[3] > '1' {
			return false
		}
	}

	return position == len(value)
}

func validDate(value string, yearDigits int, allowDayZero bool) bool {
	if len(value) != yearDigits+4 {
		return false
	}
	if !allDigits(value) {
		return false
	}
	year, _ := strconv.Atoi(value[:yearDigits])
	month, _ := strconv.Atoi(value[yearDigits : yearDigits+2])
	day, _ := strconv.Atoi(value[yearDigits+2:])
	if allowDayZero {
		if day == 0 {
			if month < 1 {
				return false
			}
			return month <= 12
		}
	}
	if month < 1 {
		return false
	}
	if day < 1 {
		return false
	}
	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)

	if date.Year() != year {
		return false
	}
	if int(date.Month()) != month {
		return false
	}
	return date.Day() == day
}

func validRange(value string, minimum, maximum int) bool {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return false
	}
	if len(value) != 2 {
		return false
	}
	if parsed < minimum {
		return false
	}
	return parsed <= maximum
}

func validMaximum(value string, length int, maximum uint64) bool {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return false
	}
	if len(value) != length {
		return false
	}

	return parsed <= maximum
}

func validPercentEncoding(value string) bool {
	for index := range len(value) {
		if value[index] != '%' {
			continue
		}
		if index+2 >= len(value) {
			return false
		}
		if !isHex(value[index+1]) {
			return false
		}
		if !isHex(value[index+2]) {
			return false
		}
	}

	return true
}

func isHex(value byte) bool {
	if value >= '0' && value <= '9' {
		return true
	}
	if value >= 'A' && value <= 'F' {
		return true
	}
	return value >= 'a' && value <= 'f'
}

func validPieceOfTotal(value string) bool {
	if len(value) == 0 {
		return false
	}
	if len(value)%2 != 0 {
		return false
	}
	if !allDigits(value) {
		return false
	}
	half := len(value) / 2
	piece, total := value[:half], value[half:]

	if strings.Trim(piece, "0") == "" {
		return false
	}
	if strings.Trim(total, "0") == "" {
		return false
	}
	return piece <= total
}

func validPositionInSequence(value string) bool {
	position, total, found := strings.Cut(value, "/")
	if !found {
		return false
	}
	if position == "" {
		return false
	}
	if total == "" {
		return false
	}
	if !allDigits(position) {
		return false
	}
	if !allDigits(total) {
		return false
	}
	if position[0] == '0' {
		return false
	}
	if total[0] == '0' {
		return false
	}
	if len(position) > len(total) {
		return false
	}
	if len(position) < len(total) {
		return true
	}
	return position <= total
}

func validIBAN(value string) bool {
	if len(value) < 5 {
		return false
	}
	if len(value) > 34 {
		return false
	}
	if !isUpperASCII(rune(value[0])) {
		return false
	}
	if !isUpperASCII(rune(value[1])) {
		return false
	}
	if !allDigits(value[2:4]) {
		return false
	}
	reordered := value[4:] + value[:4]
	remainder := 0
	for _, character := range reordered {
		switch {
		case isDigitASCII(character):
			remainder = (remainder*10 + int(character-'0')) % 97
		case isUpperASCII(character):
			number := int(character-'A') + 10
			remainder = (remainder*100 + number) % 97
		default:
			return false
		}
	}

	return remainder == 1
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if !isDigitASCII(character) {
			return false
		}
	}

	return true
}

func isDigitASCII(character rune) bool {
	return character >= '0' && character <= '9'
}

func isUpperASCII(character rune) bool {
	return character >= 'A' && character <= 'Z'
}
