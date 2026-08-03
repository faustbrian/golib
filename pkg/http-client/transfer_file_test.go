package httpclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyResponseToFileAtomicallyReplacesValidatedDestination(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	destination := filepath.Join(directory, "artifact.bin")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed destination: %v", err)
	}
	content := []byte("validated artifact")
	digest := sha256.Sum256(content)
	response := &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(bytes.NewReader(content)), ContentLength: int64(len(content)),
	}
	result, err := CopyResponseToFile(context.Background(), response, destination, FileTransferOptions{
		Mode: 0o640,
		Transfer: TransferOptions{
			MaximumBytes: 64, DigestAlgorithm: DigestSHA256, ExpectedDigest: digest[:],
		},
	})
	if err != nil {
		t.Fatalf("copy response to file: %v", err)
	}
	stored, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	information, err := os.Stat(destination)
	if err != nil {
		t.Fatalf("stat destination: %v", err)
	}
	if string(stored) != string(content) || result.Bytes != int64(len(content)) ||
		information.Mode().Perm() != 0o640 {
		t.Fatalf("stored = %q, result %#v, mode %o", stored, result, information.Mode().Perm())
	}
	assertNoTransferTemporaryFiles(t, directory)
}

func TestCopyResponseToFileLeavesDestinationUntouchedOnValidationFailure(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	destination := filepath.Join(directory, "artifact.bin")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed destination: %v", err)
	}
	response := &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader("untrusted")), ContentLength: -1,
	}
	_, err := CopyResponseToFile(context.Background(), response, destination, FileTransferOptions{
		Transfer: TransferOptions{
			MaximumBytes: 64, DigestAlgorithm: DigestSHA256,
			ExpectedDigest: make([]byte, sha256.Size),
		},
	})
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("validation error = %v", err)
	}
	stored, readErr := os.ReadFile(destination)
	if readErr != nil || string(stored) != "old" {
		t.Fatalf("destination = %q, %v", stored, readErr)
	}
	assertNoTransferTemporaryFiles(t, directory)
}

func TestCopyResponseToFileRejectsInvalidPathsAndModesSecretSafely(t *testing.T) {
	t.Parallel()

	secret := "path-secret"
	for _, test := range []struct {
		name        string
		destination string
		mode        os.FileMode
	}{
		{name: "empty path", destination: ""},
		{name: "invalid mode", destination: filepath.Join(t.TempDir(), "file"), mode: 0o1000},
		{name: "missing directory", destination: filepath.Join(t.TempDir(), secret, "file")},
		{name: "directory destination", destination: t.TempDir()},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{
				StatusCode: http.StatusOK, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader("body")), ContentLength: 4,
			}
			_, err := CopyResponseToFile(context.Background(), response, test.destination, FileTransferOptions{
				Mode: test.mode, Transfer: TransferOptions{MaximumBytes: 64},
			})
			var fileError *FileTransferError
			if !errors.As(err, &fileError) || strings.Contains(err.Error(), secret) {
				t.Fatalf("file transfer error = %#v", err)
			}
		})
	}
}

