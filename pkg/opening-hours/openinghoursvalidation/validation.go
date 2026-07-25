// Package openinghoursvalidation provides a dependency-neutral validator seam
// suitable for registration with validation once that module is published.
package openinghoursvalidation

import (
	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
	validation "github.com/faustbrian/golib/pkg/validation"
)

// CodeInvalidSchedule is the stable validation violation code.
const CodeInvalidSchedule = "opening_hours.invalid_schedule"

// Validate proves that a schedule has a lossless strict canonical round trip.
func Validate(schedule openinghours.Schedule) error {
	encoded, err := schedule.CanonicalJSON()
	if err != nil {
		return err
	}
	_, err = openinghours.ParseJSON(encoded)

	return err
}

// Validator returns a deterministic validation adapter.
func Validator() validation.Validator[openinghours.Schedule] {
	return validation.ValidatorFunc[openinghours.Schedule](func(
		ctx validation.Context, schedule openinghours.Schedule,
	) validation.Report {
		report := validation.NewReport(ctx.Limits())
		if err := Validate(schedule); err != nil {
			return report.Add(validation.NewViolation(
				ctx.Path(), CodeInvalidSchedule, validation.Error, nil, err,
			))
		}
		return report
	})
}

// ValidationError reports a canonical round-trip mismatch without data.
type ValidationError struct{}

func (*ValidationError) Error() string {
	return "openinghoursvalidation: canonical round trip mismatch"
}
