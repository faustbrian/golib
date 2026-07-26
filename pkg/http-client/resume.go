package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// HTTPDoer executes one standard HTTP request.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// ResumeFileOptions configures persistent partial-file continuation.
type ResumeFileOptions struct {
	PartialPath    string
	Mode           os.FileMode
	Validator      RangeValidator
	DisableRestart bool
	Transfer       TransferOptions
}

// ResumeError reports resume state or filesystem failure without rendering
// paths, validators, headers, or response data.
type ResumeError struct {
	Operation string
	Cause     error
}

// Error implements error.
func (err *ResumeError) Error() string { return "HTTP resume " + err.Operation + " failed" }

// Unwrap returns the resume failure.
func (err *ResumeError) Unwrap() error { return err.Cause }

// ResumeDownloadToFile continues a same-directory partial file, validates the
// complete representation, and atomically publishes destination. Failed range
// appends roll back to their prior safe offset.
func ResumeDownloadToFile(
	ctx context.Context,
	doer HTTPDoer,
	request *http.Request,
	destination string,
	options ResumeFileOptions,
) (TransferResult, error) {
	return resumeDownloadToFile(ctx, doer, request, destination, options, osResumeFS{})
}

type resumeFile interface {
	io.Reader
	io.Writer
	io.Seeker
	Chmod(os.FileMode) error
	Stat() (os.FileInfo, error)
	Truncate(int64) error
	Sync() error
	Close() error
}

type resumeFS interface {
	OpenFile(string, int, os.FileMode) (resumeFile, error)
	Rename(string, string) error
	OpenDirectory(string) (fileTransferDirectory, error)
}

type osResumeFS struct{}

func (osResumeFS) OpenFile(name string, flag int, mode os.FileMode) (resumeFile, error) {
	return os.OpenFile(name, flag, mode)
}

