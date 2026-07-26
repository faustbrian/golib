package decode_test

import (
	"context"
	"errors"
	"math"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/config/decode"
)

type panicText string

func (p *panicText) UnmarshalText([]byte) error {
	panic("canary-secret-value")
}

type panicValue struct{}

func (*panicValue) UnmarshalConfigValue(any) error {
	panic("canary-secret-value")
}

type contextHookKey struct{}

type contextValueHook struct{ Value string }

func (hook *contextValueHook) UnmarshalConfigValueContext(ctx context.Context, input any) error {
	if input == "panic" {
		panic("canary-secret-value")
	}
	if cancel, ok := ctx.Value(contextHookKey{}).(context.CancelFunc); ok {
		cancel()
		return nil
	}
	hook.Value, _ = input.(string)
	return ctx.Err()
}

type contextTextHook string

func (hook *contextTextHook) UnmarshalTextContext(ctx context.Context, input []byte) error {
	if string(input) == "panic" {
		panic("canary-secret-value")
	}
	*hook = contextTextHook(input)
	return ctx.Err()
}

type failingValue struct{}

var errValueRejected = errors.New("canary-secret-value")

func (*failingValue) UnmarshalConfigValue(any) error { return errValueRejected }

type successfulValue struct{ Called bool }

func (v *successfulValue) UnmarshalConfigValue(any) error {
	v.Called = true
	return nil
}

func TestIntoRecoversAndRedactsTextUnmarshalerPanic(t *testing.T) {
	t.Parallel()

	type settings struct {
		Token panicText `config:"token"`
	}
	var destination settings
	err := decode.Into(map[string]any{"token": "sensitive"}, &destination)
	var panicErr *decode.PanicError
	if !errors.As(err, &panicErr) {
		t.Fatalf("expected PanicError, got %T: %v", err, err)
	}
	if strings.Contains(err.Error(), "canary-secret-value") || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("error leaked secret: %q", err)
	}
}

func TestIntoAggregatesIndependentFieldErrorsDeterministically(t *testing.T) {
	t.Parallel()

	type settings struct {
		Count int    `config:"count"`
		Name  string `config:"name,required"`
	}

	destination := settings{Count: 7, Name: "unchanged"}
	err := decode.Into(map[string]any{
		"z_unknown": true,
		"count":     "not-an-integer",
		"a_unknown": true,
	}, &destination)

	var decodeErrors *decode.Errors
	if !errors.As(err, &decodeErrors) {
		t.Fatalf("expected Errors, got %T: %v", err, err)
	}
	paths := make([]string, 0, len(decodeErrors.Fields))
	for _, fieldErr := range decodeErrors.Fields {
		paths = append(paths, fieldErr.Path)
	}
	want := []string{"a_unknown", "count", "name", "z_unknown"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("error paths = %v, want %v", paths, want)
	}
	if destination != (settings{Count: 7, Name: "unchanged"}) {
		t.Fatalf("destination was partially assigned: %#v", destination)
	}
}

type mode string

func (m *mode) UnmarshalText(text []byte) error {
	switch string(text) {
	case "safe", "fast":
		*m = mode(text)
		return nil
	default:
		return errors.New("unsupported mode")
	}
}

type configuration struct {
	Name     string         `config:"name"`
	Port     int            `config:"port"`
	Timeout  time.Duration  `config:"timeout"`
	Endpoint url.URL        `config:"endpoint"`
	Mode     mode           `config:"mode"`
	Tags     []string       `config:"tags"`
	Limits   map[string]int `config:"limits"`
	Database struct {
		Host string `config:"host"`
	} `config:"database"`
}

func TestIntoDecodesTypedStruct(t *testing.T) {
	t.Parallel()

	tree := map[string]any{
		"name":     "worker",
		"port":     int64(8080),
		"timeout":  "1.5s",
		"endpoint": "https://example.com/api",
		"mode":     "safe",
		"tags":     []any{"one", "two"},
		"limits":   map[string]any{"jobs": int64(3)},
		"database": map[string]any{"host": "db.internal"},
	}

	var got configuration
	if err := decode.Into(tree, &got); err != nil {
		t.Fatalf("Into() error = %v", err)
	}

	if got.Name != "worker" || got.Port != 8080 || got.Timeout != 1500*time.Millisecond {
		t.Fatalf("Into() scalar result = %#v", got)
	}
	if got.Endpoint.String() != "https://example.com/api" || got.Mode != "safe" {
		t.Fatalf("Into() hook result = %#v", got)
	}
	if len(got.Tags) != 2 || got.Limits["jobs"] != 3 || got.Database.Host != "db.internal" {
		t.Fatalf("Into() collection result = %#v", got)
	}
}

