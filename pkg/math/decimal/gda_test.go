package decimal_test

import (
	"bufio"
	"context"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
)

func TestGeneralDecimalArithmeticVectors(t *testing.T) {
	t.Parallel()

	for _, vector := range []struct {
		filename          string
		executed, skipped int
	}{
		{"add.decTest", 1483, 617},
		{"subtract.decTest", 533, 148},
		{"multiply.decTest", 160, 361},
		{"divide.decTest", 357, 274},
		{"quantize0.decTest", 377, 51},
		{"rounding0.decTest", 637, 91},
	} {
		vector := vector
		t.Run(vector.filename, func(t *testing.T) {
			t.Parallel()
			runGDAVectors(
				t, filepath.Join("..", "specification", "gda", vector.filename),
				vector.executed, vector.skipped,
			)
		})
	}
}

func runGDAVectors(t *testing.T, path string, wantExecuted, wantSkipped int) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open vectors: %v", err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Errorf("close vectors: %v", err)
		}
	})

	operation := decimal.Context{
		Precision: 9, MinExponent: -999, MaxExponent: 999,
		Rounding: decimal.HalfUp,
	}
	executed := 0
	skipped := 0
	extended := true
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		if updateGDADirective(t, line, &operation, &extended) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[4] != "->" {
			skipped++
			continue
		}
		left, leftOK := parseGDAValue(fields[2])
		right, rightOK := parseGDAValue(fields[3])
		want, wantOK := parseGDAValue(fields[5])
		if !leftOK || !rightOK || !wantOK {
			skipped++
			continue
		}
		if !extended && (!fitsPrecision(left, operation.Precision) ||
			!fitsPrecision(right, operation.Precision) ||
			!fitsPrecision(want, operation.Precision) || hasGDACondition(fields[6:], "lost_digits")) {
			skipped++
			continue
		}

		var result decimal.Result
		var operationErr error
		switch fields[1] {
		case "add":
			result, operationErr = operation.Add(context.Background(), left, right)
		case "subtract":
			result, operationErr = operation.Sub(context.Background(), left, right)
		case "multiply":
			result, operationErr = operation.Mul(context.Background(), left, right)
		case "divide":
			result, operationErr = operation.Quo(context.Background(), left, right)
		case "quantize":
			result, operationErr = left.Quantize(
				context.Background(), -right.Exponent(), operation.Rounding,
				gomath.DefaultLimits(),
			)
		default:
			skipped++
			continue
		}
		if operationErr != nil {
			skipped++
			continue
		}
		wantConditions := parseGDAConditions(fields[6:])
		unsupportedConditions := gomath.ConditionClamped | gomath.ConditionSubnormal |
			gomath.ConditionUnderflow | gomath.ConditionOverflow
		if wantConditions&unsupportedConditions != 0 {
			skipped++
			continue
		}
		if !result.Value.Equal(want) || result.Conditions != wantConditions {
			t.Errorf(
				"%s: got %s exp %d [%s], want %s exp %d [%s]",
				fields[0], result.Value, result.Value.Exponent(), result.Conditions,
				want, want.Exponent(), wantConditions,
			)
		}
		executed++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan vectors: %v", err)
	}
	if executed != wantExecuted || skipped != wantSkipped {
		t.Fatalf(
			"vector accounting changed: executed %d, skipped %d; want %d, %d",
			executed, skipped, wantExecuted, wantSkipped,
		)
	}
	t.Logf("executed %d official vectors; skipped %d unsupported cases", executed, skipped)
}

func updateGDADirective(t *testing.T, line string, operation *decimal.Context, extended *bool) bool {
	t.Helper()
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(parts[0]))
	value, _, _ := strings.Cut(parts[1], "--")
	value = strings.TrimSpace(value)
	switch key {
	case "extended":
		*extended = value == "1"
	case "precision":
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			t.Fatalf("precision directive: %v", err)
		}
		operation.Precision = uint32(parsed)
	case "maxexponent":
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			t.Fatalf("maxExponent directive: %v", err)
		}
		operation.MaxExponent = int32(parsed)
	case "minexponent":
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			t.Fatalf("minExponent directive: %v", err)
		}
		operation.MinExponent = int32(parsed)
	case "rounding":
		modes := map[string]gomath.RoundingMode{
			"half_even": decimal.HalfEven,
			"half_up":   decimal.HalfUp,
			"half_down": decimal.HalfDown,
			"down":      decimal.Down,
			"up":        decimal.Up,
			"ceiling":   decimal.Ceiling,
			"floor":     decimal.Floor,
		}
		mode, ok := modes[value]
		if !ok {
			t.Fatalf("unsupported rounding directive %q", value)
		}
		operation.Rounding = mode
	}

	return true
}

func fitsPrecision(value decimal.Decimal, precision uint32) bool {
	digits := new(big.Int).Abs(value.Coefficient()).String()
	return len(digits) <= int(precision)
}

func hasGDACondition(fields []string, wanted string) bool {
	for _, field := range fields {
		if strings.EqualFold(field, wanted) {
			return true
		}
	}
	return false
}

func parseGDAValue(text string) (decimal.Decimal, bool) {
	text = strings.Trim(text, "'")
	if strings.ContainsAny(text, "#") || strings.EqualFold(text, "nan") || strings.Contains(strings.ToLower(text), "inf") {
		return decimal.Decimal{}, false
	}
	if strings.HasPrefix(text, ".") {
		text = "0" + text
	} else if strings.HasPrefix(text, "-.") {
		text = "-0" + text[1:]
	}
	text = strings.TrimSuffix(text, ".")
	value, err := decimal.ParseWithOptions(text, decimal.ParseOptions{
		AllowExponent: true, AllowPlus: true, AllowLeadingZeros: true,
		Limits: gomath.DefaultLimits(),
	})

	return value, err == nil
}

func parseGDAConditions(fields []string) gomath.Condition {
	conditions := gomath.Condition(0)
	for _, field := range fields {
		switch strings.ToLower(field) {
		case "rounded":
			conditions |= gomath.ConditionRounded
		case "inexact":
			conditions |= gomath.ConditionInexact
		case "overflow":
			conditions |= gomath.ConditionOverflow
		case "underflow":
			conditions |= gomath.ConditionUnderflow
		case "clamped":
			conditions |= gomath.ConditionClamped
		case "subnormal":
			conditions |= gomath.ConditionSubnormal
		case "division_by_zero":
			conditions |= gomath.ConditionDivisionByZero
		case "invalid_operation":
			conditions |= gomath.ConditionInvalidOperation
		}
	}

	return conditions
}