func (osResumeFS) Rename(oldPath string, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func (osResumeFS) OpenDirectory(directory string) (fileTransferDirectory, error) {
	return os.Open(directory)
}

func resumeDownloadToFile(
	ctx context.Context,
	doer HTTPDoer,
	request *http.Request,
	destination string,
	options ResumeFileOptions,
	filesystem resumeFS,
) (TransferResult, error) {
	if ctx == nil || nilLike(doer) || request == nil || destination == "" {
		return TransferResult{}, &ResumeError{Operation: "configuration", Cause: ErrInvalidTransfer}
	}
	mode := options.Mode
	if mode == 0 {
		mode = 0o600
	}
	partialPath := options.PartialPath
	if partialPath == "" {
		partialPath = destination + ".partial"
	}
	destinationDirectory := filepath.Clean(filepath.Dir(destination))
	if mode.Perm() != mode || mode.Perm() == 0 || partialPath == destination ||
		filepath.Clean(filepath.Dir(partialPath)) != destinationDirectory {
		return TransferResult{}, &ResumeError{Operation: "configuration", Cause: ErrInvalidTransfer}
	}
	partial, err := filesystem.OpenFile(partialPath, os.O_CREATE|os.O_RDWR, mode)
	if err != nil {
		return TransferResult{}, &ResumeError{Operation: "partial open", Cause: err}
	}
	closed := false
	defer func() {
		if !closed {
			_ = partial.Close()
		}
	}()
	if err := partial.Chmod(mode); err != nil {
		return TransferResult{}, &ResumeError{Operation: "partial mode", Cause: err}
	}
	information, err := partial.Stat()
	if err != nil {
		return TransferResult{}, &ResumeError{Operation: "partial stat", Cause: err}
	}
	offset := information.Size()
	if offset > 0 && options.Validator.ETag == "" && options.Validator.LastModified.IsZero() {
		return TransferResult{}, &ResumeError{Operation: "validator", Cause: ErrInvalidRange}
	}
	ranged, err := WithRange(request.Clone(ctx), RangeOptions{Offset: offset, Validator: options.Validator})
	if err != nil {
		return TransferResult{}, err
	}
	response, err := doer.Do(ranged)
	if err != nil {
		return TransferResult{}, err
	}
	metadata, disposition, err := ValidateRangeResponse(response, RangeResponseOptions{
		Offset: offset, Validator: options.Validator, AllowRestart: !options.DisableRestart,
	})
	if err != nil {
		return TransferResult{}, errors.Join(err, closeResumeResponse(response))
	}
	var result TransferResult
	switch disposition {
	case RangeContinue:
		if _, err := partial.Seek(offset, io.SeekStart); err != nil {
			_ = response.Body.Close()
			return result, &ResumeError{Operation: "partial seek", Cause: err}
		}
		total := metadata.Total
		if total < 0 {
			total = options.Transfer.ExpectedBytes
		}
		if err := observeResumeProgress(ctx, options.Transfer.Progress, TransferProgress{
			Bytes: offset, Total: total,
		}); err != nil {
			_ = response.Body.Close()
			return result, err
		}
		suffix := options.Transfer
		maximum := suffix.MaximumBytes
		if maximum == 0 {
			maximum = defaultMaximumTransferBytes
		}
		if offset >= maximum {
			_ = response.Body.Close()
			return result, &TransferLimitError{MaximumBytes: maximum, Bytes: offset}
		}
		suffix.MaximumBytes = maximum - offset
		suffix.ExpectedBytes = metadata.End - metadata.Start + 1
		suffix.DigestAlgorithm = ""
		suffix.ExpectedDigest = nil
		suffix.Progress = nil
		if _, err := CopyResponse(ctx, response, partial, suffix); err != nil {
			return result, errors.Join(err, rollbackPartial(partial, offset))
		}
		result, err = validateCompletePartial(ctx, partial, options.Transfer)
		if err != nil {
			return result, errors.Join(err, rollbackPartial(partial, offset))
		}
		if err := observeResumeProgress(ctx, options.Transfer.Progress, TransferProgress{
			Bytes: result.Bytes, Total: total, Elapsed: result.Elapsed,
			Digest: append([]byte(nil), result.Digest...), Complete: true,
		}); err != nil {
			return result, errors.Join(err, rollbackPartial(partial, offset))
		}
	case RangeRestart:
		if err := partial.Truncate(0); err != nil {
			_ = response.Body.Close()
			return result, &ResumeError{Operation: "partial truncate", Cause: err}
		}
		if _, err := partial.Seek(0, io.SeekStart); err != nil {
			_ = response.Body.Close()
			return result, &ResumeError{Operation: "partial seek", Cause: err}
		}
		result, err = CopyResponse(ctx, response, partial, options.Transfer)
		if err != nil {
			return result, err
		}
	case RangeComplete:
		if closeErr := response.Body.Close(); closeErr != nil {
			return result, &ResumeError{Operation: "response close", Cause: closeErr}
		}
		result, err = validateCompletePartial(ctx, partial, options.Transfer)
		if err != nil {
			return result, err
		}
	}
	if err := partial.Sync(); err != nil {
		return result, &ResumeError{Operation: "partial sync", Cause: err}
	}
	if err := partial.Close(); err != nil {
		closed = true
		return result, &ResumeError{Operation: "partial close", Cause: err}
	}
	closed = true
	if err := filesystem.Rename(partialPath, destination); err != nil {
		return result, &ResumeError{Operation: "destination replace", Cause: err}
	}
	directory, err := filesystem.OpenDirectory(destinationDirectory)
	if err != nil {
		return result, &ResumeError{Operation: "directory open", Cause: err}
	}
	return result, errors.Join(
		wrapResumeError("directory sync", directory.Sync()),
		wrapResumeError("directory close", directory.Close()),
	)
}

func validateCompletePartial(ctx context.Context, partial resumeFile, options TransferOptions) (TransferResult, error) {
	if _, err := partial.Seek(0, io.SeekStart); err != nil {
		return TransferResult{}, &ResumeError{Operation: "partial validation seek", Cause: err}
	}
	information, err := partial.Stat()
	if err != nil {
		return TransferResult{}, &ResumeError{Operation: "partial validation stat", Cause: err}
	}
	options.Progress = nil
	response := &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body:          io.NopCloser(io.LimitReader(partial, information.Size())),
		ContentLength: information.Size(),
	}
	return CopyResponse(ctx, response, io.Discard, options)
}

func rollbackPartial(partial resumeFile, offset int64) error {
	if err := partial.Truncate(offset); err != nil {
		return &ResumeError{Operation: "partial rollback", Cause: err}
	}
	_, err := partial.Seek(offset, io.SeekStart)
	return wrapResumeError("partial rollback seek", err)
}

func closeResumeResponse(response *http.Response) error {
	if response == nil || response.Body == nil {
		return nil
	}
	return wrapResumeError("response close", response.Body.Close())
}

func wrapResumeError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &ResumeError{Operation: operation, Cause: err}
}

func observeResumeProgress(ctx context.Context, observer TransferProgressObserver, update TransferProgress) error {
	if observer == nil {
		return nil
	}
	if err := observeTransferProgress(ctx, observer, update); err != nil {
		return &TransferError{Operation: "progress observation", Cause: err}
	}
	return nil
}
