package ruleenginetemporal_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginetemporal "github.com/faustbrian/golib/pkg/rule-engine/adapters/temporal"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func FuzzPeriodTaggedValues(f *testing.F) {
	f.Add("period:2026-01-01T00:00:00Z|2026-01-02T00:00:00Z|[)", uint8(0), false, false)
	f.Add("period:2026-01-01T00:00:00.123456789+02:00|2026-01-02T00:00:00Z|[]", uint8(3), false, false)
	f.Add("period:0000-01-01T00:00:00+23:59|9999-12-31T23:59:59.999999999-23:59|(]", uint8(4), false, false)
	f.Add("period:v2:2026-01-01T00:00:00Z|2026-01-02T00:00:00Z|[)", uint8(1), false, false)
	f.Add("period:2026-01-01T00:00:00Z|2026-01-02T00:00:00Z|\u3010\u3011", uint8(2), false, false)
	f.Add("period:2026-01-01T00:00:00Z|2026-01-02T00:00:00Z|"+string([]byte{0xff, 0xfe}), uint8(2), false, false)
	f.Add("not-a-period", uint8(0), true, true)

	operators := ruleenginetemporal.Operators()[:5]
	f.Fuzz(func(t *testing.T, text string, operation uint8, wrongKind, canceled bool) {
		value := ruleengine.String(text)
		if wrongKind {
			value = ruleengine.Int(int64(len(text)))
		}
		ctx := context.Background()
		if canceled {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		operator := operators[int(operation)%len(operators)]
		matched, err := operator.Evaluate(ctx, value, value)
		if canceled {
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s canceled error = %v", operator.Name(), err)
			}
			return
		}
		if wrongKind {
			if err == nil {
				t.Fatalf("%s accepted wrong kind", operator.Name())
			}
			return
		}
		if err == nil && operator.Name() == ruleenginetemporal.OpPeriodEqual && !matched {
			t.Fatalf("Evaluate(%q, %q) = false without an error", text, text)
		}
	})
}

func FuzzInstantTaggedValues(f *testing.F) {
	f.Add("instant:2026-01-01T12:00:00Z", false, false)
	f.Add("instant:2026-01-01T12:00:00.123456789+02:30", false, false)
	f.Add("instant:0000-01-01T00:00:00+23:59", false, false)
	f.Add("instant:9999-12-31T23:59:59.999999999-23:59", false, false)
	f.Add("instant:v2:2026-01-01T12:00:00Z", false, false)
	f.Add("instant:\u2603", false, false)
	f.Add("instant:"+string([]byte{0xff, 0xfe}), false, false)
	f.Add("not-an-instant", true, true)

	start := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2100, time.January, 1, 0, 0, 0, 0, time.UTC)
	period, err := instant.New(start, end, temporal.ClosedOpen)
	if err != nil {
		f.Fatalf("create reference period: %v", err)
	}
	contains := ruleenginetemporal.Operators()[5]
	left := mustEncodedPeriod(f, period)

	f.Fuzz(func(t *testing.T, text string, wrongKind, canceled bool) {
		payload, tagged := strings.CutPrefix(text, "instant:")
		point, parseErr := time.ParseInLocation(time.RFC3339Nano, payload, time.UTC)
		right := ruleengine.String(text)
		if wrongKind {
			right = ruleengine.Int(int64(len(text)))
		}
		ctx := context.Background()
		if canceled {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		matched, evaluateErr := contains.Evaluate(
			ctx,
			left,
			right,
		)
		if canceled {
			if !errors.Is(evaluateErr, context.Canceled) {
				t.Fatalf("canceled error = %v", evaluateErr)
			}
			return
		}
		if wrongKind {
			if evaluateErr == nil {
				t.Fatal("accepted wrong instant kind")
			}
			return
		}
		if evaluateErr != nil {
			return
		}
		if !tagged || parseErr != nil {
			t.Fatalf("Evaluate(%q) accepted an invalid instant", text)
		}
		if matched != period.Includes(point) {
			t.Fatalf(
				"Evaluate(%q) = %t, want %t",
				text,
				matched,
				period.Includes(point),
			)
		}
	})
}
