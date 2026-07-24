package media

import (
	"bytes"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"

	"golang.org/x/net/http/httpguts"
)

var (
	// ErrInvalidMultipartEncoding reports invalid multipart parts or bounds.
	ErrInvalidMultipartEncoding = errors.New("invalid multipart encoding")
	// ErrMultipartEncodingLimit reports a part-count or output-size bound.
	ErrMultipartEncodingLimit = errors.New("multipart encoding limit exceeded")
	errMultipartWriteLimit    = errors.New("multipart write limit")
)

// MultipartHeader is one additional part header. Content-Disposition and
// Content-Type are represented by dedicated MultipartPart fields.
type MultipartHeader struct {
	Name  string
	Value string
}

// MultipartPart is one already serialized multipart/form-data value.
// Repeated names and caller order are preserved.
type MultipartPart struct {
	Name        string
	ContentType string
	Headers     []MultipartHeader
	Data        []byte
}

// MultipartFormDataEncode writes a deterministic multipart/form-data body
// using a caller-owned boundary. It performs no content sniffing or I/O.
func MultipartFormDataEncode(
	parts []MultipartPart,
	boundary string,
	maxParts int,
	maxBytes int,
) ([]byte, string, error) {
	return multipartNamedEncode(
		parts, boundary, maxParts, maxBytes, "multipart/form-data",
	)
}

// MultipartMixedEncode writes a deterministic multipart/mixed body whose
// parts use Content-Disposition: form-data with a name parameter. This is the
// optional named-part convention described by OpenAPI 3.0 and 3.1.
func MultipartMixedEncode(
	parts []MultipartPart,
	boundary string,
	maxParts int,
	maxBytes int,
) ([]byte, string, error) {
	return multipartNamedEncode(
		parts, boundary, maxParts, maxBytes, "multipart/mixed",
	)
}

func multipartNamedEncode(
	parts []MultipartPart,
	boundary string,
	maxParts int,
	maxBytes int,
	mediaType string,
) ([]byte, string, error) {
	if maxParts < 1 || maxBytes < 1 || boundary == "" {
		return nil, "", ErrInvalidMultipartEncoding
	}
	if len(parts) > maxParts {
		return nil, "", ErrMultipartEncodingLimit
	}

	output := &multipartBoundedBuffer{maximum: maxBytes}
	writer := multipart.NewWriter(output)
	if err := writer.SetBoundary(boundary); err != nil {
		return nil, "", fmt.Errorf("%w: boundary: %v", ErrInvalidMultipartEncoding, err)
	}
	for _, part := range parts {
		headers, err := multipartPartHeaders(part, maxBytes)
		if err != nil {
			return nil, "", err
		}
		partWriter, err := writer.CreatePart(headers)
		if err != nil {
			return nil, "", ErrMultipartEncodingLimit
		}
		if _, err = partWriter.Write(part.Data); err != nil {
			return nil, "", ErrMultipartEncodingLimit
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", ErrMultipartEncodingLimit
	}
	contentType := mime.FormatMediaType(
		mediaType, map[string]string{"boundary": boundary},
	)
	return bytes.Clone(output.Bytes()), contentType, nil
}

func multipartPartHeaders(
	part MultipartPart,
	maxBytes int,
) (textproto.MIMEHeader, error) {
	disposition, err := MultipartFormDataDisposition(part.Name, maxBytes)
	if err != nil {
		if errors.Is(err, ErrMultipartDispositionLimit) {
			return nil, ErrMultipartEncodingLimit
		}
		return nil, fmt.Errorf("%w: part name: %v", ErrInvalidMultipartEncoding, err)
	}
	headers := textproto.MIMEHeader{"Content-Disposition": {disposition}}
	if part.ContentType != "" {
		mediaType, parameters, parseErr := mime.ParseMediaType(part.ContentType)
		if parseErr != nil || !strings.Contains(mediaType, "/") ||
			strings.Contains(mediaType, "*") {
			return nil, ErrInvalidMultipartEncoding
		}
		headers.Set("Content-Type", mime.FormatMediaType(mediaType, parameters))
	}
	for _, header := range part.Headers {
		if !httpguts.ValidHeaderFieldName(header.Name) ||
			!httpguts.ValidHeaderFieldValue(header.Value) ||
			strings.EqualFold(header.Name, "Content-Disposition") ||
			strings.EqualFold(header.Name, "Content-Type") {
			return nil, ErrInvalidMultipartEncoding
		}
		headers.Add(header.Name, header.Value)
	}
	return headers, nil
}

type multipartBoundedBuffer struct {
	bytes.Buffer
	maximum int
}

func (buffer *multipartBoundedBuffer) Write(value []byte) (int, error) {
	if len(value) > buffer.maximum-buffer.Len() {
		return 0, errMultipartWriteLimit
	}
	return buffer.Buffer.Write(value)
}
