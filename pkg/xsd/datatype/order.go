package datatype

import (
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// CompareOrdered compares values in an XSD 1.0 ordered primitive value space.
// comparable is false for invalid values and for indeterminate duration or
// timezone comparisons in the specification's partial orders.
func CompareOrdered(kind string, left string, right string) (comparison int, comparable bool) {
	switch kind {
	case "duration":
		return compareOrderedDurations(left, right)
	case "dateTime", "time", "date", "gYearMonth", "gYear", "gMonthDay", "gDay", "gMonth":
		return compareOrderedCalendars(kind, left, right)
	default:
		return 0, false
	}
}

// CanonicalOrderedValue returns a stable value-space key for duration and
// calendar primitives. The key is intended for equality and identity tables,
// not as an XML lexical representation.
func CanonicalOrderedValue(kind string, lexical string) (string, bool) {
	if kind == "duration" {
		value, ok := parseOrderedDuration(lexical)
		if !ok {
			return "", false
		}
		months, seconds := orderedDurationComponents(value)
		return months.String() + ":" + seconds.RatString(), true
	}
	value, ok := parseOrderedCalendar(kind, lexical)
	if !ok {
		return "", false
	}
	if value.timezone {
		return "zoned:" + value.instant().RatString(), true
	}
	return "local:" + value.local.RatString(), true
}

var orderedDurationPattern = regexp.MustCompile(
	`^(-)?P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`,
)

type orderedDuration struct {
	sign                       int
	years, months, days, hours big.Int
	minutes                    big.Int
	seconds                    big.Rat
}

func parseOrderedDuration(lexical string) (orderedDuration, bool) {
	if !validDuration(lexical) {
		return orderedDuration{}, false
	}
	match := orderedDurationPattern.FindStringSubmatch(lexical)
	value := orderedDuration{sign: 1}
	if match[1] != "" {
		value.sign = -1
	}
	values := []*big.Int{&value.years, &value.months, &value.days, &value.hours, &value.minutes}
	for index, target := range values {
		if match[index+2] != "" {
			target.SetString(match[index+2], 10)
		}
	}
	if match[7] != "" {
		value.seconds.SetString(match[7])
	}
	return value, true
}

type orderedDurationReference struct {
	year, month int64
}

func compareOrderedDurations(left string, right string) (int, bool) {
	leftValue, leftOK := parseOrderedDuration(left)
	rightValue, rightOK := parseOrderedDuration(right)
	if !leftOK || !rightOK {
		return 0, false
	}
	references := [...]orderedDurationReference{
		{year: 1696, month: 9},
		{year: 1697, month: 2},
		{year: 1903, month: 3},
		{year: 1903, month: 7},
	}
	comparison := 0
	for _, reference := range references {
		current := addOrderedDuration(reference, leftValue).Cmp(addOrderedDuration(reference, rightValue))
		if current == 0 {
			continue
		}
		if comparison != 0 && comparison != current {
			return 0, false
		}
		comparison = current
	}
	return comparison, true
}

func orderedDurationComponents(value orderedDuration) (*big.Int, *big.Rat) {
	months := new(big.Int).Mul(&value.years, big.NewInt(12))
	months.Add(months, &value.months)
	days := new(big.Int).Mul(&value.days, big.NewInt(86400))
	days.Add(days, new(big.Int).Mul(&value.hours, big.NewInt(3600)))
	days.Add(days, new(big.Int).Mul(&value.minutes, big.NewInt(60)))
	seconds := new(big.Rat).SetInt(days)
	seconds.Add(seconds, &value.seconds)
	if value.sign < 0 {
		months.Neg(months)
		seconds.Neg(seconds)
	}
	return months, seconds
}

func addOrderedDuration(reference orderedDurationReference, value orderedDuration) *big.Rat {
	monthOffset := new(big.Int).Mul(&value.years, big.NewInt(12))
	monthOffset.Add(monthOffset, &value.months)
	if value.sign < 0 {
		monthOffset.Neg(monthOffset)
	}
	monthIndex := new(big.Int).Mul(big.NewInt(reference.year), big.NewInt(12))
	monthIndex.Add(monthIndex, big.NewInt(reference.month-1))
	monthIndex.Add(monthIndex, monthOffset)
	year, monthIndex := orderedFloorDivMod(monthIndex, 12)
	month := monthIndex.Int64() + 1

	days := orderedDaysBeforeYear(year)
	days.Add(days, big.NewInt(int64(orderedDaysBeforeMonth(year, month))))
	durationDays := new(big.Int).Set(&value.days)
	if value.sign < 0 {
		durationDays.Neg(durationDays)
	}
	days.Add(days, durationDays)

	seconds := new(big.Rat).SetInt(days)
	seconds.Mul(seconds, big.NewRat(86400, 1))
	clockSeconds := new(big.Int).Mul(&value.hours, big.NewInt(3600))
	clockSeconds.Add(clockSeconds, new(big.Int).Mul(&value.minutes, big.NewInt(60)))
	clock := new(big.Rat).SetInt(clockSeconds)
	clock.Add(clock, &value.seconds)
	if value.sign < 0 {
		clock.Neg(clock)
	}
	return seconds.Add(seconds, clock)
}

func orderedFloorDivMod(value *big.Int, divisor int64) (*big.Int, *big.Int) {
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(value, big.NewInt(divisor), remainder)
	if remainder.Sign() < 0 {
		quotient.Sub(quotient, big.NewInt(1))
		remainder.Add(remainder, big.NewInt(divisor))
	}
	return quotient, remainder
}

func orderedDaysBeforeYear(year *big.Int) *big.Int {
	days := new(big.Int).Mul(year, big.NewInt(365))
	for _, term := range []struct {
		offset, divisor, sign int64
	}{
		{offset: 3, divisor: 4, sign: 1},
		{offset: 99, divisor: 100, sign: -1},
		{offset: 399, divisor: 400, sign: 1},
	} {
		adjusted := new(big.Int).Add(year, big.NewInt(term.offset))
		quotient, _ := orderedFloorDivMod(adjusted, term.divisor)
		if term.sign < 0 {
			quotient.Neg(quotient)
		}
		days.Add(days, quotient)
	}
	return days
}

func orderedDaysBeforeMonth(year *big.Int, month int64) int {
	days := [...]int{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}
	result := days[month-1]
	if month > 2 && orderedBigYearIsLeap(year) {
		result++
	}
	return result
}

func orderedBigYearIsLeap(year *big.Int) bool {
	return new(big.Int).Mod(year, big.NewInt(400)).Sign() == 0 ||
		new(big.Int).Mod(year, big.NewInt(4)).Sign() == 0 &&
			new(big.Int).Mod(year, big.NewInt(100)).Sign() != 0
}

type orderedCalendar struct {
	local          *big.Rat
	timezone       bool
	timezoneOffset int64
}

func (value orderedCalendar) instant() *big.Rat {
	instant := new(big.Rat).Set(value.local)
	if value.timezone {
		instant.Sub(instant, big.NewRat(value.timezoneOffset, 1))
	}
	return instant
}

var (
	orderedDatePattern      = regexp.MustCompile(`^(-?)([0-9]{4,})-([0-9]{2})-([0-9]{2})$`)
	orderedTimePattern      = regexp.MustCompile(`^([0-9]{2}):([0-9]{2}):([0-9]{2}(?:\.[0-9]+)?)$`)
	orderedYearMonthPattern = regexp.MustCompile(`^(-?)([0-9]{4,})-([0-9]{2})$`)
	orderedYearPattern      = regexp.MustCompile(`^(-?)([0-9]{4,})$`)
	orderedMonthDayPattern  = regexp.MustCompile(`^--([0-9]{2})-([0-9]{2})$`)
	orderedDayPattern       = regexp.MustCompile(`^---([0-9]{2})$`)
	orderedMonthPattern     = regexp.MustCompile(`^--([0-9]{2})(?:--)?$`)
)

func compareOrderedCalendars(kind string, left string, right string) (int, bool) {
	leftValue, leftOK := parseOrderedCalendar(kind, left)
	rightValue, rightOK := parseOrderedCalendar(kind, right)
	if !leftOK || !rightOK {
		return 0, false
	}
	if leftValue.timezone == rightValue.timezone {
		return leftValue.instant().Cmp(rightValue.instant()), true
	}
	uncertainty := big.NewRat(14*60*60, 1)
	if leftValue.timezone {
		leftInstant := leftValue.instant()
		if leftInstant.Cmp(new(big.Rat).Sub(rightValue.local, uncertainty)) < 0 {
			return -1, true
		}
		if leftInstant.Cmp(new(big.Rat).Add(rightValue.local, uncertainty)) > 0 {
			return 1, true
		}
		return 0, false
	}
	rightInstant := rightValue.instant()
	if new(big.Rat).Sub(leftValue.local, uncertainty).Cmp(rightInstant) > 0 {
		return 1, true
	}
	if new(big.Rat).Add(leftValue.local, uncertainty).Cmp(rightInstant) < 0 {
		return -1, true
	}
	return 0, false
}

func parseOrderedCalendar(kind string, lexical string) (orderedCalendar, bool) {
	if ValidateBuiltInLexical(kind, lexical) != nil {
		return orderedCalendar{}, false
	}
	core, timezone, offset := orderedCalendarTimezone(lexical)
	year := big.NewInt(2000)
	month, day := int64(1), int64(1)
	hour, minute := int64(0), int64(0)
	second := new(big.Rat)
	switch kind {
	case "dateTime":
		parts := strings.Split(core, "T")
		year, month, day = orderedCalendarDateParts(parts[0])
		hour, minute, second = orderedCalendarTimeParts(parts[1])
	case "date":
		year, month, day = orderedCalendarDateParts(core)
	case "time":
		hour, minute, second = orderedCalendarTimeParts(core)
	case "gYearMonth":
		match := orderedYearMonthPattern.FindStringSubmatch(core)
		year = orderedCalendarYear(match[1], match[2])
		month, _ = strconv.ParseInt(match[3], 10, 64)
	case "gYear":
		match := orderedYearPattern.FindStringSubmatch(core)
		year = orderedCalendarYear(match[1], match[2])
	case "gMonthDay":
		match := orderedMonthDayPattern.FindStringSubmatch(core)
		month, _ = strconv.ParseInt(match[1], 10, 64)
		day, _ = strconv.ParseInt(match[2], 10, 64)
	case "gDay":
		match := orderedDayPattern.FindStringSubmatch(core)
		day, _ = strconv.ParseInt(match[1], 10, 64)
	case "gMonth":
		match := orderedMonthPattern.FindStringSubmatch(core)
		month, _ = strconv.ParseInt(match[1], 10, 64)
	}
	local := orderedCalendarSeconds(year, month, day, hour, minute, second)
	return orderedCalendar{local: local, timezone: timezone, timezoneOffset: offset}, true
}

func orderedCalendarTimezone(lexical string) (core string, present bool, offset int64) {
	if strings.HasSuffix(lexical, "Z") {
		return strings.TrimSuffix(lexical, "Z"), true, 0
	}
	if len(lexical) >= 6 {
		zone := lexical[len(lexical)-6:]
		if (zone[0] == '+' || zone[0] == '-') && zone[3] == ':' {
			hours, _ := strconv.ParseInt(zone[1:3], 10, 64)
			minutes, _ := strconv.ParseInt(zone[4:6], 10, 64)
			offset = hours*3600 + minutes*60
			if zone[0] == '-' {
				offset = -offset
			}
			return lexical[:len(lexical)-6], true, offset
		}
	}
	return lexical, false, 0
}

func orderedCalendarDateParts(lexical string) (*big.Int, int64, int64) {
	match := orderedDatePattern.FindStringSubmatch(lexical)
	year := orderedCalendarYear(match[1], match[2])
	month, _ := strconv.ParseInt(match[3], 10, 64)
	day, _ := strconv.ParseInt(match[4], 10, 64)
	return year, month, day
}

func orderedCalendarYear(sign string, digits string) *big.Int {
	year, _ := new(big.Int).SetString(digits, 10)
	if sign == "-" {
		// XML Schema has no year zero: -0001 is the year before 0001.
		year.Neg(year)
		year.Add(year, big.NewInt(1))
	}
	return year
}

func orderedCalendarTimeParts(lexical string) (int64, int64, *big.Rat) {
	match := orderedTimePattern.FindStringSubmatch(lexical)
	hour, _ := strconv.ParseInt(match[1], 10, 64)
	minute, _ := strconv.ParseInt(match[2], 10, 64)
	second := new(big.Rat)
	second.SetString(match[3])
	return hour, minute, second
}

func orderedCalendarSeconds(
	year *big.Int,
	month int64,
	day int64,
	hour int64,
	minute int64,
	second *big.Rat,
) *big.Rat {
	days := orderedDaysBeforeYear(year)
	days.Add(days, big.NewInt(int64(orderedDaysBeforeMonth(year, month))))
	days.Add(days, big.NewInt(day-1))
	result := new(big.Rat).SetInt(days)
	result.Mul(result, big.NewRat(86400, 1))
	result.Add(result, big.NewRat(hour*3600+minute*60, 1))
	return result.Add(result, second)
}
