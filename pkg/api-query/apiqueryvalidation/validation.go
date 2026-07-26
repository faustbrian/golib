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
	path := validation.RootPath()
	for len(value) > 0 {
		fieldEnd := strings.IndexAny(value, ".[")
		if fieldEnd == -1 {
			fieldEnd = len(value)
		}
		if fieldEnd > 0 {
			path = path.Append(validation.Field(value[:fieldEnd]))
			value = value[fieldEnd:]
		}
		if strings.HasPrefix(value, ".") {
			value = value[1:]
			continue
		}
		if strings.HasPrefix(value, "[") {
			end := strings.IndexByte(value, ']')
			if end == -1 {
				return path.Append(validation.Key(value[1:]))
			}
			item := value[1:end]
			if index, err := strconv.Atoi(item); err == nil {
				path = path.Append(validation.Index(index))
			} else {
				path = path.Append(validation.Key(item))
			}
			value = value[end+1:]
			continue
		}
	}
	return path
}
