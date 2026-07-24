package sourceio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"runtime"
	"testing"
	"testing/fstest"
	"time"

	config "github.com/faustbrian/golib/pkg/config"
)

func TestBytesIsImmutableRepeatableAndBounded(t *testing.T) {
	t.Parallel()

	data := []byte("original")
	input := Bytes(data)
	data[0] = 'X'
	first, err := input.Read(context.Background(), 8)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	first[0] = 'Y'
	second, err := input.Read(context.Background(), 8)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(second) != "original" {
		t.Fatalf("second Read() = %q", second)
	}
	if _, err := input.Read(context.Background(), 7); err == nil {
		t.Fatal("Read() error = nil, want byte limit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := input.Read(ctx, 8); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read() error = %v, want context.Canceled", err)
	}
}

func TestContextReaderChecksContextAndSystemChangeToken(t *testing.T) {
	t.Parallel()

	data, err := io.ReadAll(ContextReader(context.Background(), []byte("value")))
	if err != nil || string(data) != "value" {
		t.Fatalf("ContextReader() = %q, %v", data, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := io.ReadAll(ContextReader(ctx, []byte("value"))); !errors.Is(err, context.Canceled) {
		t.Fatalf("ContextReader(canceled) error = %v", err)
	}

	file, err := os.CreateTemp(t.TempDir(), "change-token")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	info, err := os.Stat(file.Name())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	_, tracked := systemChangeToken(info)
	if (runtime.GOOS == "darwin" || runtime.GOOS == "linux") && !tracked {
		t.Fatalf("systemChangeToken() tracked = false on %s", runtime.GOOS)
	}
}

func TestFromFSValidatesAndReopensFiles(t *testing.T) {
	t.Parallel()

	if _, err := FromFS(nil, "config.json"); err == nil {
		t.Fatal("FromFS(nil) error = nil")
	}
	if _, err := FromFS(fstest.MapFS{}, "../config.json"); err == nil {
		t.Fatal("FromFS(invalid path) error = nil")
	}
	missing, err := FromFS(fstest.MapFS{}, "missing.json")
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	if _, err := missing.Read(context.Background(), 100); !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("Read() error = %v, want ErrNotFound", err)
	}

	filesystem := fstest.MapFS{"config.json": {Data: []byte("first")}}
	input, err := FromFS(filesystem, "config.json")
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	first, err := input.Read(context.Background(), 100)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	filesystem["config.json"].Data = []byte("second")
	second, err := input.Read(context.Background(), 100)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(first) != "first" || string(second) != "second" {
		t.Fatalf("reads = %q, %q", first, second)
	}
}

func TestFromFSPreservesNonAbsenceOpenError(t *testing.T) {
	t.Parallel()

	want := errors.New("permission denied")
	input, err := FromFS(errorFS{err: want}, "config.json")
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	if _, err := input.Read(context.Background(), 100); !errors.Is(err, want) {
		t.Fatalf("Read() error = %v, want %v", err, want)
	}
}

func TestFromFSClosesFileReturnedWithOpenError(t *testing.T) {
	t.Parallel()

	want := errors.New("open failed after allocating a file")
	file := &trackedFile{Reader: bytes.NewReader(nil)}
	input, err := FromFS(fileErrorFS{file: file, err: want}, "config.json")
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	if _, err := input.Read(context.Background(), 100); !errors.Is(err, want) {
		t.Fatalf("Read() error = %v, want %v", err, want)
	}
	if file.closes != 1 {
		t.Fatalf("Close() calls = %d, want 1", file.closes)
	}
}

func TestFromFSPreservesHostileMissingErrorWithoutCallbacks(t *testing.T) {
	t.Parallel()

	want := &hostileMissingError{}
	input, err := FromFS(missingErrorFS{err: want}, "config.json")
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	//nolint:errorlint // Identity check avoids hostile Is and Error callbacks.
	if _, err := input.Read(context.Background(), 100); err != want {
		t.Fatalf("Read() error identity changed: %T", err)
	}

	cycle := &fs.PathError{Op: "open", Path: "config.json"}
	cycle.Err = cycle
	input, err = FromFS(errorFS{err: cycle}, "config.json")
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	//nolint:errorlint // Identity proves bounded trusted-wrapper traversal.
	if _, err := input.Read(context.Background(), 100); err != cycle {
		t.Fatalf("Read() cyclic error identity changed: %T", err)
	}
}

func TestFromFSPreservesReaderErrorWithoutCallingIs(t *testing.T) {
	t.Parallel()

	want := &hostileIsError{}
	file := &trackedFile{Reader: errorReader{err: want}}
	input, err := FromFS(fileOnlyFS{file: file}, "config.json")
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	//nolint:errorlint // Identity check avoids invoking the hostile Is method.
	if _, err := input.Read(context.Background(), 100); err != want {
		t.Fatalf("Read() error identity changed: %T", err)
	}
	if file.closes != 1 {
		t.Fatalf("Close() calls = %d, want 1", file.closes)
	}
}

func TestFromFSContainsHostileFilesystemOperations(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		filesystem fs.FS
		want       string
	}{
		"nil file": {
			filesystem: hostileFS{operation: "nil"},
			want:       "filesystem source returned nil file",
		},
		"open panic": {
			filesystem: hostileFS{operation: "open"},
			want:       "filesystem open panicked",
		},
		"read panic": {
			filesystem: hostileFS{operation: "read"},
			want:       "filesystem read panicked",
		},
		"stat panic": {
			filesystem: hostileFS{operation: "stat"},
			want:       "filesystem stat panicked",
		},
		"close panic": {
			filesystem: hostileFS{operation: "close"},
			want:       "filesystem close panicked",
		},
		"generation panic": {
			filesystem: hostileFS{operation: "generation"},
			want:       "filesystem generation lookup panicked",
		},
		"metadata size panic": {
			filesystem: hostileFS{operation: "metadata size"},
			want:       "filesystem metadata inspection panicked",
		},
		"metadata mode panic": {
			filesystem: hostileFS{operation: "metadata mode"},
			want:       "filesystem metadata inspection panicked",
		},
		"metadata modification time panic": {
			filesystem: hostileFS{operation: "metadata modification time"},
			want:       "filesystem metadata inspection panicked",
		},
		"negative read count": {
			filesystem: hostileFS{operation: "negative read count"},
			want:       "source reader returned invalid byte count",
		},
		"oversized read count": {
			filesystem: hostileFS{operation: "oversized read count"},
			want:       "source reader returned invalid byte count",
		},
		"custom error classification ignored": {
			filesystem: hostileFS{operation: "error classification"},
			want:       "hostile classification error",
		},
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		tests["metadata system value panic"] = struct {
			filesystem fs.FS
			want       string
		}{
			filesystem: hostileFS{operation: "metadata system value"},
			want:       "filesystem metadata inspection panicked",
		}
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			input, err := FromFS(test.filesystem, "config.json")
			if err != nil {
				t.Fatalf("FromFS() error = %v", err)
			}
			if _, err := input.Read(context.Background(), 100); err == nil || err.Error() != test.want {
				t.Fatalf("Read() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestFromFSUsesContextAwareOperationsWhenAvailable(t *testing.T) {
	t.Parallel()

	filesystem := &contextualFS{}
	input, err := FromFS(filesystem, "config.json")
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	data, err := input.Read(context.Background(), 100)
	if err != nil || string(data) != "value" {
		t.Fatalf("Read() = %q, %v", data, err)
	}
	if filesystem.opens != 1 || filesystem.file.reads == 0 ||
		filesystem.file.stats != 2 || filesystem.file.closes != 1 {
		t.Fatalf(
			"context calls = opens %d, reads %d, stats %d, closes %d",
			filesystem.opens,
			filesystem.file.reads,
			filesystem.file.stats,
			filesystem.file.closes,
		)
	}
}

func TestFromFSClosesWithIndependentContextAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	filesystem := &contextualFS{cancel: cancel}
	input, err := FromFS(filesystem, "config.json")
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	if _, err := input.Read(ctx, 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read() error = %v, want context.Canceled", err)
	}
	if filesystem.file.closes != 1 {
		t.Fatalf("CloseContext() calls = %d, want 1", filesystem.file.closes)
	}
}

func TestFromFSRejectsFileMutationDuringRead(t *testing.T) {
	t.Parallel()

	input, err := FromFS(changingFS{}, "config.json")
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	data, err := input.Read(context.Background(), 100)
	if !errors.Is(err, config.ErrSourceChanged) {
		t.Fatalf("Read() = %q, %v; want ErrSourceChanged", data, err)
	}
	if data != nil {
		t.Fatalf("Read() data = %q, want nil on mutation", data)
	}
}

func TestStableFSReadRejectsSameMetadataGenerationChange(t *testing.T) {
	t.Parallel()

	file := &generationChangingFile{Reader: bytes.NewReader([]byte("mixed"))}
	input := Input{
		open:     func(context.Context) (io.ReadCloser, error) { return file, nil },
		stableFS: true,
	}
	if data, err := input.Read(context.Background(), 100); !errors.Is(err, config.ErrSourceChanged) || data != nil {
		t.Fatalf("Read() = %q, %v; want ErrSourceChanged", data, err)
	}
}

func TestStableFSReadRejectsInvalidFileAndStatFailures(t *testing.T) {
	t.Parallel()

	statFailure := errors.New("stat failure")
	tests := map[string]struct {
		reader io.ReadCloser
		want   error
	}{
		"not filesystem file": {
			reader: &readCloser{Reader: bytes.NewBufferString("value")},
			want:   errors.New("filesystem source did not return a file"),
		},
		"initial stat": {
			reader: &statFailureFile{Reader: bytes.NewBufferString("value"), failAt: 1, err: statFailure},
			want:   statFailure,
		},
		"final stat": {
			reader: &statFailureFile{Reader: bytes.NewBufferString("value"), failAt: 2, err: statFailure},
			want:   statFailure,
		},
		"nil initial metadata": {
			reader: &statFailureFile{Reader: bytes.NewBufferString("value"), nilAt: 1},
			want:   errors.New("filesystem source returned nil file metadata"),
		},
		"nil final metadata": {
			reader: &statFailureFile{Reader: bytes.NewBufferString("value"), nilAt: 2},
			want:   errors.New("filesystem source returned nil file metadata"),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := Input{
				open:     func(context.Context) (io.ReadCloser, error) { return test.reader, nil },
				stableFS: true,
			}
			_, err := input.Read(context.Background(), 100)
			if errors.Is(test.want, statFailure) {
				if !errors.Is(err, test.want) {
					t.Fatalf("Read() error = %v, want %v", err, test.want)
				}
			} else if err == nil || err.Error() != test.want.Error() {
				t.Fatalf("Read() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestInputReadHandlesReadAndCloseFailures(t *testing.T) {
	t.Parallel()

	readFailure := errors.New("read failure")
	closeFailure := errors.New("close failure")
	tests := map[string]struct {
		reader io.ReadCloser
		want   error
	}{
		"read": {
			reader: &readCloser{Reader: errorReader{err: readFailure}, closeErr: closeFailure},
			want:   readFailure,
		},
		"close": {
			reader: &readCloser{Reader: bytes.NewBufferString("value"), closeErr: closeFailure},
			want:   closeFailure,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := Input{open: func(context.Context) (io.ReadCloser, error) { return test.reader, nil }}
			_, err := input.Read(context.Background(), 100)
			if !errors.Is(err, test.want) {
				t.Fatalf("Read() error = %v, want %v", err, test.want)
			}
		})
	}

	openFailure := errors.New("open failure")
	input := Input{open: func(context.Context) (io.ReadCloser, error) { return nil, openFailure }}
	if _, err := input.Read(context.Background(), 100); !errors.Is(err, openFailure) {
		t.Fatalf("Read() error = %v, want open failure", err)
	}
}

func TestReadValidatesBoundsErrorsAndCancellation(t *testing.T) {
	t.Parallel()

	if _, err := Read(context.Background(), nil, 1); err == nil {
		t.Fatal("Read(nil) error = nil")
	}
	if _, err := Read(context.Background(), bytes.NewBufferString("too large"), 3); err == nil {
		t.Fatal("Read() error = nil, want byte limit")
	}
	if got, err := Read(context.Background(), bytes.NewBufferString("value"), math.MaxInt64); err != nil || string(got) != "value" {
		t.Fatalf("Read(MaxInt64) = %q, %v", got, err)
	}

	want := errors.New("reader failed")
	if _, err := Read(context.Background(), errorReader{err: want}, 100); !errors.Is(err, want) {
		t.Fatalf("Read() error = %v, want %v", err, want)
	}
	if _, err := Read(context.Background(), zeroProgressReader{}, 100); !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("Read(no progress) error = %v, want io.ErrNoProgress", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelReader{cancel: cancel}
	if _, err := Read(ctx, reader, 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read() error = %v, want context.Canceled", err)
	}
}

type readCloser struct {
	io.Reader
	closeErr error
}

func (r *readCloser) Close() error { return r.closeErr }

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

type cancelReader struct {
	cancel context.CancelFunc
	read   bool
}

type errorFS struct{ err error }

func (f errorFS) Open(string) (fs.File, error) { return nil, f.err }

type fileErrorFS struct {
	file fs.File
	err  error
}

func (filesystem fileErrorFS) Open(string) (fs.File, error) {
	return filesystem.file, filesystem.err
}

type fileOnlyFS struct{ file fs.File }

func (filesystem fileOnlyFS) Open(string) (fs.File, error) {
	return filesystem.file, nil
}

type missingErrorFS struct{ err error }

func (filesystem missingErrorFS) Open(string) (fs.File, error) {
	return nil, filesystem.err
}

type hostileMissingError struct{}

func (hostileMissingError) Error() string {
	panic("hostile missing error value")
}

func (hostileMissingError) Is(target error) bool {
	return target == fs.ErrNotExist
}

type trackedFile struct {
	io.Reader
	closes int
}

func (file *trackedFile) Close() error {
	file.closes++
	return nil
}

func (*trackedFile) Stat() (fs.FileInfo, error) {
	return changingFileInfo{size: 0}, nil
}

type hostileFS struct{ operation string }

func (filesystem hostileFS) Open(string) (fs.File, error) {
	if filesystem.operation == "open" {
		panic("hostile open value")
	}
	if filesystem.operation == "nil" {
		return nil, nil
	}
	if filesystem.operation == "error classification" {
		return nil, hostileIsError{}
	}
	return &hostileFile{
		Reader:    bytes.NewReader([]byte("value")),
		operation: filesystem.operation,
	}, nil
}

type hostileFile struct {
	*bytes.Reader
	operation string
}

func (file *hostileFile) Close() error {
	if file.operation == "close" {
		panic("hostile close value")
	}
	return nil
}

func (file *hostileFile) Read(buffer []byte) (int, error) {
	if file.operation == "read" {
		panic("hostile read value")
	}
	if file.operation == "negative read count" {
		return -1, nil
	}
	if file.operation == "oversized read count" {
		return len(buffer) + 1, nil
	}
	return file.Reader.Read(buffer)
}

func (file *hostileFile) Stat() (fs.FileInfo, error) {
	if file.operation == "stat" {
		panic("hostile stat value")
	}
	return hostileFileInfo{operation: file.operation}, nil
}

func (file *hostileFile) GenerationContext(context.Context) (string, error) {
	if file.operation == "generation" {
		panic("hostile generation value")
	}
	return "stable", nil
}

type hostileFileInfo struct{ operation string }

func (hostileFileInfo) Name() string { return "config.json" }

func (info hostileFileInfo) Size() int64 {
	if info.operation == "metadata size" {
		panic("hostile metadata size value")
	}
	return 5
}

func (info hostileFileInfo) Mode() fs.FileMode {
	if info.operation == "metadata mode" {
		panic("hostile metadata mode value")
	}
	return 0o600
}

func (info hostileFileInfo) ModTime() time.Time {
	if info.operation == "metadata modification time" {
		panic("hostile metadata modification time value")
	}
	return time.Unix(0, 0)
}

func (hostileFileInfo) IsDir() bool { return false }

func (info hostileFileInfo) Sys() any {
	if info.operation == "metadata system value" {
		panic("hostile metadata system value")
	}
	return nil
}

type hostileIsError struct{}

func (hostileIsError) Error() string { return "hostile classification error" }

func (hostileIsError) Is(error) bool { panic("hostile error classification value") }

type changingFS struct{}

func (changingFS) Open(string) (fs.File, error) {
	return &changingFile{Reader: bytes.NewReader([]byte("mixed"))}, nil
}

type changingFile struct {
	*bytes.Reader
	stats int
}

func (f *changingFile) Close() error { return nil }

func (f *changingFile) Stat() (fs.FileInfo, error) {
	f.stats++
	return changingFileInfo{size: int64(6 - f.stats)}, nil
}

type changingFileInfo struct{ size int64 }

func (changingFileInfo) Name() string       { return "config.json" }
func (info changingFileInfo) Size() int64   { return info.size }
func (changingFileInfo) Mode() fs.FileMode  { return 0o600 }
func (changingFileInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (changingFileInfo) IsDir() bool        { return false }
func (changingFileInfo) Sys() any           { return nil }

type statFailureFile struct {
	io.Reader
	calls  int
	failAt int
	nilAt  int
	err    error
}

func (*statFailureFile) Close() error { return nil }

func (f *statFailureFile) Stat() (fs.FileInfo, error) {
	f.calls++
	if f.calls == f.failAt {
		return nil, f.err
	}
	if f.calls == f.nilAt {
		return nil, nil
	}
	return changingFileInfo{size: 5}, nil
}

type generationChangingFile struct {
	io.Reader
	generation int
}

func (*generationChangingFile) Close() error { return nil }

func (*generationChangingFile) Stat() (fs.FileInfo, error) {
	return changingFileInfo{size: 5}, nil
}

func (f *generationChangingFile) GenerationContext(
	ctx context.Context,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.generation++
	return fmt.Sprintf("generation-%d", f.generation), nil
}

type zeroProgressReader struct{}

func (zeroProgressReader) Read([]byte) (int, error) { return 0, nil }

type contextualFS struct {
	opens  int
	file   *contextualFile
	cancel context.CancelFunc
}

func (*contextualFS) Open(string) (fs.File, error) {
	panic("non-contextual Open called")
}

func (filesystem *contextualFS) OpenContext(ctx context.Context, _ string) (fs.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filesystem.opens++
	filesystem.file = &contextualFile{
		Reader: bytes.NewReader([]byte("value")),
		cancel: filesystem.cancel,
	}
	return filesystem.file, nil
}

type contextualFile struct {
	*bytes.Reader
	reads  int
	stats  int
	closes int
	cancel context.CancelFunc
}

func (*contextualFile) Close() error { panic("non-contextual Close called") }

func (*contextualFile) Read([]byte) (int, error) {
	panic("non-contextual Read called")
}

func (*contextualFile) Stat() (fs.FileInfo, error) {
	panic("non-contextual Stat called")
}

func (file *contextualFile) ReadContext(ctx context.Context, buffer []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	file.reads++
	count, err := file.Reader.Read(buffer)
	if count > 0 && file.cancel != nil {
		file.cancel()
	}
	return count, err
}

func (file *contextualFile) StatContext(ctx context.Context) (fs.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file.stats++
	return changingFileInfo{size: 5}, nil
}

func (file *contextualFile) CloseContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file.closes++
	return nil
}

func (r *cancelReader) Read(buffer []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	buffer[0] = 'x'
	r.cancel()
	return 1, nil
}
