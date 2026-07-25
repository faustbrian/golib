package tabular

import "strings"

// Row is one tabular record. Fields retain source order.
type Row []string

// NormalizationConfig describes explicit field-level data changes.
type NormalizationConfig struct {
	TrimSpace bool
	EmptyAs   string
}

// HeaderCase controls header-name case conversion.
type HeaderCase uint8

const (
	// HeaderCasePreserve retains source header casing.
	HeaderCasePreserve HeaderCase = iota
	// HeaderCaseLower converts headers to lowercase.
	HeaderCaseLower
	// HeaderCaseUpper converts headers to uppercase.
	HeaderCaseUpper
)

// HeaderConfig describes explicit header normalization and validation.
type HeaderConfig struct {
	TrimSpace        bool
	Case             HeaderCase
	Replace          map[string]string
	RejectEmpty      bool
	RejectDuplicates bool
}

// NormalizeRow returns a copy of row with configured transformations applied.
func NormalizeRow(row Row, config NormalizationConfig) Row {
	normalized := make(Row, len(row))
	for index, value := range row {
		if config.TrimSpace {
			value = strings.TrimSpace(value)
		}
		if value == "" && config.EmptyAs != "" {
			value = config.EmptyAs
		}
		normalized[index] = value
	}
	return normalized
}

// NormalizeHeader returns a normalized copy of header or a typed validation
// error. A UTF-8 BOM is removed from the first field before other changes.
func NormalizeHeader(header Row, config HeaderConfig) (Row, error) {
	normalized := make(Row, len(header))
	seen := make(map[string]struct{}, len(header))
	for index, value := range header {
		if index == 0 {
			value = strings.TrimPrefix(value, "\ufeff")
		}
		if config.TrimSpace {
			value = strings.TrimSpace(value)
		}
		switch config.Case {
		case HeaderCasePreserve:
		case HeaderCaseLower:
			value = strings.ToLower(value)
		case HeaderCaseUpper:
			value = strings.ToUpper(value)
		}
		if replacement, ok := config.Replace[value]; ok {
			value = replacement
		}
		if config.RejectEmpty && value == "" {
			return nil, &Error{Kind: ErrorInvalidHeader, Op: "header.normalize", Field: index + 1}
		}
		if config.RejectDuplicates {
			if _, ok := seen[value]; ok {
				return nil, &Error{Kind: ErrorDuplicateHeader, Op: "header.normalize", Field: index + 1}
			}
			seen[value] = struct{}{}
		}
		normalized[index] = value
	}
	return normalized, nil
}
