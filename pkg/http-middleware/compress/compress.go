// Package compress performs bounded gzip content-coding negotiation. Sensitive
// dynamic responses should opt out by setting Cache-Control: no-transform.
package compress

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/faustbrian/golib/pkg/http-middleware/internal/httpx"
)

// Policy configures bounded gzip response compression. ExcludedTypes accepts
// at most 64 exact media types of at most 256 bytes each.
type Policy struct {
	MinimumBytes, MaxBuffer int
	Level, MaxHeaderBytes   int
	ExcludedTypes           []string
}

// ErrInvalidPolicy identifies invalid compression policy configuration.
var ErrInvalidPolicy = errors.New("compress: invalid policy")

// ConfigError reports an invalid compression policy field.
type ConfigError struct{ Field string }

func (e *ConfigError) Error() string { return fmt.Sprintf("compress: invalid %s", e.Field) }
func (e *ConfigError) Unwrap() error { return ErrInvalidPolicy }

// New constructs gzip middleware. Responses are buffered up to MaxBuffer so
// eligibility can be decided before commitment. Larger eligible responses
// continue as gzip streams; other responses spill as identity.
func New(policy Policy) (func(http.Handler) http.Handler, error) {
	if policy.MinimumBytes == 0 {
		policy.MinimumBytes = 1024
	}
	if policy.MaxBuffer == 0 {
		policy.MaxBuffer = 1 << 20
	}
	if policy.Level == 0 {
		policy.Level = gzip.DefaultCompression
	}
	if policy.MaxHeaderBytes == 0 {
		policy.MaxHeaderBytes = 8192
	}
	if policy.MinimumBytes < 1 || policy.MaxBuffer < policy.MinimumBytes ||
		policy.MaxBuffer > 16<<20 || policy.Level < gzip.HuffmanOnly ||
		policy.Level > gzip.BestCompression || policy.MaxHeaderBytes < 1 ||
		policy.MaxHeaderBytes > 1<<20 || len(policy.ExcludedTypes) > 64 {
		return nil, &ConfigError{Field: "limit"}
	}
	excluded := make([]string, len(policy.ExcludedTypes))
	for index, value := range policy.ExcludedTypes {
		if len(value) > 256 {
			return nil, &ConfigError{Field: "excluded media type"}
		}
		mediaType, _, err := mime.ParseMediaType(value)
		if err != nil || !strings.Contains(mediaType, "/") || strings.Contains(mediaType, "*") {
			return nil, &ConfigError{Field: "excluded media type"}
		}
		excluded[index] = strings.ToLower(mediaType)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gzipQuality, identityQuality, ok := negotiate(r.Header.Values("Accept-Encoding"), policy.MaxHeaderBytes)
			httpx.AddVary(w.Header(), "Accept-Encoding")
			if !ok {
				httpx.SafeError(w, http.StatusNotAcceptable, "no acceptable content coding\n")
				return
			}
			buffered := newBuffer(w, policy.MaxBuffer)
			defer buffered.finish()
			buffered.compression = &streamPolicy{
				request: r, gzipQuality: gzipQuality, identityQuality: identityQuality,
				minimum: policy.MinimumBytes, excluded: excluded, level: policy.Level,
			}
			next.ServeHTTP(buffered, r)
			if buffered.spilled {
				return
			}
			if shouldCompress(r, buffered, gzipQuality, identityQuality, policy.MinimumBytes, excluded) {
				writeGzip(w, buffered, policy.Level)
				return
			}
			buffered.commitIdentity()
		})
	}, nil
}

type responseBuffer struct {
	destination http.ResponseWriter
	header      http.Header
	status      int
	buffer      bytes.Buffer
	maximum     int
	spilled     bool
	encoder     *gzip.Writer
	compression *streamPolicy
	committed   http.Header
	trailers    []string
	compressed  bool
}

type streamPolicy struct {
	request                      *http.Request
	gzipQuality, identityQuality float64
	minimum, level               int
	excluded                     []string
}

