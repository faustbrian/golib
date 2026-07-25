package tabular

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

const defaultMaxRecordBytes = 1024 * 1024

// FixedWidthField identifies a half-open byte range [Start, End) in a source
// record. Offsets apply before character decoding.
type FixedWidthField struct {
	Name      string
	Start     int
	End       int
	TrimSpace bool
}

// FixedWidthConfig controls fixed-width record parsing.
type FixedWidthConfig struct {
	Fields              []FixedWidthField
	Encoding            Encoding
	AllowShortRecords   bool
	RejectTrailingBytes bool
	MaxRecordBytes      int
	Normalize           NormalizationConfig
}

// FixedWidthReader streams newline-delimited fixed-width records.
type FixedWidthReader struct {
	scanner             *bufio.Scanner
	fields              []FixedWidthField
	encoding            Encoding
	allowShortRecords   bool
	rejectTrailingBytes bool
	normalize           NormalizationConfig
	recordWidth         int
	row                 int
}

// NewFixedWidthReader validates a layout and constructs a streaming reader.
func NewFixedWidthReader(source io.Reader, config FixedWidthConfig) (*FixedWidthReader, error) {
	if source == nil || len(config.Fields) == 0 || config.MaxRecordBytes < 0 {
		return nil, &Error{Kind: ErrorInvalidLayout, Op: "fixedwidth.new", Format: "fixed-width"}
	}
	if config.Encoding == "" {
		config.Encoding = EncodingUTF8
	}
	if _, err := decoderFor(config.Encoding); err != nil {
		return nil, err
	}

	fields := append([]FixedWidthField(nil), config.Fields...)
	end := 0
	for index, field := range fields {
		if field.Name == "" || field.Start < 0 || field.End <= field.Start || field.Start < end {
			return nil, &Error{
				Kind:   ErrorInvalidLayout,
				Op:     "fixedwidth.new",
				Format: "fixed-width",
				Field:  index + 1,
			}
		}
		end = field.End
	}

	maxRecordBytes := config.MaxRecordBytes
	if maxRecordBytes == 0 {
		maxRecordBytes = defaultMaxRecordBytes
	}
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, min(64*1024, maxRecordBytes)), maxRecordBytes)

	return &FixedWidthReader{
		scanner:             scanner,
		fields:              fields,
		encoding:            config.Encoding,
		allowShortRecords:   config.AllowShortRecords,
		rejectTrailingBytes: config.RejectTrailingBytes,
		normalize:           config.Normalize,
		recordWidth:         end,
	}, nil
}

// Fields returns the configured field names in source order.
func (reader *FixedWidthReader) Fields() []string {
	names := make([]string, len(reader.fields))
	for index, field := range reader.fields {
		names[index] = field.Name
	}
	return names
}

// Read returns the next decoded row. io.EOF marks a clean end of input.
func (reader *FixedWidthReader) Read() (Row, error) {
	if !reader.scanner.Scan() {
		if err := reader.scanner.Err(); err != nil {
			return nil, &Error{
				Kind:   ErrorLimitExceeded,
				Op:     "fixedwidth.read",
				Format: "fixed-width",
				Row:    reader.row + 1,
				Err:    err,
			}
		}
		return nil, io.EOF
	}
	reader.row++
	record := reader.scanner.Bytes()
	if len(record) < reader.recordWidth && !reader.allowShortRecords {
		return nil, reader.rowError(ErrorMalformedRow, 0, errors.New("record is shorter than layout"))
	}
	if len(record) > reader.recordWidth && reader.rejectTrailingBytes {
		return nil, reader.rowError(ErrorMalformedRow, 0, errors.New("record has trailing bytes"))
	}

	row := make(Row, len(reader.fields))
	for index, field := range reader.fields {
		start, end := field.Start, field.End
		if start >= len(record) {
			start, end = len(record), len(record)
		} else if end > len(record) {
			end = len(record)
		}
		value, err := DecodeBytes(record[start:end], reader.encoding)
		if err != nil {
			return nil, reader.rowError(ErrorInvalidEncoding, index+1, err)
		}
		if field.TrimSpace {
			value = strings.TrimSpace(value)
		}
		row[index] = value
	}
	return NormalizeRow(row, reader.normalize), nil
}

func (reader *FixedWidthReader) rowError(kind ErrorKind, field int, cause error) error {
	return &Error{
		Kind:   kind,
		Op:     "fixedwidth.read",
		Format: "fixed-width",
		Row:    reader.row,
		Field:  field,
		Err:    cause,
	}
}

// ExtractBytes returns the requested half-open byte range without copying it.
func ExtractBytes(record []byte, start, end int) ([]byte, error) {
	if start < 0 || end <= start || end > len(record) {
		return nil, &Error{Kind: ErrorInvalidLayout, Op: "fixedwidth.extract", Format: "fixed-width"}
	}
	return record[start:end], nil
}