func TestIntoRejectsUnknownFieldWithoutMutatingDestination(t *testing.T) {
	t.Parallel()

	destination := configuration{Name: "unchanged"}
	err := decode.Into(map[string]any{"unknown": "value"}, &destination)
	if err == nil {
		t.Fatal("Into() error = nil, want unknown field error")
	}
	if destination.Name != "unchanged" {
		t.Fatalf("Into() mutated destination to %#v", destination)
	}

	var fieldError *decode.FieldError
	if !errors.As(err, &fieldError) {
		t.Fatalf("Into() error type = %T, want *decode.FieldError", err)
	}
	if fieldError.Path != "unknown" || fieldError.Expected != "known field" {
		t.Fatalf("FieldError = %#v", fieldError)
	}
}

func TestIntoReportsNestedTypeErrorSafely(t *testing.T) {
	t.Parallel()

	var destination configuration
	err := decode.Into(
		map[string]any{"database": map[string]any{"host": map[string]any{"secret": "do-not-print"}}},
		&destination,
	)
	if err == nil {
		t.Fatal("Into() error = nil, want type error")
	}
	if got := err.Error(); got == "" || contains(got, "do-not-print") {
		t.Fatalf("Into() error leaked received value: %q", got)
	}

	var fieldError *decode.FieldError
	if !errors.As(err, &fieldError) {
		t.Fatalf("Into() error type = %T, want *decode.FieldError", err)
	}
	if fieldError.Path != "database.host" || fieldError.Received != "object" {
		t.Fatalf("FieldError = %#v", fieldError)
	}
}

func TestIntoRejectsInvalidDestination(t *testing.T) {
	t.Parallel()

	var notPointer configuration
	if err := decode.Into(map[string]any{}, notPointer); err == nil {
		t.Fatal("Into() error = nil for non-pointer destination")
	}

	var nilPointer *configuration
	if err := decode.Into(map[string]any{}, nilPointer); err == nil {
		t.Fatal("Into() error = nil for nil destination")
	}
}

func TestValueDecodesPointersNullsAndInterfacesAtomically(t *testing.T) {
	t.Parallel()

	var pointer *int
	if err := decode.Value(int64(42), &pointer); err != nil || pointer == nil || *pointer != 42 {
		t.Fatalf("Value(pointer) = %v, %v", pointer, err)
	}
	if err := decode.Value(nil, &pointer); err != nil || pointer != nil {
		t.Fatalf("Value(nil pointer) = %v, %v", pointer, err)
	}

	input := map[string]any{"items": []any{map[string]any{"value": "original"}}}
	var untyped any
	if err := decode.Value(input, &untyped); err != nil {
		t.Fatalf("Value(interface) error = %v", err)
	}
	input["items"].([]any)[0].(map[string]any)["value"] = "input mutated"
	got := untyped.(map[string]any)
	got["items"].([]any)[0].(map[string]any)["value"] = "output mutated"
	if input["items"].([]any)[0].(map[string]any)["value"] != "input mutated" {
		t.Fatal("Value(interface) aliased input")
	}
	if err := decode.Value(nil, &untyped); err != nil || untyped != nil {
		t.Fatalf("Value(nil interface) = %#v, %v", untyped, err)
	}
}

