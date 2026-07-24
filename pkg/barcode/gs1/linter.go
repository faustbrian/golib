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
		return strings.IndexFunc(value, func(character rune) bool { return character < '0' || character > '9' }) >= 0
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
		return err == nil && len(value) == 2 && (media >= 1 && media <= 10 || media >= 80 && media <= 99)
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
	if len(value) != 3 || !allDigits(value) {
		return false
	}
	code, _ := strconv.Atoi(value)
	return allocations[code/64]&(uint64(1)<<(63-code%64)) != 0
}

func validAlpha2Allocation(value string) bool {
	if len(value) != 2 || value[0] < 'A' || value[0] > 'Z' || value[1] < 'A' || value[1] > 'Z' {
		return false
	}
	code := int(value[0]-'A')*26 + int(value[1]-'A')
	return iso3166Alpha2Codes[code/64]&(uint64(1)<<(63-code%64)) != 0
}

func validAlphaCheckPair(value string) bool {
	if len(value) < 2 || len(value) > len(alphaCheckPrimes)+2 {
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
		if weight < 0 {
			return false
		}
		sum += weight * alphaCheckPrimes[len(value)-3-index]
	}
	sum %= 1021
	return value[len(value)-2] == checkCharacters[sum>>5] &&
		value[len(value)-1] == checkCharacters[sum&31]
}

func validCouponPositiveOffer(value string) bool {
	if !allDigits(value) || len(value) < 2 || value[0] > '1' || value[1] > '6' {
		return false
	}
	position := 2 + int(value[1]-'0') + 6
	if len(value) < position+7 {
		return false
	}
	position += 6
	serialLength := int(value[position]-'0') + 6
	return len(value) == position+1+serialLength
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
		if indicator < minimum || indicator > maximum {
			return false
		}
		length := indicator + offset
		position++
		return consume(length)
	}
	consumeRequirement := func() bool {
		if !consumeVLI(1, 5, 0) || position == len(value) ||
			value[position] > '4' && value[position] != '9' {
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

	if !consumeGCP(false) || !consume(6) || !consumeVLI(1, 5, 0) ||
		!consumeRequirement() {
		return false
	}
	if position < len(value) && value[position] == '1' {
		position++
		if position == len(value) || value[position] > '3' {
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
		if !validDate(start, 2, false) || expiration != "" && start > expiration {
			return false
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
		if !strings.Contains("01256", miscellaneous[0:1]) ||
			miscellaneous[1] > '2' || miscellaneous[3] > '1' {
			return false
		}
	}

	return position == len(value)
}

func validDate(value string, yearDigits int, allowDayZero bool) bool {
	if len(value) != yearDigits+4 || !allDigits(value) {
		return false
	}
	year, _ := strconv.Atoi(value[:yearDigits])
	if yearDigits == 2 {
		year += 2000
	}
	month, _ := strconv.Atoi(value[yearDigits : yearDigits+2])
	day, _ := strconv.Atoi(value[yearDigits+2:])
	if allowDayZero && day == 0 {
		return month >= 1 && month <= 12
	}
	if month < 1 || day < 1 {
		return false
	}
	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)

	return date.Year() == year && int(date.Month()) == month && date.Day() == day
}

func validRange(value string, minimum, maximum int) bool {
	parsed, err := strconv.Atoi(value)
	return err == nil && len(value) == 2 && parsed >= minimum && parsed <= maximum
}

func validMaximum(value string, length int, maximum uint64) bool {
	parsed, err := strconv.ParseUint(value, 10, 64)
	return err == nil && len(value) == length && parsed <= maximum
}

func validPercentEncoding(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] != '%' {
			continue
		}
		if index+2 >= len(value) || !isHex(value[index+1]) || !isHex(value[index+2]) {
			return false
		}
		index += 2
	}

	return true
}

func isHex(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'A' && value <= 'F' || value >= 'a' && value <= 'f'
}

func validPieceOfTotal(value string) bool {
	if len(value) == 0 || len(value)%2 != 0 || !allDigits(value) {
		return false
	}
	half := len(value) / 2
	piece, total := value[:half], value[half:]

	return strings.Trim(piece, "0") != "" && strings.Trim(total, "0") != "" && piece <= total
}

func validPositionInSequence(value string) bool {
	position, total, found := strings.Cut(value, "/")
	if !found || position == "" || total == "" || !allDigits(position) || !allDigits(total) ||
		position[0] == '0' || total[0] == '0' || len(position) > len(total) {
		return false
	}
	return len(position) < len(total) || position <= total
}

func validIBAN(value string) bool {
	if len(value) < 5 || len(value) > 34 || value[0] < 'A' || value[0] > 'Z' ||
		value[1] < 'A' || value[1] > 'Z' || !allDigits(value[2:4]) {
		return false
	}
	reordered := value[4:] + value[:4]
	remainder := 0
	for _, character := range reordered {
		switch {
		case character >= '0' && character <= '9':
			remainder = (remainder*10 + int(character-'0')) % 97
		case character >= 'A' && character <= 'Z':
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
		if character < '0' || character > '9' {
			return false
		}
	}

	return true
}
