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
	"sync"
	"testing"
	"time"
)

func TestResumeDownloadToFileAppendsValidatedRangeAndPublishesAtomically(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	destination := filepath.Join(directory, "artifact.bin")
	partial := destination + ".partial"
	if err := os.WriteFile(partial, []byte("hello "), 0o600); err != nil {
		t.Fatalf("seed partial: %v", err)
	}
	content := []byte("hello world")
	digest := sha256.Sum256(content)
	var progress []TransferProgress
	doer := httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Range") != "bytes=6-" || request.Header.Get("If-Range") != `"v1"` {
			t.Fatalf("resume headers = %#v", request.Header)
		}
		return &http.Response{
			StatusCode: http.StatusPartialContent,
			Header: http.Header{
				"Content-Range": {"bytes 6-10/11"}, "Etag": {`"v1"`},
			},
			Body: io.NopCloser(strings.NewReader("world")), ContentLength: 5,
			Request: request,
		}, nil
	})
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test/artifact", nil)
	result, err := ResumeDownloadToFile(context.Background(), doer, request, destination, ResumeFileOptions{
		Validator: RangeValidator{ETag: `"v1"`},
		Transfer: TransferOptions{
			MaximumBytes: 64, ExpectedBytes: 11,
			DigestAlgorithm: DigestSHA256, ExpectedDigest: digest[:],
			Progress: func(_ context.Context, update TransferProgress) error {
				progress = append(progress, update)
				return nil
			},
		},
	})
	if err != nil {
		t.Fatalf("resume download: %v", err)
	}
	stored, readErr := os.ReadFile(destination)
	if readErr != nil || !bytes.Equal(stored, content) || result.Bytes != 11 {
		t.Fatalf("stored = %q, result %#v, %v", stored, result, readErr)
	}
	if _, statErr := os.Stat(partial); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partial still exists: %v", statErr)
	}
	if len(progress) != 2 || progress[0].Bytes != 6 || progress[0].Complete ||
		progress[1].Bytes != 11 || !progress[1].Complete ||
		!bytes.Equal(progress[1].Digest, digest[:]) {
		t.Fatalf("resume progress = %#v", progress)
	}
}

func TestResumeDownloadToFileRestartsWhenServerReturnsFullResponse(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	destination := filepath.Join(directory, "artifact.bin")
	partial := destination + ".partial"
	if err := os.WriteFile(partial, []byte("obsolete partial"), 0o600); err != nil {
		t.Fatalf("seed partial: %v", err)
	}
	doer := httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("replacement")), ContentLength: 11,
			Request: request,
		}, nil
	})
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test/artifact", nil)
	_, err := ResumeDownloadToFile(context.Background(), doer, request, destination, ResumeFileOptions{
		Validator: RangeValidator{ETag: `"old"`},
		Transfer:  TransferOptions{MaximumBytes: 64, ExpectedBytes: 11},
	})
	if err != nil {
		t.Fatalf("restart download: %v", err)
	}
	stored, _ := os.ReadFile(destination)
	if string(stored) != "replacement" {
		t.Fatalf("restarted destination = %q", stored)
	}
}

func TestResumeDownloadToFileRollsBackRejectedAppend(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		header http.Header
		body   string
		want   error
	}{
		{
			name:   "validator mismatch",
			header: http.Header{"Content-Range": {"bytes 4-6/7"}, "Etag": {`"other"`}},
			body:   "new", want: ErrRangeValidatorMismatch,
		},
		{
			name:   "digest mismatch",
			header: http.Header{"Content-Range": {"bytes 4-6/7"}, "Etag": {`"v1"`}},
			body:   "new", want: ErrDigestMismatch,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			destination := filepath.Join(directory, "artifact.bin")
			partial := destination + ".partial"
			if err := os.WriteFile(partial, []byte("safe"), 0o600); err != nil {
				t.Fatalf("seed partial: %v", err)
			}
			doer := httpDoerFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusPartialContent, Header: test.header,
					Body: io.NopCloser(strings.NewReader(test.body)), ContentLength: 3,
					Request: request,
				}, nil
			})
			request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test/artifact", nil)
			options := ResumeFileOptions{
				Validator: RangeValidator{ETag: `"v1"`},
				Transfer:  TransferOptions{MaximumBytes: 64, ExpectedBytes: 7},
			}
			if test.want == ErrDigestMismatch {
				options.Transfer.DigestAlgorithm = DigestSHA256
				options.Transfer.ExpectedDigest = make([]byte, sha256.Size)
			}
			_, err := ResumeDownloadToFile(context.Background(), doer, request, destination, options)
			if !errors.Is(err, test.want) {
				t.Fatalf("resume error = %v, want %v", err, test.want)
			}
			stored, readErr := os.ReadFile(partial)
			if readErr != nil || string(stored) != "safe" {
				t.Fatalf("rolled back partial = %q, %v", stored, readErr)
			}
		})
	}
}

