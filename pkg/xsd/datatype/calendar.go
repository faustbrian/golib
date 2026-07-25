package datatype

import (
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

var (
	dateLexical      = regexp.MustCompile(`^(-?)([0-9]{4,})-([0-9]{2})-([0-9]{2})$`)
	timeLexical      = regexp.MustCompile(`^([0-9]{2}):([0-9]{2}):([0-9]{2})(\.[0-9]+)?$`)
	yearMonthLexical = regexp.MustCompile(`^(-?)([0-9]{4,})-([0-9]{2})$`)
	yearLexical      = regexp.MustCompile(`^(-?)([0-9]{4,})$`)
	monthDayLexical  = regexp.MustCompile(`^--([0-9]{2})-([0-9]{2})$`)
	dayLexical       = regexp.MustCompile(`^---([0-9]{2})$`)
	monthLexical     = regexp.MustCompile(`^--([0-9]{2})(?:--)?$`)
	durationLexical  = regexp.MustCompile(
		`^-?P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`,
	)
)

func validCalendarLexical(name string, lexical string) bool {
	core, timezoneValid := stripTimezone(lexical)
	if !timezoneValid {
		return false
	}
	switch name {
	case "dateTime":
		parts := strings.Split(core, "T")
		return len(parts) == 2 && validDate(parts[0]) && validTime(parts[1])
	case "date":
		return validDate(core)
	case "time":
		return validTime(core)
	case "gYearMonth":
		match := yearMonthLexical.FindStringSubmatch(core)
		return match != nil && validYear(match[2]) && numberIn(match[3], 1, 12)
	case "gYear":
		match := yearLexical.FindStringSubmatch(core)
		return match != nil && validYear(match[2])
	case "gMonthDay":
		match := monthDayLexical.FindStringSubmatch(core)
		return match != nil && validMonthDay(match[1], match[2], true)
	case "gDay":
		match := dayLexical.FindStringSubmatch(core)
		return match != nil && numberIn(match[1], 1, 31)
	case "gMonth":
		match := monthLexical.FindStringSubmatch(core)
		return match != nil && numberIn(match[1], 1, 12)
	default:
		return false
	}
}

func validDuration(lexical string) bool {
	match := durationLexical.FindStringSubmatch(lexical)
	if match == nil {
		return false
	}
	hasComponent := false
	for _, component := range match[1:] {
		if component != "" {
			hasComponent = true
			break
		}
	}
	if !hasComponent {
		return false
	}
	if strings.Contains(lexical, "T") && match[4] == "" && match[5] == "" && match[6] == "" {
		return false
	}
	return true
}

func stripTimezone(lexical string) (string, bool) {
	if strings.HasSuffix(lexical, "Z") {
		return strings.TrimSuffix(lexical, "Z"), true
	}
	if len(lexical) < 6 {
		return lexical, true
	}
	zone := lexical[len(lexical)-6:]
	if (zone[0] != '+' && zone[0] != '-') || zone[3] != ':' {
		return lexical, true
	}
	hours, hoursErr := strconv.Atoi(zone[1:3])
	minutes, minutesErr := strconv.Atoi(zone[4:6])
	if hoursErr != nil || minutesErr != nil || minutes > 59 || hours > 14 ||
		hours == 14 && minutes != 0 {
		return "", false
	}
	return lexical[:len(lexical)-6], true
}

func validDate(lexical string) bool {
	match := dateLexical.FindStringSubmatch(lexical)
	if match == nil || !validYear(match[2]) {
		return false
	}
	leap := leapYear(match[2])
	return validMonthDay(match[3], match[4], leap)
}

func validTime(lexical string) bool {
	match := timeLexical.FindStringSubmatch(lexical)
	if match == nil {
		return false
	}
	hours, _ := strconv.Atoi(match[1])
	minutes, _ := strconv.Atoi(match[2])
	seconds, _ := strconv.Atoi(match[3])
	if hours == 24 {
		return minutes == 0 && seconds == 0 && match[4] == ""
	}
	return hours <= 23 && minutes <= 59 && seconds <= 59
}

func validYear(year string) bool {
	if len(year) < 4 || len(year) > 4 && year[0] == '0' {
		return false
	}
	value, _ := new(big.Int).SetString(year, 10)
	return value.Sign() != 0
}

func leapYear(year string) bool {
	value, _ := new(big.Int).SetString(year, 10)
	return divisible(value, 400) || divisible(value, 4) && !divisible(value, 100)
}

func divisible(value *big.Int, divisor int64) bool {
	return new(big.Int).Mod(value, big.NewInt(divisor)).Sign() == 0
}

func validMonthDay(monthValue string, dayValue string, leap bool) bool {
	month, _ := strconv.Atoi(monthValue)
	day, _ := strconv.Atoi(dayValue)
	if month < 1 || month > 12 || day < 1 {
		return false
	}
	days := [...]int{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if leap {
		days[2] = 29
	}
	return day <= days[month]
}

func numberIn(value string, minimum int, maximum int) bool {
	number, err := strconv.Atoi(value)
	return err == nil && number >= minimum && number <= maximum
}
