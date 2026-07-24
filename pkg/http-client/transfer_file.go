package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// FileTransferOptions configures atomic response-to-file replacement.
type FileTransferOptions struct {
	Mode     os.FileMode
	Transfer TransferOptions
}

// FileTransferError reports filesystem failure without rendering paths or
// underlying errors.
type FileTransferError struct {
	Operation string
	Cause     error
}

// Error implements error.
func (err *FileTransferError) Error() string {
	return "HTTP file transfer " + err.Operation + " failed"
}

// Unwrap returns the filesystem failure.
func (err *FileTransferError) Unwrap() error { return err.Cause }

// CopyResponseToFile streams into a same-directory temporary file and replaces
// destination only after transfer validation, file sync, and close succeed.
func CopyResponseToFile(
	ctx context.Context,
	response *http.Response,
	destination string,
	options FileTransferOptions,
) (result TransferResult, resultErr error) {
	return copyResponseToFile(ctx, response, destination, options, osFileTransferFS{})
}

type fileTransferFile interface {
	io.Writer
	Name() string
	Chmod(os.FileMode) error
	Sync() error
	Close() error
}

type fileTransferDirectory interface {
	Sync() error
	Close() error
}

type fileTransferFS interface {
	CreateTemp(string, string) (fileTransferFile, error)
	Remove(string) error
	Rename(string, string) error
	OpenDirectory(string) (fileTransferDirectory, error)
}

type osFileTransferFS struct{}

func (osFileTransferFS) CreateTemp(directory string, pattern string) (fileTransferFile, error) {
	return os.CreateTemp(directory, pattern)
}

func (osFileTransferFS) Remove(name string) error { return os.Remove(name) }

func (osFileTransferFS) Rename(oldPath string, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func (osFileTransferFS) OpenDirectory(directory string) (fileTransferDirectory, error) {
	return os.Open(directory)
}

func copyResponseToFile(
	ctx context.Context,
	response *http.Response,
	destination string,
	options FileTransferOptions,
	filesystem fileTransferFS,
) (result TransferResult, resultErr error) {
	if ctx == nil || response == nil || response.Body == nil {
		return result, fmt.Errorf("%w: context, response, or body is invalid", ErrInvalidTransfer)
	}
	closeBeforeCopy := func(err error) error {
		if closeErr := response.Body.Close(); closeErr != nil {
			return errors.Join(err, &FileTransferError{Operation: "response close", Cause: closeErr})
		}
		return err
	}
	mode := options.Mode
	if mode == 0 {
		mode = 0o600
	}
	if destination == "" || mode.Perm() != mode || mode.Perm() == 0 {
		return result, closeBeforeCopy(&FileTransferError{Operation: "configuration", Cause: ErrInvalidTransfer})
	}
	directory := filepath.Dir(destination)
	temporary, err := filesystem.CreateTemp(directory, ".http-transfer-*")
	if err != nil {
		return result, closeBeforeCopy(&FileTransferError{Operation: "temporary create", Cause: err})
	}
	temporaryClosed := false
	renamed := false
	defer func() {
		if !temporaryClosed {
			if closeErr := temporary.Close(); closeErr != nil {
				resultErr = errors.Join(resultErr, &FileTransferError{Operation: "temporary close", Cause: closeErr})
			}
		}
		if !renamed {
			if removeErr := filesystem.Remove(temporary.Name()); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				resultErr = errors.Join(resultErr, &FileTransferError{Operation: "temporary remove", Cause: removeErr})
			}
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return result, &FileTransferError{Operation: "temporary mode", Cause: err}
	}
	result, err = CopyResponse(ctx, response, temporary, options.Transfer)
	if err != nil {
		return result, err
	}
	if err := temporary.Sync(); err != nil {
		return result, &FileTransferError{Operation: "temporary sync", Cause: err}
	}
	if err := temporary.Close(); err != nil {
		temporaryClosed = true
		return result, &FileTransferError{Operation: "temporary close", Cause: err}
	}
	temporaryClosed = true
	if err := filesystem.Rename(temporary.Name(), destination); err != nil {
		return result, &FileTransferError{Operation: "destination replace", Cause: err}
	}
	renamed = true
	directoryHandle, err := filesystem.OpenDirectory(directory)
	if err != nil {
		return result, &FileTransferError{Operation: "directory open", Cause: err}
	}
	syncErr := directoryHandle.Sync()
	closeErr := directoryHandle.Close()
	if syncErr != nil || closeErr != nil {
		return result, errors.Join(
			wrapFileTransferError("directory sync", syncErr),
			wrapFileTransferError("directory close", closeErr),
		)
	}

	return result, nil
}

func wrapFileTransferError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &FileTransferError{Operation: operation, Cause: err}
}
