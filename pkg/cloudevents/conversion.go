package cloudevents

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrConversionLoss identifies an encoding that would change declared event
// data or metadata without an explicit loss report.
var ErrConversionLoss = errors.New("cloudevents: conversion loss")

// ConversionLoss identifies one declared field whose representation changes
// in a target event format or protocol binding. It never contains field data.
type ConversionLoss struct {
	Field  string
	Reason string
}

// ConversionReport makes every representation change explicit. Losses are
// sorted by field and reason so callers can compare and persist reports.
type ConversionReport struct {
	Losses []ConversionLoss
}

func jsonConversionReport(event Event) ConversionReport {
	report := ConversionReport{}
	if event.data.present && event.data.kind == DataJSON &&
		!bytes.Equal(event.data.bytes, bytes.TrimSpace(event.data.bytes)) {
		report.Losses = append(report.Losses, ConversionLoss{
			Field: "data", Reason: "leading or trailing JSON whitespace is not representable inside a structured event",
		})
	}
	if event.data.present {
		switch {
		case event.data.kind == DataText && event.dataContentType == "":
			report.Losses = append(report.Losses, ConversionLoss{
				Field: "datacontenttype", Reason: "text/plain is materialized to preserve text data semantics",
			})
		case event.data.kind == DataText && isJSONMediaType(event.dataContentType):
			report.Losses = append(report.Losses, ConversionLoss{
				Field: "data", Reason: "JSON content type decodes text data as JSON",
			})
		case event.data.kind == DataJSON && !isJSONMediaType(event.dataContentType):
			report.Losses = append(report.Losses, ConversionLoss{
				Field: "data", Reason: "non-JSON content type cannot preserve JSON data semantics",
			})
		}
	}
	for name, attribute := range event.extensions {
		switch attribute.kind {
		case AttributeString, AttributeBoolean, AttributeInteger:
		default:
			report.Losses = append(report.Losses, ConversionLoss{
				Field: "extensions." + name, Reason: "abstract extension type normalizes to a JSON string",
			})
		}
	}
	return canonicalConversionReport(report)
}

func binaryConversionReport(event Event) ConversionReport {
	report := ConversionReport{}
	if event.data.present && event.dataContentType == "" {
		report.Losses = append(report.Losses, ConversionLoss{
			Field: "datacontenttype", Reason: implicitDataContentType(event.data.kind) + " is materialized to preserve data semantics",
		})
	} else if event.data.present && !dataKindMatchesContentType(event.data.kind, event.dataContentType) {
		report.Losses = append(report.Losses, ConversionLoss{
			Field: "data", Reason: "declared content type changes the data kind in the binary binding",
		})
	}
	for name, attribute := range event.extensions {
		if attribute.kind != AttributeString {
			report.Losses = append(report.Losses, ConversionLoss{
				Field: "extensions." + name, Reason: "abstract extension type normalizes to a protocol string",
			})
		}
	}
	return canonicalConversionReport(report)
}

func httpBinaryConversionReport(event Event) ConversionReport {
	return binaryConversionReport(event)
}

func implicitDataContentType(kind DataKind) string {
	switch kind {
	case DataJSON:
		return "application/json"
	case DataText:
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

func dataKindMatchesContentType(kind DataKind, contentType string) bool {
	switch kind {
	case DataJSON:
		return isJSONMediaType(contentType)
	case DataText:
		return isTextMediaType(contentType)
	default:
		if isJSONMediaType(contentType) {
			return false
		}
		return !isTextMediaType(contentType)
	}
}

func prefixConversionReport(report ConversionReport, prefix string) ConversionReport {
	for index := range report.Losses {
		report.Losses[index].Field = prefix + report.Losses[index].Field
	}
	return canonicalConversionReport(report)
}

func canonicalConversionReport(report ConversionReport) ConversionReport {
	slices.SortFunc(report.Losses, func(left, right ConversionLoss) int {
		if compared := strings.Compare(left.Field, right.Field); compared != 0 {
			return compared
		}
		return strings.Compare(left.Reason, right.Reason)
	})
	return report
}

func rejectConversionLoss(report ConversionReport) error {
	if len(report.Losses) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrConversionLoss, report.Losses[0].Field)
}
