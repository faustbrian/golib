package jsonschema

import (
	"fmt"
	"net/url"
	"strings"
)

func normalizeResourceIdentifier(identifier string) (string, error) {
	normalized, err := normalizeResourceURL(identifier)
	if err != nil {
		return "", err
	}
	return normalized.String(), nil
}

func normalizeResourceURL(identifier string) (*url.URL, error) {
	parsed, err := url.Parse(identifier)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeURL(parsed)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

func normalizeURL(source *url.URL) (*url.URL, error) {
	if source == nil {
		return nil, fmt.Errorf("nil URI")
	}
	normalized := *source
	normalized.Scheme = strings.ToLower(normalized.Scheme)

	hostname := strings.ToLower(normalized.Hostname())
	port := normalized.Port()
	if normalized.Scheme == "http" && port == "80" ||
		normalized.Scheme == "https" && port == "443" {
		port = ""
	}
	if hostname != "" {
		if strings.Contains(hostname, ":") {
			hostname = "[" + hostname + "]"
		}
		normalized.Host = hostname
		if port != "" {
			normalized.Host += ":" + port
		}
	}

	escapedPath, _ := normalizePercentEncoding(normalized.EscapedPath())
	if normalized.Opaque == "" {
		escapedPath = removeDotSegments(escapedPath)
	}
	path, _ := url.PathUnescape(escapedPath)
	normalized.Path = path
	normalized.RawPath = escapedPath
	var err error
	normalized.RawQuery, err = normalizePercentEncoding(normalized.RawQuery)
	if err != nil {
		return nil, err
	}
	normalized.Opaque, err = normalizePercentEncoding(normalized.Opaque)
	if err != nil {
		return nil, err
	}
	escapedFragment, _ := normalizePercentEncoding(normalized.EscapedFragment())
	fragment, _ := url.PathUnescape(escapedFragment)
	normalized.Fragment = fragment
	normalized.RawFragment = escapedFragment

	return &normalized, nil
}

func normalizePercentEncoding(value string) (string, error) {
	var result strings.Builder
	result.Grow(len(value))
	for index := 0; index < len(value); index++ {
		if value[index] != '%' {
			result.WriteByte(value[index])
			continue
		}
		if index+2 >= len(value) {
			return "", fmt.Errorf("invalid URI percent encoding")
		}
		high, highOK := hexadecimalValue(value[index+1])
		low, lowOK := hexadecimalValue(value[index+2])
		if !highOK || !lowOK {
			return "", fmt.Errorf("invalid URI percent encoding")
		}
		decoded := high<<4 | low
		if uriUnreserved(decoded) {
			result.WriteByte(decoded)
		} else {
			const hexadecimal = "0123456789ABCDEF"
			result.WriteByte('%')
			result.WriteByte(hexadecimal[high])
			result.WriteByte(hexadecimal[low])
		}
		index += 2
	}

	return result.String(), nil
}

func hexadecimalValue(value byte) (byte, bool) {
	switch value {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return value - '0', true
	case 'a', 'b', 'c', 'd', 'e', 'f':
		return value - 'a' + 10, true
	case 'A', 'B', 'C', 'D', 'E', 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

func uriUnreserved(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '-' || value == '.' || value == '_' || value == '~'
}

func removeDotSegments(value string) string {
	var result strings.Builder
	for value != "" {
		if replacement, terminal := map[string]string{
			"/.":  "/",
			"/..": "/",
			".":   "",
			"..":  "",
		}[value]; terminal {
			if value == "/.." {
				removeLastURISegment(&result)
			}
			value = replacement
			continue
		}
		switch {
		case strings.HasPrefix(value, "../"):
			value = value[3:]
		case strings.HasPrefix(value, "./"):
			value = value[2:]
		case strings.HasPrefix(value, "/./"):
			value = value[2:]
		case strings.HasPrefix(value, "/../"):
			value = value[3:]
			removeLastURISegment(&result)
		default:
			segment, remainder, found := strings.Cut(value[1:], "/")
			if !found {
				result.WriteString(value)
				value = ""
				continue
			}
			result.WriteString(value[:len(segment)+1])
			value = "/" + remainder
		}
	}

	return result.String()
}

func removeLastURISegment(value *strings.Builder) {
	current := value.String()
	value.Reset()
	separator := strings.LastIndexByte(current, '/')
	if separator == -1 {
		return
	}
	value.WriteString(current[:separator])
}