func newBuffer(destination http.ResponseWriter, maximum int) *responseBuffer {
	return &responseBuffer{destination: destination, header: destination.Header().Clone(), maximum: maximum}
}
func (w *responseBuffer) Header() http.Header {
	if w.spilled {
		return w.destination.Header()
	}
	return w.header
}
func (w *responseBuffer) WriteHeader(status int) {
	httpx.CheckWriteHeaderCode(status)
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		copyHeader(w.destination.Header(), w.header)
		w.destination.WriteHeader(status)
		return
	}
	if w.status == 0 {
		w.status = status
		w.commitHeader()
		if status == http.StatusSwitchingProtocols {
			w.commitIdentity()
		}
	}
}
func (w *responseBuffer) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
		w.commitHeader()
	}
	w.sniffContentType(payload)
	if w.spilled {
		if w.encoder != nil {
			return w.encoder.Write(payload)
		}
		return w.destination.Write(payload)
	}
	if w.buffer.Len()+len(payload) > w.maximum {
		if w.compression != nil && shouldCompressSize(
			w.compression.request,
			w,
			w.compression.gzipQuality,
			w.compression.identityQuality,
			w.compression.minimum,
			w.compression.excluded,
			w.buffer.Len()+len(payload),
		) {
			return w.startGzip(payload, w.compression.level)
		}
		w.commitIdentity()
		return w.destination.Write(payload)
	}
	return w.buffer.Write(payload)
}
func (w *responseBuffer) commitIdentity() {
	if w.spilled {
		return
	}
	w.spilled = true
	httpx.AddVary(w.header, "Accept-Encoding")
	w.commitHeader()
	httpx.AddVary(w.committed, "Accept-Encoding")
	copyHeader(w.destination.Header(), w.committed)
	w.destination.WriteHeader(statusOrOK(w.status))
	if w.buffer.Len() > 0 {
		_, _ = w.destination.Write(w.buffer.Bytes())
	}
	w.buffer.Reset()
}
func shouldCompress(r *http.Request, w *responseBuffer, gzipQ, identityQ float64, minimum int, excluded []string) bool {
	return shouldCompressSize(r, w, gzipQ, identityQ, minimum, excluded, w.buffer.Len())
}
func shouldCompressSize(r *http.Request, w *responseBuffer, gzipQ, identityQ float64, minimum int, excluded []string, size int) bool {
	status := statusOrOK(w.status)
	header := w.responseHeader()
	if gzipQ <= 0 || gzipQ < identityQ || r.Method == http.MethodHead || status < 200 || status == http.StatusNoContent || status == http.StatusNotModified || r.Header.Get("Range") != "" || header.Get("Content-Range") != "" || header.Get("Content-Encoding") != "" || strings.Contains(strings.ToLower(header.Get("Cache-Control")), "no-transform") || size < minimum {
		return false
	}
	mediaType, _, _ := mime.ParseMediaType(header.Get("Content-Type"))
	for _, excludedType := range excluded {
		if strings.EqualFold(mediaType, excludedType) {
			return false
		}
	}
	return true
}
func (w *responseBuffer) startGzip(payload []byte, level int) (int, error) {
	w.compressed = true
	header := compressedHeader(w.responseHeader())
	copyHeader(w.destination.Header(), header)
	w.destination.WriteHeader(statusOrOK(w.status))
	encoder, _ := gzip.NewWriterLevel(w.destination, level)
	w.spilled = true
	w.encoder = encoder
	if _, err := encoder.Write(w.buffer.Bytes()); err != nil {
		w.buffer.Reset()
		return 0, err
	}
	w.buffer.Reset()
	return encoder.Write(payload)
}
func (w *responseBuffer) closeEncoder() {
	if w.encoder != nil {
		_ = w.encoder.Close()
	}
}
func (w *responseBuffer) finish() {
	w.closeEncoder()
	if w.spilled {
		w.copyTrailers()
	}
}
func writeGzip(destination http.ResponseWriter, w *responseBuffer, level int) {
	w.compressed = true
	w.spilled = true
	w.commitHeader()
	header := compressedHeader(w.committed)
	copyHeader(destination.Header(), header)
	destination.WriteHeader(statusOrOK(w.status))
	encoder, _ := gzip.NewWriterLevel(destination, level)
	_, _ = io.Copy(encoder, &w.buffer)
	_ = encoder.Close()
}
func (w *responseBuffer) commitHeader() {
	if w.committed != nil {
		return
	}
	w.committed = w.header.Clone()
	w.trailers = declaredTrailers(w.committed)
	for _, name := range w.trailers {
		w.committed.Del(name)
	}
	for name := range w.committed {
		if strings.HasPrefix(name, http.TrailerPrefix) {
			w.committed.Del(name)
		}
	}
}
func (w *responseBuffer) responseHeader() http.Header {
	if w.committed != nil {
		return w.committed
	}
	return w.header
}
func (w *responseBuffer) sniffContentType(payload []byte) {
	if len(payload) == 0 || w.committed.Get("Content-Type") != "" || w.committed.Get("Content-Encoding") != "" {
		return
	}
	w.committed.Set("Content-Type", http.DetectContentType(payload[:min(len(payload), 512)]))
}
func (w *responseBuffer) copyTrailers() {
	for _, name := range w.trailers {
		if w.compressed && representationHeader(name) {
			continue
		}
		if values, exists := w.header[name]; exists {
			w.destination.Header()[name] = append([]string(nil), values...)
		}
	}
	for name, values := range w.header {
		if strings.HasPrefix(name, http.TrailerPrefix) {
			trailer := strings.TrimPrefix(name, http.TrailerPrefix)
			if !w.compressed || !representationHeader(trailer) {
				w.destination.Header()[name] = append([]string(nil), values...)
			}
		}
	}
}
func declaredTrailers(header http.Header) []string {
	var result []string
	for _, line := range header.Values("Trailer") {
		for _, name := range strings.Split(line, ",") {
			if name = http.CanonicalHeaderKey(strings.TrimSpace(name)); name != "" {
				result = append(result, name)
			}
		}
	}
	return result
}
func compressedHeader(source http.Header) http.Header {
	header := source.Clone()
	header.Set("Content-Encoding", "gzip")
	httpx.AddVary(header, "Accept-Encoding")
	for _, name := range []string{"Content-Length", "ETag", "Content-MD5", "Digest"} {
		header.Del(name)
	}
	removeRepresentationTrailers(header)
	return header
}
func removeRepresentationTrailers(header http.Header) {
	var retained []string
	for _, name := range declaredTrailers(header) {
		if !representationHeader(name) {
			retained = append(retained, name)
		}
	}
	header.Del("Trailer")
	if len(retained) > 0 {
		header.Set("Trailer", strings.Join(retained, ", "))
	}
}
func representationHeader(name string) bool {
	switch strings.ToLower(name) {
	case "content-length", "etag", "content-md5", "digest":
		return true
	default:
		return false
	}
}
func negotiate(lines []string, maxBytes int) (gzipQ, identityQ float64, ok bool) {
	identityQ = 1
	if len(lines) == 0 {
		return 0, identityQ, true
	}
	if len(lines) == 1 && lines[0] == "" {
		return 0, identityQ, true
	}
	wildcard := -1.0
	gzipSet := false
	identitySet := false
	remaining, items := maxBytes, 0
	for _, line := range lines {
		parts, valid := httpx.SplitDelimited(line, ',', remaining, 64-items)
		if !valid {
			return 0, 0, false
		}
		remaining -= len(line)
		items += len(parts)
		for _, part := range parts {
			fields, valid := httpx.SplitDelimited(part, ';', len(part), 8)
			if !valid {
				return 0, 0, false
			}
			coding := strings.ToLower(fields[0])
			q := 1.0
			qualitySeen := false
			for _, field := range fields[1:] {
				key, value, found := strings.Cut(strings.TrimSpace(field), "=")
				if !found || !strings.EqualFold(key, "q") || qualitySeen {
					return 0, 0, false
				}
				parsed, valid := httpx.ParseQuality(value)
				if !valid {
					return 0, 0, false
				}
				q = parsed
				qualitySeen = true
			}
			switch coding {
			case "gzip":
				gzipSet = true
				if q > gzipQ {
					gzipQ = q
				}
			case "identity":
				identityQ = q
				identitySet = true
			case "*":
				wildcard = q
			}
		}
	}
	if !gzipSet && wildcard >= 0 {
		gzipQ = wildcard
	}
	if !identitySet && wildcard == 0 {
		identityQ = 0
	}
	return gzipQ, identityQ, gzipQ > 0 || identityQ > 0
}
func copyHeader(destination, source http.Header) {
	for key := range destination {
		destination.Del(key)
	}
	for key, values := range source {
		destination[key] = append([]string(nil), values...)
	}
}
func statusOrOK(status int) int {
	if status == 0 {
		return http.StatusOK
	}
	return status
}