func TestValueDecodesNumericKindsAndRejectsBoundaries(t *testing.T) {
	t.Parallel()

	type numbers struct {
		Int8    int8    `config:"int8"`
		Uint8   uint8   `config:"uint8"`
		Uint64  uint64  `config:"uint64"`
		Float32 float32 `config:"float32"`
		Float64 float64 `config:"float64"`
	}
	var got numbers
	if err := decode.Into(map[string]any{
		"int8": int8(-8), "uint8": uint16(8), "uint64": ^uint64(0),
		"float32": float32(1.5), "float64": int32(2),
	}, &got); err != nil {
		t.Fatalf("Into() error = %v", err)
	}
	if got.Int8 != -8 || got.Uint8 != 8 || got.Uint64 != ^uint64(0) ||
		got.Float32 != 1.5 || got.Float64 != 2 {
		t.Fatalf("numbers = %#v", got)
	}

	tests := map[string]struct {
		input       any
		destination any
	}{
		"signed overflow":   {input: ^uint64(0), destination: new(int64)},
		"narrow overflow":   {input: int64(128), destination: new(int8)},
		"negative unsigned": {input: int64(-1), destination: new(uint)},
		"float mismatch":    {input: "1.5", destination: new(float64)},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := decode.Value(test.input, test.destination); err == nil {
				t.Fatal("Value() error = nil")
			}
		})
	}
}

func TestIntoReportsCollectionFailuresByElementPath(t *testing.T) {
	t.Parallel()

	type settings struct {
		Items  []int          `config:"items"`
		Labels map[string]int `config:"labels"`
	}
	var destination settings
	err := decode.Into(map[string]any{
		"items":  []any{"bad", int64(2), false},
		"labels": map[string]any{"z": "bad", "a": false},
	}, &destination)
	var failures *decode.Errors
	if !errors.As(err, &failures) {
		t.Fatalf("expected Errors, got %T: %v", err, err)
	}
	paths := make([]string, len(failures.Fields))
	for index, failure := range failures.Fields {
		paths[index] = failure.Path
	}
	want := []string{"items[0]", "items[2]", "labels.a", "labels.z"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
	if destination.Items != nil || destination.Labels != nil {
		t.Fatalf("destination was partially assigned: %#v", destination)
	}
}

func TestIntoRejectsAmbiguousIgnoredAndUnsupportedFields(t *testing.T) {
	t.Parallel()

	type ambiguous struct {
		First  string `config:"same"`
		Second string `config:"same"`
	}
	if err := decode.Into(map[string]any{}, &ambiguous{}); err == nil {
		t.Fatal("Into(ambiguous) error = nil")
	}

	type caseFoldCollision struct {
		Value string
		VALUE string
	}
	if err := decode.Into(map[string]any{}, &caseFoldCollision{}); err == nil {
		t.Fatal("Into(case-fold collision) error = nil")
	}

	type EmbeddedOne struct {
		Value string `config:"value"`
	}
	type EmbeddedTwo struct {
		Value string `config:"value"`
	}
	type ambiguousEmbedded struct {
		EmbeddedOne
		EmbeddedTwo
	}
	var embedded ambiguousEmbedded
	if err := decode.Into(map[string]any{"value": "canary"}, &embedded); err == nil {
		t.Fatal("Into(ambiguous embedded fields) error = nil")
	}
	if embedded.EmbeddedOne.Value != "" || embedded.EmbeddedTwo.Value != "" {
		t.Fatalf("Into() assigned an ambiguous embedded field: %#v", embedded)
	}

	type ignored struct {
		Ignored    string `config:"-"`
		unexported string
	}
	ignoredDestination := ignored{unexported: "preserved"}
	if err := decode.Into(map[string]any{}, &ignoredDestination); err != nil {
		t.Fatalf("Into(ignored) error = %v", err)
	}
	if ignoredDestination.unexported != "" {
		t.Fatalf("Into(ignored) unexported = %q, want empty", ignoredDestination.unexported)
	}

	var unsupported chan int
	if err := decode.Value("value", &unsupported); err == nil {
		t.Fatal("Value(chan) error = nil")
	}
	var invalidMap map[int]string
	if err := decode.Value(map[string]any{"one": "value"}, &invalidMap); err == nil {
		t.Fatal("Value(map[int]string) error = nil")
	}
	var invalidSlice []string
	if err := decode.Value(map[string]any{}, &invalidSlice); err == nil {
		t.Fatal("Value(slice from object) error = nil")
	}
}

func TestIntoReportsScalarHookFailuresSafely(t *testing.T) {
	t.Parallel()

	type settings struct {
		Duration time.Duration `config:"duration"`
		URL      url.URL       `config:"url"`
		Mode     mode          `config:"mode"`
		Panic    panicValue    `config:"panic"`
		Failure  failingValue  `config:"failure"`
	}
	var destination settings
	err := decode.Into(map[string]any{
		"duration": "invalid", "url": "://invalid", "mode": "invalid",
		"panic": "sensitive", "failure": "sensitive",
	}, &destination)
	var failures *decode.Errors
	if !errors.As(err, &failures) || len(failures.Fields) != 5 {
		t.Fatalf("Into() error = %T %#v", err, err)
	}
	var panicErr *decode.PanicError
	if !errors.As(err, &panicErr) || panicErr.Operation != "value unmarshaler" {
		t.Fatalf("Into() panic error = %#v", panicErr)
	}
	if !errors.Is(err, errValueRejected) {
		t.Fatalf("Into() error does not preserve value rejection: %v", err)
	}
	if strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), "canary") {
		t.Fatalf("Into() error leaked input: %q", err)
	}

	for name, input := range map[string]any{
		"duration type": int64(1), "url type": true, "text type": int64(1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var target any
			switch name {
			case "duration type":
				target = new(time.Duration)
			case "url type":
				target = new(url.URL)
			default:
				target = new(mode)
			}
			if err := decode.Value(input, target); err == nil {
				t.Fatal("Value() error = nil")
			}
		})
	}
}

