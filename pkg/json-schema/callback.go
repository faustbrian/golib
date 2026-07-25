package jsonschema

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
)

type callbackError struct {
	operation string
	cause     error
}

func (err *callbackError) Error() string {
	return err.operation + " failed"
}

func (err *callbackError) Unwrap() error {
	return err.cause
}

func callKeywordCompiler(
	ctx context.Context,
	compiler KeywordCompiler,
	dialect Dialect,
	value Value,
) (evaluator KeywordEvaluator, err error) {
	defer containCallbackPanic(ctx, "custom keyword compiler", &err)

	evaluator, err = compiler.Compile(ctx, dialect, value)
	return evaluator, redactCallbackError("custom keyword compiler", err)
}

func callKeywordEvaluator(
	ctx context.Context,
	evaluator KeywordEvaluator,
	value Value,
) (result KeywordResult, err error) {
	defer containCallbackPanic(ctx, "custom keyword evaluator", &err)

	result, err = evaluator.Evaluate(ctx, value)
	return result, redactCallbackError("custom keyword evaluator", err)
}

func callFormatChecker(
	ctx context.Context,
	checker FormatChecker,
	value string,
) (valid bool, err error) {
	defer containCallbackPanic(ctx, "format checker", &err)

	valid, err = checker.Valid(ctx, value)
	if _, custom := checker.(customFormatChecker); custom {
		err = redactCallbackError("format checker", err)
	}
	return valid, err
}

func callResourceLoader(
	ctx context.Context,
	loader ResourceLoader,
	identifier string,
) (raw []byte, err error) {
	defer containCallbackPanic(ctx, "resource loader", &err)

	raw, err = loader.Load(ctx, identifier)
	return raw, redactCallbackError("resource loader", err)
}

func callFilesystemRead(
	ctx context.Context,
	filesystem fs.FS,
	name string,
) (raw []byte, err error) {
	defer containCallbackPanic(ctx, "filesystem loader", &err)

	raw, err = fs.ReadFile(filesystem, name)
	return raw, redactCallbackError("filesystem loader", err)
}

func callJSONEncode(
	ctx context.Context,
	encoder *json.Encoder,
	value any,
) (err error) {
	defer containCallbackPanic(ctx, "JSON marshaler", &err)

	err = encoder.Encode(value)
	return redactCallbackError("JSON marshaler", err)
}

func redactCallbackError(operation string, cause error) error {
	if cause == nil {
		return nil
	}
	return &callbackError{operation: operation, cause: cause}
}

func containCallbackPanic(ctx context.Context, operation string, err *error) {
	if recovered := recover(); recovered != nil {
		if ctx != nil && ctx.Err() != nil {
			*err = ctx.Err()
			return
		}
		*err = fmt.Errorf("%w: %s", ErrCallbackPanic, operation)
	}
}
