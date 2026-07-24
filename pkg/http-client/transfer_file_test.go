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
	if _, err := copyResponseToFile(nilContext, nil, "file", FileTransferOptions{}, &fakeFileTransferFS{}); !errors.Is(err, ErrInvalidTransfer) {
		t.Fatalf("invalid input error = %v", err)
	}
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

	for _, test := range []struct {
		name       string
		file       *fakeTransferFile
		filesystem *fakeFileTransferFS
	}{
		{
			name:       "mode and cleanup",
			file:       &fakeTransferFile{chmodErr: failure, closeErr: failure},
			filesystem: &fakeFileTransferFS{removeErr: failure},
		},
		{
			name: "sync",
			file: &fakeTransferFile{syncErr: failure}, filesystem: &fakeFileTransferFS{},
		},
		{
			name: "close",
			file: &fakeTransferFile{closeErr: failure}, filesystem: &fakeFileTransferFS{},
		},
		{
			name: "rename",
			file: &fakeTransferFile{}, filesystem: &fakeFileTransferFS{renameErr: failure},
		},
		{
			name: "directory open",
			file: &fakeTransferFile{}, filesystem: &fakeFileTransferFS{openErr: failure},
		},
		{
			name: "directory sync and close",
			file: &fakeTransferFile{},
			filesystem: &fakeFileTransferFS{
				directory: &fakeTransferDirectory{syncErr: failure, closeErr: failure},
			},
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
			if !errors.Is(err, failure) {
				t.Fatalf("filesystem error = %v", err)
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
	chmodErr error
	syncErr  error
	closeErr error
}

func (*fakeTransferFile) Name() string                 { return "temporary" }
func (file *fakeTransferFile) Chmod(os.FileMode) error { return file.chmodErr }
func (file *fakeTransferFile) Sync() error             { return file.syncErr }
func (file *fakeTransferFile) Close() error            { return file.closeErr }

type fakeTransferDirectory struct {
	syncErr  error
	closeErr error
}

func (directory *fakeTransferDirectory) Sync() error  { return directory.syncErr }
func (directory *fakeTransferDirectory) Close() error { return directory.closeErr }

type fakeFileTransferFS struct {
	file      *fakeTransferFile
	directory *fakeTransferDirectory
	createErr error
	removeErr error
	renameErr error
	openErr   error
}

func (filesystem *fakeFileTransferFS) CreateTemp(string, string) (fileTransferFile, error) {
	if filesystem.createErr != nil {
		return nil, filesystem.createErr
	}
	return filesystem.file, nil
}

func (filesystem *fakeFileTransferFS) Remove(string) error         { return filesystem.removeErr }
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