func TestTypedErrorsExposeSafeMetadataAndCauses(t *testing.T) {
	t.Parallel()

	cause := errors.New("canary cause")
	field := &decode.FieldError{
		Path: "token", Source: "environment", Location: "APP_TOKEN",
		Expected: "string", Received: "int", Cause: cause,
	}
	if !errors.Is(field, cause) {
		t.Fatal("FieldError does not unwrap cause")
	}
	if unwrapped := errors.Unwrap(field); unwrapped == nil ||
		strings.Contains(unwrapped.Error(), "canary cause") {
		t.Fatalf("FieldError.Unwrap() leaked cause: %v", unwrapped)
	}
	if got := field.Error(); !strings.Contains(got, `source "environment"`) ||
		!strings.Contains(got, `at "APP_TOKEN"`) || strings.Contains(got, "canary cause") {
		t.Fatalf("FieldError.Error() = %q", got)
	}
	if text, err := field.MarshalText(); err != nil || string(text) != field.Error() {
		t.Fatalf("FieldError.MarshalText() = %q, %v", text, err)
	}
	errorsGroup := &decode.Errors{Fields: []*decode.FieldError{field}}
	if got := errorsGroup.Error(); got != "decode config: 1 field errors" {
		t.Fatalf("Errors.Error() = %q", got)
	}
	if text, err := errorsGroup.MarshalText(); err != nil || string(text) != errorsGroup.Error() {
		t.Fatalf("Errors.MarshalText() = %q, %v", text, err)
	}
	if unwrapped := errorsGroup.Unwrap(); len(unwrapped) != 1 || !errors.Is(unwrapped[0], field) {
		t.Fatalf("Errors.Unwrap() = %#v", unwrapped)
	}
	panicErr := &decode.PanicError{Operation: "test extension"}
	if panicErr.Error() != "config test extension panicked" {
		t.Fatalf("PanicError.Error() = %q", panicErr)
	}
}

func TestHookCauseIsRedactedAtEveryExposedErrorLayer(t *testing.T) {
	t.Parallel()

	var destination failingValue
	err := decode.Value("secret-input", &destination)
	var field *decode.FieldError
	if !errors.As(err, &field) || !errors.Is(err, errValueRejected) {
		t.Fatalf("Value() error = %T %v", err, err)
	}
	for _, exposed := range []error{err, field.Cause, errors.Unwrap(field)} {
		if exposed == nil || strings.Contains(exposed.Error(), "canary-secret-value") {
			t.Fatalf("exposed error leaked custom cause: %T %v", exposed, exposed)
		}
	}
}

