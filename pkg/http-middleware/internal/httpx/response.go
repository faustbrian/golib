// Package httpx contains bounded response mechanics shared by middleware.
package httpx

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/felixge/httpsnoop"
)

// CheckWriteHeaderCode panics for status codes rejected by net/http.
func CheckWriteHeaderCode(status int) {
	if status < 100 || status > 999 {
		panic(fmt.Sprintf("invalid WriteHeader code %v", status))
	}
}

// Recorder tracks final status and payload bytes.
type Recorder struct {
	Status    int
	Bytes     int64
	Committed bool
}

// Track returns a writer with exactly the optional interfaces of writer.
func Track(writer http.ResponseWriter) (http.ResponseWriter, *Recorder) {
	recorder := &Recorder{}
	wrapped := httpsnoop.Wrap(writer, httpsnoop.Hooks{
		WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
			return func(status int) {
				next(status)
				if (status < 100 || status >= 200) && !recorder.Committed {
					recorder.Status = status
					recorder.Committed = true
				}
			}
		},
		Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			return func(payload []byte) (int, error) {
				written, err := next(payload)
				commitOK(recorder)
				recorder.Bytes += int64(written)
				return written, err
			}
		},
		ReadFrom: func(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
			return func(reader io.Reader) (int64, error) {
				written, err := next(reader)
				commitOK(recorder)
				recorder.Bytes += written
				return written, err
			}
		},
		Flush: func(next httpsnoop.FlushFunc) httpsnoop.FlushFunc {
			return func() { commitOK(recorder); next() }
		},
		Hijack: func(next httpsnoop.HijackFunc) httpsnoop.HijackFunc {
			return func() (net.Conn, *bufio.ReadWriter, error) {
				connection, buffered, err := next()
				if err == nil {
					recorder.Committed = true
				}
				return connection, buffered, err
			}
		},
	})
	return wrapped, recorder
}

func commitOK(recorder *Recorder) {
	if !recorder.Committed {
		recorder.Status = http.StatusOK
		recorder.Committed = true
	}
}

// WithPolicy preserves the exact optional-interface set and applies a header
// policy immediately before response commitment.
func WithPolicy(writer http.ResponseWriter, apply func(http.Header)) http.ResponseWriter {
	before := func() { apply(writer.Header()) }
	return httpsnoop.Wrap(writer, httpsnoop.Hooks{
		WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
			return func(status int) { before(); next(status) }
		},
		Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			return func(payload []byte) (int, error) { before(); return next(payload) }
		},
		ReadFrom: func(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
			return func(reader io.Reader) (int64, error) { before(); return next(reader) }
		},
		Flush: func(next httpsnoop.FlushFunc) httpsnoop.FlushFunc {
			return func() { before(); next() }
		},
		Hijack: func(next httpsnoop.HijackFunc) httpsnoop.HijackFunc {
			return func() (net.Conn, *bufio.ReadWriter, error) { before(); return next() }
		},
	})
}

// AddVary merges case-insensitive field names without replacing existing
// cache-key dimensions.
func AddVary(header http.Header, names ...string) {
	seen := make(map[string]bool)
	var values []string
	for _, line := range header.Values("Vary") {
		for _, value := range strings.Split(line, ",") {
			value = strings.TrimSpace(value)
			if value != "" && !seen[strings.ToLower(value)] {
				seen[strings.ToLower(value)] = true
				values = append(values, value)
			}
		}
	}
	for _, name := range names {
		if !seen[strings.ToLower(name)] {
			seen[strings.ToLower(name)] = true
			values = append(values, name)
		}
	}
	header.Del("Vary")
	if len(values) > 0 {
		header.Set("Vary", strings.Join(values, ", "))
	}
}

// SafeError emits a deterministic text response without internal details.
func SafeError(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}
