package xsd

import "strings"

// NormalizeIdentityXPath removes XML whitespace permitted around tokens in
// the XML Schema 1.0 identity-constraint XPath subset. Whitespace that would
// separate two name tokens is retained so a later grammar check rejects it.
func NormalizeIdentityXPath(expression string) string {
	var normalized strings.Builder
	for index := 0; index < len(expression); {
		if !identityXPathSpace(expression[index]) {
			normalized.WriteByte(expression[index])
			index++
			continue
		}
		start := index
		for index < len(expression) && identityXPathSpace(expression[index]) {
			index++
		}
		previous := previousIdentityXPathByte(expression, start)
		next := nextIdentityXPathByte(expression, index)
		if previous != 0 && next != 0 && !identityXPathPunctuation(previous) &&
			!identityXPathPunctuation(next) {
			normalized.WriteByte(' ')
		}
	}
	return normalized.String()
}

func identityXPathSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\n' || character == '\r'
}

func identityXPathPunctuation(character byte) bool {
	return character == '.' || character == '/' || character == '|' || character == '@'
}

func previousIdentityXPathByte(expression string, index int) byte {
	for index > 0 {
		index--
		if !identityXPathSpace(expression[index]) {
			return expression[index]
		}
	}
	return 0
}

func nextIdentityXPathByte(expression string, index int) byte {
	if index < len(expression) {
		return expression[index]
	}
	return 0
}
