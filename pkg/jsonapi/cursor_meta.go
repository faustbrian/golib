package jsonapi

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
)

// CursorPageMeta describes metadata adjacent to paginated data.
type CursorPageMeta struct {
	RangeTruncated *bool
	Total          *int64
	EstimatedTotal *CursorEstimatedTotal
}

// CursorEstimatedTotal describes an optional estimate of collection size.
type CursorEstimatedTotal struct {
	BestGuess *int64
}

type cursorPageMetaWire struct {
	RangeTruncated *bool               `json:"rangeTruncated,omitempty"`
	Total          *int64              `json:"total,omitempty"`
	EstimatedTotal *cursorEstimateWire `json:"estimatedTotal,omitempty"`
}

type cursorEstimateWire struct {
	BestGuess *int64 `json:"bestGuess,omitempty"`
}

// Meta validates and wraps pagination metadata in the profile's page member.
func (metadata CursorPageMeta) Meta() (Meta, error) {
	return metadata.MetaAs("")
}

// MetaAs validates and wraps pagination metadata using a profile element
// alias. An empty member uses the profile's default page name.
func (metadata CursorPageMeta) MetaAs(member string) (Meta, error) {
	member, err := cursorPageMember(member)
	if err != nil {
		return nil, err
	}
	pagePath := "/meta/" + escapePointerToken(member)
	validator := documentValidator{}
	if metadata.Total != nil && *metadata.Total < 0 {
		validator.add(pagePath+"/total", "value", "total must not be negative")
	}
	if metadata.EstimatedTotal != nil && metadata.EstimatedTotal.BestGuess != nil &&
		*metadata.EstimatedTotal.BestGuess < 0 {
		validator.add(
			pagePath+"/estimatedTotal/bestGuess",
			"value",
			"bestGuess must not be negative",
		)
	}
	if len(validator.violations) > 0 {
		return nil, &ValidationError{Violations: validator.violations}
	}

	var estimate *cursorEstimateWire
	if metadata.EstimatedTotal != nil {
		estimate = &cursorEstimateWire{BestGuess: metadata.EstimatedTotal.BestGuess}
	}
	return Meta{member: cursorPageMetaWire{
		RangeTruncated: metadata.RangeTruncated,
		Total:          metadata.Total,
		EstimatedTotal: estimate,
	}}, nil
}

// ParseCursorPageMeta validates and extracts pagination metadata. The boolean
// reports whether the page member was present.
func ParseCursorPageMeta(meta Meta) (CursorPageMeta, bool, error) {
	return ParseCursorPageMetaAs(meta, "")
}

// ParseCursorPageMetaAs validates and extracts pagination metadata using a
// profile element alias. An empty member uses the default page name.
func ParseCursorPageMetaAs(meta Meta, member string) (CursorPageMeta, bool, error) {
	member, err := cursorPageMember(member)
	if err != nil {
		return CursorPageMeta{}, false, err
	}
	pagePath := "/meta/" + escapePointerToken(member)
	value, present := meta[member]
	if !present {
		return CursorPageMeta{}, false, nil
	}
	object, ok := cursorMetaObject(value)
	if !ok {
		return CursorPageMeta{}, true, cursorMetaError(
			pagePath, "type", member+" metadata must be an object",
		)
	}

	var metadata CursorPageMeta
	if raw, exists := object["rangeTruncated"]; exists {
		value, valid := raw.(bool)
		if !valid {
			return CursorPageMeta{}, true, cursorMetaError(
				pagePath+"/rangeTruncated", "type", "rangeTruncated must be a boolean",
			)
		}
		metadata.RangeTruncated = &value
	}
	if raw, exists := object["total"]; exists {
		value, valid := cursorInteger(raw)
		if !valid || value < 0 {
			return CursorPageMeta{}, true, cursorMetaError(
				pagePath+"/total", "type", "total must be a non-negative integer",
			)
		}
		metadata.Total = &value
	}
	if raw, exists := object["estimatedTotal"]; exists {
		estimateObject, valid := cursorMetaObject(raw)
		if !valid {
			return CursorPageMeta{}, true, cursorMetaError(
				pagePath+"/estimatedTotal", "type", "estimatedTotal must be an object",
			)
		}
		estimate := &CursorEstimatedTotal{}
		if bestGuessRaw, exists := estimateObject["bestGuess"]; exists {
			bestGuess, integer := cursorInteger(bestGuessRaw)
			if !integer || bestGuess < 0 {
				return CursorPageMeta{}, true, cursorMetaError(
					pagePath+"/estimatedTotal/bestGuess",
					"type",
					"bestGuess must be a non-negative integer",
				)
			}
			estimate.BestGuess = &bestGuess
		}
		metadata.EstimatedTotal = estimate
	}

	return metadata, true, nil
}

