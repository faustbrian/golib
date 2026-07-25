package httpx

import "strings"

// SplitDelimited splits a bounded HTTP field while respecting quoted strings.
func SplitDelimited(value string, delimiter byte, maxBytes, maxItems int) ([]string, bool) {
	if len(value) == 0 || len(value) > maxBytes || maxItems < 1 {
		return nil, false
	}
	result := make([]string, 0, min(maxItems, 8))
	start, quoted, escaped := 0, false, false
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character == '\r' || character == '\n' || character == 0 || character == 0x7f || (character < 0x20 && character != '\t') {
			return nil, false
		}
		if escaped {
			escaped = false
			continue
		}
		if quoted && character == '\\' {
			escaped = true
			continue
		}
		if character == '"' {
			quoted = !quoted
			continue
		}
		if character == delimiter && !quoted {
			item := strings.TrimSpace(value[start:index])
			if item == "" || len(result) == maxItems {
				return nil, false
			}
			result = append(result, item)
			start = index + 1
		}
	}
	if quoted || escaped {
		return nil, false
	}
	item := strings.TrimSpace(value[start:])
	if item == "" || len(result) == maxItems {
		return nil, false
	}
	return append(result, item), true
}

// ParseQuality parses the RFC 9110 qvalue grammar with at most three decimals.
func ParseQuality(value string) (float64, bool) {
	if value == "0" {
		return 0, true
	}
	if value == "1" {
		return 1, true
	}
	if len(value) < 2 || value[1] != '.' || len(value) > 5 {
		return 0, false
	}
	if value[0] != '0' && value[0] != '1' {
		return 0, false
	}
	fraction, scale := 0, 1
	for index := 2; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return 0, false
		}
		fraction = fraction*10 + int(value[index]-'0')
		scale *= 10
	}
	if value[0] == '1' && fraction != 0 {
		return 0, false
	}
	if value[0] == '1' {
		return 1, true
	}
	return float64(fraction) / float64(scale), true
}

// ValidFieldValue enforces a conservative response field-value boundary.
func ValidFieldValue(value string, maximum int) bool {
	if len(value) > maximum {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character == 0x7f || character < 0x20 {
			return false
		}
	}
	return true
}