func TestCopyResponseToFileFilesystemFailureBoundaries(t *testing.T) {
	t.Parallel()

	failure := errors.New("filesystem")
	if !errors.Is(&FileTransferError{Cause: failure}, failure) {
		t.Fatal("file transfer error did not unwrap")
	}
	newResponse := func(body io.ReadCloser) *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header), Body: body,
			ContentLength: 4,
		}
	}
	var nilContext context.Context
	for name, input := range map[string]struct {
		ctx      context.Context
		response *http.Response
	}{
		"nil context":  {response: newResponse(http.NoBody)},
		"nil response": {ctx: context.Background()},
		"nil body":     {ctx: context.Background(), response: &http.Response{}},
	} {
		if _, err := copyResponseToFile(input.ctx, input.response, "file", FileTransferOptions{}, &fakeFileTransferFS{}); !errors.Is(err, ErrInvalidTransfer) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	_ = nilContext
	response := newResponse(&compressionErrorBody{Reader: strings.NewReader("body"), closeErr: failure})
	if _, err := copyResponseToFile(context.Background(), response, "", FileTransferOptions{}, &fakeFileTransferFS{}); !errors.Is(err, failure) {
		t.Fatalf("configuration close error = %v", err)
	}
	response = newResponse(&compressionErrorBody{Reader: strings.NewReader("body"), closeErr: failure})
	if _, err := copyResponseToFile(context.Background(), response, "file", FileTransferOptions{}, &fakeFileTransferFS{
		createErr: failure,
	}); !errors.Is(err, failure) {
		t.Fatalf("create and close error = %v", err)
	}

	chmodFailure := errors.New("chmod")
	closeFailure := errors.New("close")
	removeFailure := errors.New("remove")
	for _, test := range []struct {
		name        string
		file        *fakeTransferFile
		filesystem  *fakeFileTransferFS
		causes      []error
		closeCalls  int
		removeCalls int
	}{
		{
			name:       "mode and cleanup",
			file:       &fakeTransferFile{chmodErr: chmodFailure, closeErr: closeFailure},
			filesystem: &fakeFileTransferFS{removeErr: removeFailure},
			causes:     []error{chmodFailure, closeFailure, removeFailure}, closeCalls: 1, removeCalls: 1,
		},
		{
			name: "sync",
			file: &fakeTransferFile{syncErr: failure}, filesystem: &fakeFileTransferFS{},
			causes: []error{failure}, closeCalls: 1, removeCalls: 1,
		},
		{
			name: "close",
			file: &fakeTransferFile{closeErr: failure}, filesystem: &fakeFileTransferFS{},
			causes: []error{failure}, closeCalls: 1, removeCalls: 1,
		},
		{
			name: "rename",
			file: &fakeTransferFile{}, filesystem: &fakeFileTransferFS{renameErr: failure},
			causes: []error{failure}, closeCalls: 1, removeCalls: 1,
		},
		{
			name: "directory open",
			file: &fakeTransferFile{}, filesystem: &fakeFileTransferFS{openErr: failure},
			causes: []error{failure}, closeCalls: 1,
		},
		{
			name: "directory sync",
			file: &fakeTransferFile{},
			filesystem: &fakeFileTransferFS{
				directory: &fakeTransferDirectory{syncErr: failure},
			},
			causes: []error{failure}, closeCalls: 1,
		},
		{
			name: "directory close",
			file: &fakeTransferFile{},
			filesystem: &fakeFileTransferFS{
				directory: &fakeTransferDirectory{closeErr: failure},
			},
			causes: []error{failure}, closeCalls: 1,
		},
		{
			name: "missing temporary already removed",
			file: &fakeTransferFile{chmodErr: failure}, filesystem: &fakeFileTransferFS{removeErr: os.ErrNotExist},
			causes: []error{failure}, closeCalls: 1, removeCalls: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.filesystem.file = test.file
			response := newResponse(io.NopCloser(strings.NewReader("body")))
			_, err := copyResponseToFile(
				context.Background(), response, "destination", FileTransferOptions{
					Transfer: TransferOptions{MaximumBytes: 64},
				}, test.filesystem,
			)
			for _, cause := range test.causes {
				if !errors.Is(err, cause) {
					t.Fatalf("filesystem error = %v, want cause %v", err, cause)
				}
			}
			if errors.Is(err, os.ErrNotExist) {
				t.Fatalf("filesystem error retained ignored missing temporary: %v", err)
			}
			if test.file.closeCalls != test.closeCalls || test.filesystem.removeCalls != test.removeCalls {
				t.Fatalf("cleanup calls = close:%d remove:%d, want close:%d remove:%d", test.file.closeCalls, test.filesystem.removeCalls, test.closeCalls, test.removeCalls)
			}
		})
	}
	if wrapped := wrapFileTransferError("test", nil); wrapped != nil {
		t.Fatalf("nil filesystem error = %v", wrapped)
	}
}

func assertNoTransferTemporaryFiles(t *testing.T, directory string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(directory, ".http-transfer-*"))
	if err != nil {
		t.Fatalf("glob temporary files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %#v", matches)
	}
}

type fakeTransferFile struct {
	bytes.Buffer
	chmodErr   error
	syncErr    error
	closeErr   error
	closeCalls int
}

func (*fakeTransferFile) Name() string                 { return "temporary" }
func (file *fakeTransferFile) Chmod(os.FileMode) error { return file.chmodErr }
func (file *fakeTransferFile) Sync() error             { return file.syncErr }
func (file *fakeTransferFile) Close() error {
	file.closeCalls++
	return file.closeErr
}

type fakeTransferDirectory struct {
	syncErr  error
	closeErr error
}

func (directory *fakeTransferDirectory) Sync() error  { return directory.syncErr }
func (directory *fakeTransferDirectory) Close() error { return directory.closeErr }

type fakeFileTransferFS struct {
	file        *fakeTransferFile
	directory   *fakeTransferDirectory
	createErr   error
	removeErr   error
	renameErr   error
	openErr     error
	removeCalls int
}

func (filesystem *fakeFileTransferFS) CreateTemp(string, string) (fileTransferFile, error) {
	if filesystem.createErr != nil {
		return nil, filesystem.createErr
	}
	return filesystem.file, nil
}

func (filesystem *fakeFileTransferFS) Remove(string) error {
	filesystem.removeCalls++
	return filesystem.removeErr
}
func (filesystem *fakeFileTransferFS) Rename(string, string) error { return filesystem.renameErr }

func (filesystem *fakeFileTransferFS) OpenDirectory(string) (fileTransferDirectory, error) {
	if filesystem.openErr != nil {
		return nil, filesystem.openErr
	}
	if filesystem.directory == nil {
		filesystem.directory = &fakeTransferDirectory{}
	}
	return filesystem.directory, nil
}
