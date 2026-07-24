package tabular

import (
	"bufio"
	"encoding/csv"
	"errors"
	"io"
	"unicode/utf8"
)

const delimitedReadBufferSize = 64 * 1024

// DelimitedConfig explicitly controls delimited-text parsing behavior.
type DelimitedConfig struct {
	Delimiter           rune
	Comment             rune
	LazyQuotes          bool
	TrimLeadingSpace    bool
	AllowVariableFields bool
	FieldsPerRecord     int
	Header              *HeaderConfig
	Normalize           NormalizationConfig
}

// DelimitedReader streams records from CSV or another delimited text format.
type DelimitedReader struct {
	reader       *csv.Reader
	format       string
	header       Row
	headerConfig *HeaderConfig
	headerRead   bool
	headerErr    error
	normalize    NormalizationConfig
	row          int
}

// NewCSVReader constructs a comma-delimited streaming reader.
func NewCSVReader(source io.Reader, config DelimitedConfig) (*DelimitedReader, error) {
	config.Delimiter = ','
	reader, err := newDelimitedReader(source, config, "csv")
	if err != nil {
		return nil, err
	}
	return reader, nil
}

// NewDelimitedReader constructs a streaming reader for an explicit delimiter.
func NewDelimitedReader(source io.Reader, config DelimitedConfig) (*DelimitedReader, error) {
	return newDelimitedReader(source, config, "delimited")
}

func newDelimitedReader(source io.Reader, config DelimitedConfig, format string) (*DelimitedReader, error) {
	if source == nil || !validDelimiter(config.Delimiter) ||
		(config.Comment != 0 && (!validDelimiter(config.Comment) || config.Comment == config.Delimiter)) ||
		config.FieldsPerRecord < 0 {
		return nil, &Error{Kind: ErrorInvalidConfig, Op: "delimited.new", Format: format}
	}

	parser := csv.NewReader(bufio.NewReaderSize(source, delimitedReadBufferSize))
	parser.Comma = config.Delimiter
	parser.Comment = config.Comment
	parser.LazyQuotes = config.LazyQuotes
	parser.TrimLeadingSpace = config.TrimLeadingSpace
	parser.FieldsPerRecord = config.FieldsPerRecord
	if config.AllowVariableFields {
		parser.FieldsPerRecord = -1
	}

	return &DelimitedReader{
		reader:       parser,
		format:       format,
		headerConfig: cloneHeaderConfig(config.Header),
		normalize:    config.Normalize,
	}, nil
}

// Header returns the normalized first row when header handling is configured.
// The returned row is a copy and is safe for the caller to modify.
func (reader *DelimitedReader) Header() (Row, error) {
	if reader.headerConfig == nil {
		return nil, nil
	}
	reader.readHeader()
	if reader.headerErr != nil {
		return nil, reader.headerErr
	}
	return append(Row(nil), reader.header...), nil
}

// Read returns the next normalized record. io.EOF marks a clean end of input.
func (reader *DelimitedReader) Read() (Row, error) {
	if reader.headerConfig != nil {
		reader.readHeader()
		if reader.headerErr != nil {
			return nil, reader.headerErr
		}
	}

	record, err := reader.readRecord()
	if err != nil {
		return nil, err
	}
	if !reader.normalize.TrimSpace && reader.normalize.EmptyAs == "" {
		return record, nil
	}
	return NormalizeRow(record, reader.normalize), nil
}

func (reader *DelimitedReader) readHeader() {
	if reader.headerRead {
		return
	}
	reader.headerRead = true

	header, err := reader.readRecord()
	if err != nil {
		if errors.Is(err, io.EOF) {
			reader.headerErr = &Error{
				Kind:   ErrorInvalidHeader,
				Op:     "delimited.header",
				Format: reader.format,
				Err:    io.EOF,
			}
			return
		}
		reader.headerErr = err
		return
	}
	reader.header, reader.headerErr = NormalizeHeader(header, *reader.headerConfig)
}

func (reader *DelimitedReader) readRecord() (Row, error) {
	record, err := reader.reader.Read()
	if err == nil {
		reader.row++
		return Row(record), nil
	}
	if errors.Is(err, io.EOF) {
		return nil, io.EOF
	}

	row := reader.row + 1
	var parseErr *csv.ParseError
	if errors.As(err, &parseErr) && parseErr.Line > 0 {
		row = parseErr.Line
	}
	return nil, &Error{
		Kind:   ErrorMalformedRow,
		Op:     "delimited.read",
		Format: reader.format,
		Row:    row,
		Err:    err,
	}
}

func validDelimiter(delimiter rune) bool {
	return delimiter != 0 && delimiter != '"' && delimiter != '\r' &&
		delimiter != '\n' && delimiter != utf8.RuneError
}

func cloneHeaderConfig(config *HeaderConfig) *HeaderConfig {
	if config == nil {
		return nil
	}
	cloned := *config
	if config.Replace != nil {
		cloned.Replace = make(map[string]string, len(config.Replace))
		for original, replacement := range config.Replace {
			cloned.Replace[original] = replacement
		}
	}
	return &cloned
}