func TestResumeDownloadToFilePublishesAlreadyCompletePartial(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	destination := filepath.Join(directory, "artifact.bin")
	partial := destination + ".partial"
	if err := os.WriteFile(partial, []byte("done"), 0o600); err != nil {
		t.Fatalf("seed partial: %v", err)
	}
	doer := httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusRequestedRangeNotSatisfiable,
			Header:     http.Header{"Content-Range": {"bytes */4"}},
			Body:       http.NoBody, ContentLength: 0, Request: request,
		}, nil
	})
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test/artifact", nil)
	result, err := ResumeDownloadToFile(context.Background(), doer, request, destination, ResumeFileOptions{
		Validator: RangeValidator{ETag: `"v1"`},
		Transfer:  TransferOptions{MaximumBytes: 64, ExpectedBytes: 4},
	})
	if err != nil || result.Bytes != 4 {
		t.Fatalf("complete partial result = %#v, %v", result, err)
	}
	stored, _ := os.ReadFile(destination)
	if string(stored) != "done" {
		t.Fatalf("published complete partial = %q", stored)
	}
}

func TestResumeDownloadFilesystemAndProtocolFailureBoundaries(t *testing.T) {
	t.Parallel()

	failure := errors.New("resume failure")
	if (&ResumeError{Operation: "test"}).Error() == "" ||
		!errors.Is(&ResumeError{Cause: failure}, failure) {
		t.Fatal("resume error contract failed")
	}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test/artifact", nil)
	validOptions := ResumeFileOptions{
		Validator: RangeValidator{ETag: `"v1"`},
		Transfer:  TransferOptions{MaximumBytes: 64, ExpectedBytes: 7},
	}
	validResponse := func(request *http.Request) *http.Response {
		return &http.Response{
			StatusCode: http.StatusPartialContent,
			Header:     http.Header{"Content-Range": {"bytes 4-6/7"}, "Etag": {`"v1"`}},
			Body:       io.NopCloser(strings.NewReader("new")), ContentLength: 3,
			Request: request,
		}
	}
	var nilContext context.Context
	if _, err := resumeDownloadToFile(nilContext, nil, nil, "", ResumeFileOptions{}, &fakeResumeFS{}); !errors.Is(err, ErrInvalidTransfer) {
		t.Fatalf("invalid configuration error = %v", err)
	}
	for _, options := range []ResumeFileOptions{
		{Mode: 0o1000},
		{PartialPath: "other/partial"},
		{PartialPath: "destination"},
	} {
		if _, err := resumeDownloadToFile(context.Background(), httpDoerFunc(func(*http.Request) (*http.Response, error) {
			return nil, failure
		}), request, "destination", options, &fakeResumeFS{}); err == nil {
			t.Fatalf("invalid options succeeded: %#v", options)
		}
	}

	for _, test := range []struct {
		name       string
		file       *fakeResumeFile
		filesystem *fakeResumeFS
		doer       HTTPDoer
		request    *http.Request
		options    ResumeFileOptions
	}{
		{name: "open", filesystem: &fakeResumeFS{openErr: failure}},
		{name: "mode", file: newFakeResumeFile("safe"), filesystem: &fakeResumeFS{}, options: validOptions},
		{name: "stat", file: newFakeResumeFile("safe"), filesystem: &fakeResumeFS{}, options: validOptions},
		{name: "validator", file: newFakeResumeFile("safe"), filesystem: &fakeResumeFS{}},
		{name: "request", file: newFakeResumeFile(""), filesystem: &fakeResumeFS{}, request: mustResumeRequest(t, http.MethodPost), options: validOptions},
		{name: "doer", file: newFakeResumeFile("safe"), filesystem: &fakeResumeFS{}, options: validOptions,
			doer: httpDoerFunc(func(*http.Request) (*http.Response, error) { return nil, failure })},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.file == nil {
				test.file = newFakeResumeFile("safe")
			}
			test.filesystem.file = test.file
			switch test.name {
			case "mode":
				test.file.chmodErr = failure
			case "stat":
				test.file.statErr = failure
			}
			if test.doer == nil {
				test.doer = httpDoerFunc(func(request *http.Request) (*http.Response, error) {
					return validResponse(request), nil
				})
			}
			if test.request == nil {
				test.request = request
			}
			_, err := resumeDownloadToFile(context.Background(), test.doer, test.request, "destination", test.options, test.filesystem)
			if err == nil {
				t.Fatal("resume failure path succeeded")
			}
		})
	}

	if closeResumeResponse(nil) != nil || wrapResumeError("test", nil) != nil {
		t.Fatal("nil resume errors were wrapped")
	}
	file := newFakeResumeFile("body")
	file.seekErr = failure
	if _, err := validateCompletePartial(context.Background(), file, TransferOptions{MaximumBytes: 64}); !errors.Is(err, failure) {
		t.Fatalf("validation seek error = %v", err)
	}
	file = newFakeResumeFile("body")
	file.statErr = failure
	if _, err := validateCompletePartial(context.Background(), file, TransferOptions{MaximumBytes: 64}); !errors.Is(err, failure) {
		t.Fatalf("validation stat error = %v", err)
	}
	file = newFakeResumeFile("body")
	file.truncateErr = failure
	if err := rollbackPartial(file, 2); !errors.Is(err, failure) {
		t.Fatalf("rollback truncate error = %v", err)
	}
	file = newFakeResumeFile("body")
	file.seekErr = failure
	if err := rollbackPartial(file, 2); !errors.Is(err, failure) {
		t.Fatalf("rollback seek error = %v", err)
	}

	continueResponse := func(request *http.Request, body io.ReadCloser) *http.Response {
		return &http.Response{
			StatusCode: http.StatusPartialContent,
			Header:     http.Header{"Content-Range": {"bytes 4-6/7"}, "Etag": {`"v1"`}},
			Body:       body, ContentLength: 3, Request: request,
		}
	}
	for _, test := range []struct {
		name       string
		file       *fakeResumeFile
		filesystem *fakeResumeFS
		doer       HTTPDoer
		options    ResumeFileOptions
	}{
		{name: "append seek", file: &fakeResumeFile{content: []byte("safe"), seekErr: failure}, filesystem: &fakeResumeFS{}, options: validOptions},
		{name: "append maximum", file: newFakeResumeFile("safe"), filesystem: &fakeResumeFS{}, options: ResumeFileOptions{
			Validator: RangeValidator{ETag: `"v1"`}, Transfer: TransferOptions{MaximumBytes: 4, ExpectedBytes: 7},
		}},
		{name: "append copy", file: newFakeResumeFile("safe"), filesystem: &fakeResumeFS{}, options: validOptions,
			doer: httpDoerFunc(func(request *http.Request) (*http.Response, error) {
				return continueResponse(request, &compressionErrorBody{Reader: &responseErrorReader{err: failure}}), nil
			})},
		{name: "restart truncate", file: &fakeResumeFile{content: []byte("safe"), truncateErr: failure}, filesystem: &fakeResumeFS{}, options: validOptions,
			doer: fullResumeDoer("replacement")},
		{name: "restart seek", file: &fakeResumeFile{content: []byte("safe"), seekErr: failure}, filesystem: &fakeResumeFS{}, options: validOptions,
			doer: fullResumeDoer("replacement")},
		{name: "restart copy", file: newFakeResumeFile("safe"), filesystem: &fakeResumeFS{}, options: validOptions,
			doer: httpDoerFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
					Body: &compressionErrorBody{Reader: &responseErrorReader{err: failure}}, ContentLength: -1, Request: request}, nil
			})},
		{name: "sync", file: &fakeResumeFile{content: []byte("safe"), syncErr: failure}, filesystem: &fakeResumeFS{}, options: validOptions,
			doer: fullResumeDoer("replace")},
		{name: "close", file: &fakeResumeFile{content: []byte("safe"), closeErr: failure}, filesystem: &fakeResumeFS{}, options: validOptions,
			doer: fullResumeDoer("replace")},
		{name: "rename", file: newFakeResumeFile("safe"), filesystem: &fakeResumeFS{renameErr: failure}, options: validOptions,
			doer: fullResumeDoer("replace")},
		{name: "directory open", file: newFakeResumeFile("safe"), filesystem: &fakeResumeFS{directoryErr: failure}, options: validOptions,
			doer: fullResumeDoer("replace")},
		{name: "directory sync", file: newFakeResumeFile("safe"), filesystem: &fakeResumeFS{directory: &fakeTransferDirectory{syncErr: failure}}, options: validOptions,
			doer: fullResumeDoer("replace")},
		{name: "directory close", file: newFakeResumeFile("safe"), filesystem: &fakeResumeFS{directory: &fakeTransferDirectory{closeErr: failure}}, options: validOptions,
			doer: fullResumeDoer("replace")},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.filesystem.file = test.file
			if test.doer == nil {
				test.doer = httpDoerFunc(func(request *http.Request) (*http.Response, error) {
					return continueResponse(request, io.NopCloser(strings.NewReader("new"))), nil
				})
			}
			_, err := resumeDownloadToFile(context.Background(), test.doer, request, "destination", test.options, test.filesystem)
			if err == nil {
				t.Fatal("resume operation failure succeeded")
			}
		})
	}
	defaultMaximumFile := newFakeResumeFile("safe")
	defaultMaximumOptions := validOptions
	defaultMaximumOptions.Transfer.MaximumBytes = 0
	if _, err := resumeDownloadToFile(context.Background(), httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		return continueResponse(request, io.NopCloser(strings.NewReader("new"))), nil
	}), request, "destination", defaultMaximumOptions, &fakeResumeFS{file: defaultMaximumFile}); err != nil {
		t.Fatalf("default maximum resume error = %v", err)
	}

	for _, test := range []struct {
		name     string
		body     io.ReadCloser
		expected int64
		want     error
	}{
		{name: "complete close", body: &compressionErrorBody{Reader: strings.NewReader(""), closeErr: failure}, expected: 4, want: failure},
		{name: "complete validation", body: http.NoBody, expected: 5, want: ErrTransferLength},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := newFakeResumeFile("safe")
			doer := httpDoerFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusRequestedRangeNotSatisfiable,
					Header:     http.Header{"Content-Range": {"bytes */4"}},
					Body:       test.body, ContentLength: 0, Request: request,
				}, nil
			})
			options := validOptions
			options.Transfer.ExpectedBytes = test.expected
			_, err := resumeDownloadToFile(context.Background(), doer, request, "destination", options, &fakeResumeFS{file: file})
			if !errors.Is(err, test.want) {
				t.Fatalf("complete resume error = %v, want %v", err, test.want)
			}
		})
	}
	for _, test := range []struct {
		name      string
		total     string
		failCall  int
		wantError bool
	}{
		{name: "unknown total", total: "*"},
		{name: "initial progress", total: "7", failCall: 1, wantError: true},
		{name: "final progress", total: "7", failCall: 2, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := newFakeResumeFile("safe")
			calls := 0
			options := validOptions
			options.Transfer.Progress = func(context.Context, TransferProgress) error {
				calls++
				if calls == test.failCall {
					return failure
				}
				return nil
			}
			doer := httpDoerFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusPartialContent,
					Header:     http.Header{"Content-Range": {"bytes 4-6/" + test.total}, "Etag": {`"v1"`}},
					Body:       io.NopCloser(strings.NewReader("new")), ContentLength: 3, Request: request,
				}, nil
			})
			_, err := resumeDownloadToFile(context.Background(), doer, request, "destination", options, &fakeResumeFS{file: file})
			if test.wantError && !errors.Is(err, failure) {
				t.Fatalf("progress error = %v", err)
			}
			if !test.wantError && err != nil {
				t.Fatalf("unknown-total resume error = %v", err)
			}
		})
	}
}