// CursorItemMeta wraps an opaque item cursor in profile metadata.
func CursorItemMeta(cursor string) Meta {
	return Meta{"page": map[string]any{"cursor": cursor}}
}

// CursorItemMetaAs wraps an item cursor using a profile element alias. An
// empty member uses the default page name.
func CursorItemMetaAs(member, cursor string) (Meta, error) {
	member, err := cursorPageMember(member)
	if err != nil {
		return nil, err
	}
	return Meta{member: map[string]any{"cursor": cursor}}, nil
}

// ParseCursorItemMeta validates and extracts an item cursor. The boolean
// reports whether the cursor member was present.
func ParseCursorItemMeta(meta Meta) (string, bool, error) {
	return ParseCursorItemMetaAs(meta, "")
}

// ParseCursorItemMetaAs validates and extracts an item cursor using a profile
// element alias. An empty member uses the default page name.
func ParseCursorItemMetaAs(meta Meta, member string) (string, bool, error) {
	member, err := cursorPageMember(member)
	if err != nil {
		return "", false, err
	}
	pagePath := "/meta/" + escapePointerToken(member)
	page, present := meta[member]
	if !present {
		return "", false, nil
	}
	object, ok := cursorMetaObject(page)
	if !ok {
		return "", false, cursorMetaError(
			pagePath, "type", member+" metadata must be an object",
		)
	}
	value, present := object["cursor"]
	if !present {
		return "", false, nil
	}
	cursor, ok := value.(string)
	if !ok {
		return "", true, cursorMetaError(
			pagePath+"/cursor", "type", "cursor must be a string",
		)
	}

	return cursor, true, nil
}

func cursorPageMember(member string) (string, error) {
	if member == "" {
		return "page", nil
	}
	if !validImplementationMemberName(member) {
		return "", fmt.Errorf("cursor pagination page member must be a valid member name")
	}
	return member, nil
}

func cursorMetaObject(value any) (map[string]any, bool) {
	switch object := value.(type) {
	case map[string]any:
		return object, true
	case Meta:
		return map[string]any(object), true
	case cursorPageMetaWire:
		result := make(map[string]any, 3)
		if object.RangeTruncated != nil {
			result["rangeTruncated"] = *object.RangeTruncated
		}
		if object.Total != nil {
			result["total"] = *object.Total
		}
		if object.EstimatedTotal != nil {
			estimate := make(map[string]any, 1)
			if object.EstimatedTotal.BestGuess != nil {
				estimate["bestGuess"] = *object.EstimatedTotal.BestGuess
			}
			result["estimatedTotal"] = estimate
		}
		return result, true
	default:
		return nil, false
	}
}

func cursorInteger(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int8:
		return int64(number), true
	case int16:
		return int64(number), true
	case int32:
		return int64(number), true
	case int64:
		return number, true
	case uint:
		if uint64(number) <= math.MaxInt64 {
			return int64(number), true
		}
	case uint8:
		return int64(number), true
	case uint16:
		return int64(number), true
	case uint32:
		return int64(number), true
	case uint64:
		if number <= math.MaxInt64 {
			return int64(number), true
		}
	case float64:
		if number >= math.MinInt64 && number <= math.MaxInt64 && number == math.Trunc(number) {
			return int64(number), true
		}
	case json.Number:
		raw := string(number)
		if !json.Valid([]byte(raw)) {
			break
		}
		rational, ok := new(big.Rat).SetString(raw)
		if ok && rational.IsInt() && rational.Num().IsInt64() {
			return rational.Num().Int64(), true
		}
	}

	return 0, false
}

func cursorMetaError(path, code, message string) error {
	return &ValidationError{Violations: []Violation{{
		Path: path, Code: code, Message: message,
	}}}
}
