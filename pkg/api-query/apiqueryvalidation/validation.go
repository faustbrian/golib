// Package apiqueryvalidation projects query failures into validation
// reports without exposing rejected values or unsafe causes.
package apiqueryvalidation

import (
	"errors"
	"strconv"
	"strings"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
	validation "github.com/faustbrian/golib/pkg/validation"
)

// Report converts structured query violations into an immutable validation
// report. Non-query errors become one sanitized root query_error violation.
func Report(err error, limits validation.Limits) validation.Report {
	report := validation.NewReport(limits)
	if err == nil {
		return report
	}
	var queryViolations *apiquery.Violations
	if !errors.As(err, &queryViolations) {
		return report.Add(validation.NewViolation(validation.RootPath(),
			"query_error", validation.Error, nil, nil))
	}
	for _, violation := range queryViolations.Items() {
		report = report.Add(validation.NewViolation(parsePath(violation.Path),
			string(violation.Code), validation.Error, nil, nil))
	}
	return report
}

func parsePath(value string) validation.Path {
	return appendPath(validation.RootPath(), value)
}

func appendPath(path validation.Path, value string) validation.Path {
	if value == "" {
		return path
	}
	fieldEnd := strings.IndexAny(value, ".[")
	if fieldEnd == -1 {
		return path.Append(validation.Field(value))
	}
	if fieldEnd > 0 {
		path = path.Append(validation.Field(value[:fieldEnd]))
		value = value[fieldEnd:]
	}
	if remainder, found := strings.CutPrefix(value, "."); found {
		return appendPath(path, remainder)
	}
	remainder := strings.TrimPrefix(value, "[")
	item, rest, closed := strings.Cut(remainder, "]")
	if index, err := strconv.Atoi(item); err == nil {
		path = path.Append(validation.Index(index))
	} else {
		path = path.Append(validation.Key(item))
	}
	if !closed {
		return path
	}
	return appendPath(path, rest)
}
