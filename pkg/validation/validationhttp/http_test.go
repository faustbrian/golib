package validationhttp_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	validationhttp "github.com/faustbrian/golib/pkg/validation/validationhttp"
)

func TestProblemAndWriterAreRouterNeutralAndEscaped(t *testing.T) {
	report := validation.NewReport(validation.DefaultLimits()).Add(
		validation.NewViolation(validation.RootPath().Append(validation.Field("<token>")),
			"required", validation.Error, nil, nil),
	)
	problem := validationhttp.FromReport(report)
	if problem.Status != http.StatusUnprocessableEntity || len(problem.Errors) != 1 ||
		problem.Errors[0].Path != "<token>" || problem.Errors[0].Severity != "error" ||
		problem.Truncated {
		t.Fatalf("problem = %#v", problem)
	}
	recorder := httptest.NewRecorder()
	if err := validationhttp.WriteProblem(recorder, problem); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != 422 || recorder.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("response = %d %#v", recorder.Code, recorder.Header())
	}
	if strings.Contains(recorder.Body.String(), "<token>") {
		t.Fatalf("JSON did not escape HTML: %s", recorder.Body.String())
	}
}

func TestWarningProblemPreservesSeverityAndTruncation(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxViolations = 1
	report := validation.NewReport(limits).
		Add(validation.NewViolation(validation.RootPath(), "warning", validation.Warning, nil, nil)).
		Add(validation.NewViolation(validation.RootPath(), "another", validation.Warning, nil, nil))
	problem := validationhttp.FromReport(report)
	if problem.Status != http.StatusOK || problem.Title != "Validation warnings" ||
		!problem.Truncated || problem.Errors[0].Severity != "warning" {
		t.Fatalf("problem = %#v", problem)
	}
}

func TestMiddlewareHookDoesNotOwnRouting(t *testing.T) {
	called := false
	hook := validationhttp.Hook[string](func(_ *http.Request, value string) validation.Report {
		called = value == "input"
		return validation.NewReport(validation.DefaultLimits())
	})
	report := hook.Validate(httptest.NewRequest(http.MethodPost, "/", nil), "input")
	if !called || !report.Empty() {
		t.Fatalf("hook called=%v report=%v", called, report)
	}
}