type httpDoerFunc func(*http.Request) (*http.Response, error)

func (do httpDoerFunc) Do(request *http.Request) (*http.Response, error) { return do(request) }

func mustResumeRequest(t *testing.T, method string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), method, "https://example.test", nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	return request
}

func fullResumeDoer(content string) HTTPDoer {
	return httpDoerFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(content)), ContentLength: int64(len(content)),
			Request: request,
		}, nil
	})
}

type fakeResumeFile struct {
	mu          sync.Mutex
	content     []byte
	offset      int64
	chmodErr    error
	statErr     error
	seekErr     error
	truncateErr error
	syncErr     error
	closeErr    error
}

func newFakeResumeFile(content string) *fakeResumeFile {
	return &fakeResumeFile{content: []byte(content)}
}

func (file *fakeResumeFile) Read(buffer []byte) (int, error) {
	file.mu.Lock()
	defer file.mu.Unlock()
	if file.offset >= int64(len(file.content)) {
		return 0, io.EOF
	}
	count := copy(buffer, file.content[file.offset:])
	file.offset += int64(count)
	return count, nil
}

func (file *fakeResumeFile) Write(buffer []byte) (int, error) {
	file.mu.Lock()
	defer file.mu.Unlock()
	end := file.offset + int64(len(buffer))
	if end > int64(len(file.content)) {
		file.content = append(file.content, make([]byte, end-int64(len(file.content)))...)
	}
	copy(file.content[file.offset:end], buffer)
	file.offset = end
	return len(buffer), nil
}

