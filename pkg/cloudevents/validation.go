package cloudevents

import (
	"bytes"
	"encoding/base64"
	"mime"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var reservedAttributeNames = map[string]struct{}{
	"data":            {},
	"data_base64":     {},
	"datacontenttype": {},
	"dataschema":      {},
	"id":              {},
	"source":          {},
	"specversion":     {},
	"subject":         {},
	"time":            {},
	"type":            {},
}

func validateAttributes(attributes Attributes) error {
	issues := make([]Issue, 0)
	if attributes.ID == "" {
		issues = append(issues, Issue{Field: "id", Code: IssueRequired})
	} else if !validCloudString(attributes.ID) {
		issues = append(issues, Issue{Field: "id", Code: IssueInvalidString})
	}
	if attributes.Source == "" {
		issues = append(issues, Issue{Field: "source", Code: IssueRequired})
	} else if !validCloudString(attributes.Source) || !validURIReference(attributes.Source) {
		issues = append(issues, Issue{Field: "source", Code: IssueInvalidURIReference})
	}
	if attributes.Type == "" {
		issues = append(issues, Issue{Field: "type", Code: IssueRequired})
	} else if !validCloudString(attributes.Type) {
		issues = append(issues, Issue{Field: "type", Code: IssueInvalidString})
	}
	if attributes.DataContentType != "" {
		if _, _, err := mime.ParseMediaType(attributes.DataContentType); err != nil {
			issues = append(issues, Issue{Field: "datacontenttype", Code: IssueInvalidMediaType})
		}
	}
	if attributes.DataSchema != "" && !validAbsoluteURI(attributes.DataSchema) {
		issues = append(issues, Issue{Field: "dataschema", Code: IssueAbsoluteURIRequired})
	}
	if attributes.Subject != "" && !validCloudString(attributes.Subject) {
		issues = append(issues, Issue{Field: "subject", Code: IssueInvalidString})
	}
	for name, attribute := range attributes.Extensions {
		field := "extensions." + name
		switch {
		case !validAttributeName(name):
			issues = append(issues, Issue{Field: field, Code: IssueInvalidName})
		case isReservedAttributeName(name):
			issues = append(issues, Issue{Field: field, Code: IssueReservedName})
		case !validAttribute(attribute):
			issues = append(issues, Issue{Field: field, Code: IssueInvalidAttribute})
		}
	}
	if traceParent, present := attributes.Extensions["traceparent"]; present &&
		(traceParent.kind != AttributeString || !validTraceParent(traceParent.text)) {
		issues = append(issues, Issue{Field: "extensions.traceparent", Code: IssueInvalidAttribute})
	}
	if traceState, present := attributes.Extensions["tracestate"]; present {
		if traceState.kind != AttributeString || !validTraceState(traceState.text) {
			issues = append(issues, Issue{Field: "extensions.tracestate", Code: IssueInvalidAttribute})
		}
		if _, hasParent := attributes.Extensions["traceparent"]; !hasParent {
			issues = append(issues, Issue{Field: "extensions.traceparent", Code: IssueRequired})
		}
	}
	if partitionKey, present := attributes.Extensions["partitionkey"]; present &&
		(partitionKey.kind != AttributeString || partitionKey.text == "") {
		issues = append(issues, Issue{Field: "extensions.partitionkey", Code: IssueInvalidAttribute})
	}
	if len(issues) == 0 {
		return nil
	}
	slices.SortFunc(issues, func(left, right Issue) int {
		if fieldOrder := strings.Compare(left.Field, right.Field); fieldOrder != 0 {
			return fieldOrder
		}
		return strings.Compare(string(left.Code), string(right.Code))
	})
	return &ValidationError{issues: issues}
}

func isReservedAttributeName(name string) bool {
	_, reserved := reservedAttributeNames[name]
	return reserved
}

func validAttribute(attribute Attribute) bool {
	switch attribute.kind {
	case AttributeString:
		return validCloudString(attribute.text)
	case AttributeBoolean:
		return attribute.text == "true" || attribute.text == "false"
	case AttributeInteger:
		value, err := strconv.ParseInt(attribute.text, 10, 32)
		return err == nil && strconv.FormatInt(value, 10) == attribute.text
	case AttributeBinary:
		decoded, err := base64.StdEncoding.Strict().DecodeString(attribute.text)
		return err == nil && bytes.Equal(decoded, attribute.bytes)
	case AttributeURI:
		return validCloudString(attribute.text) && validAbsoluteURI(attribute.text)
	case AttributeURIReference:
		return validCloudString(attribute.text) && validURIReference(attribute.text)
	case AttributeTimestamp:
		value, err := time.Parse(time.RFC3339Nano, attribute.text)
		return err == nil && value.UTC().Format(time.RFC3339Nano) == attribute.text
	default:
		return false
	}
}

func validAttributeName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validURIReference(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.String() == value
}

func validAbsoluteURI(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.IsAbs() && parsed.String() == value
}

func validCloudString(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character <= '\u001f' || (character >= '\u007f' && character <= '\u009f') ||
			(character >= '\ufdd0' && character <= '\ufdef') ||
			(character&0xffff == 0xfffe) || (character&0xffff == 0xffff) {
			return false
		}
	}
	return true
}
