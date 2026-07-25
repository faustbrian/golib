package openinghoursvalidation_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
	"github.com/faustbrian/golib/pkg/opening-hours/openinghoursvalidation"
	validation "github.com/faustbrian/golib/pkg/validation"
)

func TestValidateAndError(t *testing.T) {
	schedule, _ := openinghours.NewSchedule(openinghours.Config{Timezone: "UTC"})
	if err := openinghoursvalidation.Validate(schedule); err != nil {
		t.Fatal(err)
	}
	if (&openinghoursvalidation.ValidationError{}).Error() != "openinghoursvalidation: canonical round trip mismatch" {
		t.Fatal("unexpected validation error text")
	}
}

func TestValidatorIntegratesWithGoValidation(t *testing.T) {
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	schedule, _ := openinghours.NewSchedule(openinghours.Config{Timezone: "UTC"})
	if report := openinghoursvalidation.Validator().Validate(ctx, schedule); !report.Empty() {
		t.Fatalf("valid schedule report = %s", report.String())
	}
	oversized := oversizedSchedule(t)
	report := openinghoursvalidation.Validator().Validate(ctx, oversized)
	if !report.HasErrors() || !report.HasCode(openinghoursvalidation.CodeInvalidSchedule) {
		t.Fatalf("invalid schedule report = %s", report.String())
	}
}

func TestValidateRejectsScheduleBeyondCanonicalLimit(t *testing.T) {
	schedule := oversizedSchedule(t)
	if err := openinghoursvalidation.Validate(schedule); !openinghours.IsCode(err, openinghours.CodeLimitExceeded) {
		t.Fatalf("Validate error = %v", err)
	}
}

func oversizedSchedule(t *testing.T) openinghours.Schedule {
	t.Helper()
	exceptions := make([]openinghours.Exception, 4096)
	date := openinghours.MustDate(2026, time.January, 1)
	for index := range exceptions {
		revision := fmt.Sprintf("%04d%s", index, strings.Repeat("r", 124))
		var err error
		exceptions[index], err = openinghours.NewException(openinghours.ExceptionConfig{
			Date: date, Operation: openinghours.ExceptionClose,
			Source: strings.Repeat("s", 128), Revision: revision,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC", Exceptions: exceptions,
		ConflictPolicy: openinghours.ResolveCanonical,
	})
	if err != nil {
		t.Fatal(err)
	}
	return schedule
}