func (file *fakeResumeFile) Seek(offset int64, whence int) (int64, error) {
	if file.seekErr != nil {
		return 0, file.seekErr
	}
	switch whence {
	case io.SeekStart:
		file.offset = offset
	case io.SeekCurrent:
		file.offset += offset
	case io.SeekEnd:
		file.offset = int64(len(file.content)) + offset
	}
	return file.offset, nil
}

func (file *fakeResumeFile) Chmod(os.FileMode) error { return file.chmodErr }
func (file *fakeResumeFile) Stat() (os.FileInfo, error) {
	if file.statErr != nil {
		return nil, file.statErr
	}
	return fakeResumeInfo{size: int64(len(file.content))}, nil
}
func (file *fakeResumeFile) Truncate(size int64) error {
	if file.truncateErr != nil {
		return file.truncateErr
	}
	file.content = file.content[:size]
	if file.offset > size {
		file.offset = size
	}
	return nil
}
func (file *fakeResumeFile) Sync() error  { return file.syncErr }
func (file *fakeResumeFile) Close() error { return file.closeErr }

type fakeResumeInfo struct{ size int64 }

func (information fakeResumeInfo) Name() string { return "partial" }
func (information fakeResumeInfo) Size() int64  { return information.size }
func (fakeResumeInfo) Mode() os.FileMode        { return 0o600 }
func (fakeResumeInfo) ModTime() time.Time       { return time.Time{} }
func (fakeResumeInfo) IsDir() bool              { return false }
func (fakeResumeInfo) Sys() any                 { return nil }

type fakeResumeFS struct {
	file         *fakeResumeFile
	directory    *fakeTransferDirectory
	openErr      error
	renameErr    error
	directoryErr error
}

func (filesystem *fakeResumeFS) OpenFile(string, int, os.FileMode) (resumeFile, error) {
	if filesystem.openErr != nil {
		return nil, filesystem.openErr
	}
	return filesystem.file, nil
}
func (filesystem *fakeResumeFS) Rename(string, string) error { return filesystem.renameErr }
func (filesystem *fakeResumeFS) OpenDirectory(string) (fileTransferDirectory, error) {
	if filesystem.directoryErr != nil {
		return nil, filesystem.directoryErr
	}
	if filesystem.directory == nil {
		filesystem.directory = &fakeTransferDirectory{}
	}
	return filesystem.directory, nil
}
