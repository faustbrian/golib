package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// EverySecond returns an interval aligned to every wall-clock second.
func EverySecond() Interval { return Cron("* * * * * *") }

// EveryTwoSeconds returns an interval aligned to every second divisible by two.
func EveryTwoSeconds() Interval { return everySeconds(2) }

// EveryFiveSeconds returns an interval aligned to every second divisible by five.
func EveryFiveSeconds() Interval { return everySeconds(5) }

// EveryTenSeconds returns an interval aligned to every second divisible by ten.
func EveryTenSeconds() Interval { return everySeconds(10) }

// EveryFifteenSeconds returns an interval aligned to every second divisible by fifteen.
func EveryFifteenSeconds() Interval { return everySeconds(15) }

// EveryTwentySeconds returns an interval aligned to every second divisible by twenty.
func EveryTwentySeconds() Interval { return everySeconds(20) }

// EveryThirtySeconds returns an interval aligned to every second divisible by thirty.
func EveryThirtySeconds() Interval { return everySeconds(30) }

// EveryTwoMinutes returns an interval aligned to every minute divisible by two.
func EveryTwoMinutes() Interval { return everyMinutes(2) }

// EveryThreeMinutes returns an interval aligned to every minute divisible by three.
func EveryThreeMinutes() Interval { return everyMinutes(3) }

// EveryFourMinutes returns an interval aligned to every minute divisible by four.
func EveryFourMinutes() Interval { return everyMinutes(4) }

// EveryFiveMinutes returns an interval aligned to every minute divisible by five.
func EveryFiveMinutes() Interval { return everyMinutes(5) }

// EveryTenMinutes returns an interval aligned to every minute divisible by ten.
func EveryTenMinutes() Interval { return everyMinutes(10) }

// EveryFifteenMinutes returns an interval aligned to every minute divisible by fifteen.
func EveryFifteenMinutes() Interval { return everyMinutes(15) }

// EveryThirtyMinutes returns an interval aligned to every minute divisible by thirty.
func EveryThirtyMinutes() Interval { return everyMinutes(30) }

// HourlyAt returns an hourly interval at minute past each hour.
func HourlyAt(minute int) Interval { return Cron(fmt.Sprintf("%d * * * *", minute)) }

// EveryOddHour returns an odd-hour interval at minute zero or the optional minute.
func EveryOddHour(minutes ...int) Interval { return everyHours("1-23/2", minutes) }

// EveryTwoHours returns a two-hour interval at minute zero or the optional minute.
func EveryTwoHours(minutes ...int) Interval { return everyHours("*/2", minutes) }

// EveryThreeHours returns a three-hour interval at minute zero or the optional minute.
func EveryThreeHours(minutes ...int) Interval { return everyHours("*/3", minutes) }

// EveryFourHours returns a four-hour interval at minute zero or the optional minute.
func EveryFourHours(minutes ...int) Interval { return everyHours("*/4", minutes) }

// EverySixHours returns a six-hour interval at minute zero or the optional minute.
func EverySixHours(minutes ...int) Interval { return everyHours("*/6", minutes) }

// DailyAt returns a daily interval at the local HH:MM time.
func DailyAt(at string) Interval { return intervalAt("* * *", at) }

// At is an alias for DailyAt.
func At(at string) Interval { return DailyAt(at) }

// TwiceDaily returns a daily interval at both hours on minute zero.
func TwiceDaily(firstHour, secondHour int) Interval {
	return TwiceDailyAt(firstHour, secondHour, 0)
}

// TwiceDailyAt returns a daily interval at both hours on minute.
func TwiceDailyAt(firstHour, secondHour, minute int) Interval {
	return Cron(fmt.Sprintf("%d %d,%d * * *", minute, firstHour, secondHour))
}

// DaysOfMonth returns a midnight interval on each requested day of the month.
func DaysOfMonth(days ...int) Interval {
	return Cron(fmt.Sprintf("0 0 %s * *", joinNumbers(days)))
}

// WeeklyOn returns a weekly interval on day at the local HH:MM time.
func WeeklyOn(day time.Weekday, at string) Interval {
	return intervalAt(fmt.Sprintf("* * %d", day), at)
}

// MonthlyOn returns a monthly interval on day at the local HH:MM time.
func MonthlyOn(day int, at string) Interval {
	return intervalAt(fmt.Sprintf("%d * *", day), at)
}

// TwiceMonthly returns a monthly interval on both days at the local HH:MM time.
func TwiceMonthly(firstDay, secondDay int, at string) Interval {
	return intervalAt(fmt.Sprintf("%d,%d * *", firstDay, secondDay), at)
}

// LastDayOfMonth returns a monthly interval on the last day at the local HH:MM time.
func LastDayOfMonth(at string) Interval { return intervalAt("L * *", at) }

// Quarterly returns a midnight interval on the first day of each quarter.
func Quarterly() Interval { return Cron("0 0 1 1,4,7,10 *") }

// QuarterlyOn returns an interval on day of each quarter at the local HH:MM time.
func QuarterlyOn(day int, at string) Interval {
	return intervalAt(fmt.Sprintf("%d 1,4,7,10 *", day), at)
}

// Yearly returns a midnight interval on the first day of each year.
func Yearly() Interval { return Cron("0 0 1 1 *") }

// YearlyOn returns an annual interval on month and day at the local HH:MM time.
func YearlyOn(month time.Month, day int, at string) Interval {
	return intervalAt(fmt.Sprintf("%d %d *", day, month), at)
}

func everySeconds(seconds int) Interval {
	return Cron(fmt.Sprintf("*/%d * * * * *", seconds))
}

func everyMinutes(minutes int) Interval {
	return Cron(fmt.Sprintf("*/%d * * * *", minutes))
}

func everyHours(hours string, minutes []int) Interval {
	minute := 0
	switch len(minutes) {
	case 0:
	case 1:
		minute = minutes[0]
	default:
		minute = 60
	}
	return Cron(fmt.Sprintf("%d %s * * *", minute, hours))
}

func intervalAt(remainingFields, at string) Interval {
	hour, minute, ok := parseTimeOfDay(at)
	if !ok {
		return Cron("invalid")
	}
	return Cron(fmt.Sprintf("%d %d %s", minute, hour, remainingFields))
}

func parseTimeOfDay(value string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

func joinNumbers(values []int) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = strconv.Itoa(value)
	}
	return strings.Join(parts, ",")
}