func TestValueCoversSuccessfulHooksNilScalarsAndPointerFailures(t *testing.T) {
	t.Parallel()

	var hook successfulValue
	if err := decode.Value("value", &hook); err != nil || !hook.Called {
		t.Fatalf("Value(hook) = %#v, %v", hook, err)
	}
	var scalar int
	if err := decode.Value(nil, &scalar); err == nil {
		t.Fatal("Value(nil scalar) error = nil")
	}
	var pointer *int
	if err := decode.Value("invalid", &pointer); err == nil || pointer != nil {
		t.Fatalf("Value(pointer failure) = %v, %v", pointer, err)
	}
	var structure struct{ Value string }
	if err := decode.Value("invalid", &structure); err == nil {
		t.Fatal("Value(struct from string) error = nil")
	}
	var boolean bool
	if err := decode.Value(true, &boolean); err != nil || !boolean {
		t.Fatalf("Value(bool) = %t, %v", boolean, err)
	}
	if err := decode.Value("true", &boolean); err == nil {
		t.Fatal("Value(bool from string) error = nil")
	}
	var implicit struct{ Value string }
	if err := decode.Value(map[string]any{"value": "loaded"}, &implicit); err != nil || implicit.Value != "loaded" {
		t.Fatalf("Value(implicit field) = %#v, %v", implicit, err)
	}
	var text string
	if err := decode.Value([1]string{"value"}, &text); err == nil {
		t.Fatal("Value(array to string) error = nil")
	}
}

func TestValueAcceptsEveryIntegerSourceKind(t *testing.T) {
	t.Parallel()

	values := []any{
		int(1), int8(2), int16(3), int32(4), int64(5),
		uint(6), uint8(7), uint16(8), uint32(9), uint64(10),
	}
	for _, input := range values {
		var got int64
		if err := decode.Value(input, &got); err != nil {
			t.Fatalf("Value(%T) error = %v", input, err)
		}
	}

	var gotFloat float64
	if err := decode.Value(float64(1.5), &gotFloat); err != nil || gotFloat != 1.5 {
		t.Fatalf("Value(float64) = %v, %v", gotFloat, err)
	}
	var narrow float32
	if err := decode.Value(math.MaxFloat64, &narrow); err == nil {
		t.Fatal("Value(float32 overflow) error = nil")
	}
}

func TestContextDecodeHooksObserveCancellationAndRecoverPanics(t *testing.T) {
	t.Parallel()

	type settings struct {
		Value contextValueHook `config:"value"`
		Text  contextTextHook  `config:"text"`
	}
	var got settings
	if err := decode.IntoContext(
		context.Background(),
		map[string]any{"value": "loaded", "text": "text"},
		&got,
	); err != nil || got.Value.Value != "loaded" || got.Text != "text" {
		t.Fatalf("IntoContext() = %#v, %v", got, err)
	}

	for name, destination := range map[string]any{
		"value": &contextValueHook{},
		"text":  new(contextTextHook),
	} {
		t.Run(name+" panic", func(t *testing.T) {
			t.Parallel()
			err := decode.ValueContext(context.Background(), "panic", destination)
			var panicErr *decode.PanicError
			if !errors.As(err, &panicErr) {
				t.Fatalf("ValueContext() error = %T %v, want PanicError", err, err)
			}
		})
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := decode.ValueContext(canceled, "value", &contextValueHook{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ValueContext(canceled) error = %v", err)
	}
	if err := decode.ValueContext(&stagedDecodeContext{}, "value", &contextValueHook{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ValueContext(mid-decode cancellation) error = %v", err)
	}
	if err := decode.ValueContext(context.Background(), int64(1), new(contextTextHook)); err == nil {
		t.Fatal("ValueContext(non-text hook input) error = nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, contextHookKey{}, cancel)
	destination := contextValueHook{}
	if err := decode.ValueContext(ctx, "value", &destination); !errors.Is(err, context.Canceled) {
		t.Fatalf("ValueContext(hook cancellation) error = %v", err)
	}
	if destination.Value != "" {
		t.Fatalf("ValueContext() partially assigned destination = %#v", destination)
	}
}

type stagedDecodeContext struct{ calls int }

func (*stagedDecodeContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*stagedDecodeContext) Done() <-chan struct{}       { return nil }
func (*stagedDecodeContext) Value(any) any               { return nil }
func (ctx *stagedDecodeContext) Err() error {
	ctx.calls++
	if ctx.calls > 1 {
		return context.Canceled
	}
	return nil
}

func contains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}
