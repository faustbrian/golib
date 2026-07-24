package wsdl

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"

	"github.com/faustbrian/golib/pkg/wire/xmlwire"
)

const (
	namespace11            = "http://schemas.xmlsoap.org/wsdl/"
	namespace20            = "http://www.w3.org/ns/wsdl"
	defaultMaxDocumentSize = 8 << 20
	defaultMaxDepth        = 256
	defaultMaxElements     = 100000
	defaultMaxAttributes   = 1000000
	defaultMaxTextBytes    = 8 << 20
	defaultMaxSchemas      = 64
	defaultMaxImports      = 4096
	defaultMaxOperations   = 10000
	defaultMaxBindings     = 10000
	defaultMaxEndpoints    = 10000
	defaultMaxExtensions   = 10000
)

var decoderToken = func(decoder *xml.Decoder) (xml.Token, error) {
	return decoder.Token()
}

// ParseOptions controls bounded parsing of one caller-supplied document.
type ParseOptions struct {
	MaxDocumentBytes int64
	MaxDepth         int
	MaxElements      int
	MaxAttributes    int
	MaxTextBytes     int64
	MaxSchemas       int
	MaxImports       int
	MaxOperations    int
	MaxBindings      int
	MaxEndpoints     int
	MaxExtensions    int
	SystemID         string
}

// Parse decodes one WSDL document without loading external resources.
func Parse(ctx context.Context, source []byte, options ParseOptions) (*Document, error) {
	limit := options.MaxDocumentBytes
	if limit < 0 || options.MaxDepth < 0 || options.MaxElements < 0 ||
		options.MaxAttributes < 0 || options.MaxTextBytes < 0 ||
		options.MaxSchemas < 0 || options.MaxImports < 0 ||
		options.MaxOperations < 0 || options.MaxBindings < 0 ||
		options.MaxEndpoints < 0 || options.MaxExtensions < 0 {
		return nil, errors.New("wsdl: parse limits must not be negative")
	}
	if limit == 0 {
		limit = defaultMaxDocumentSize
	}
	if options.MaxDepth == 0 {
		options.MaxDepth = defaultMaxDepth
	}
	if options.MaxElements == 0 {
		options.MaxElements = defaultMaxElements
	}
	if options.MaxAttributes == 0 {
		options.MaxAttributes = defaultMaxAttributes
	}
	if options.MaxTextBytes == 0 {
		options.MaxTextBytes = defaultMaxTextBytes
	}
	if options.MaxSchemas == 0 {
		options.MaxSchemas = defaultMaxSchemas
	}
	if options.MaxImports == 0 {
		options.MaxImports = defaultMaxImports
	}
	if options.MaxOperations == 0 {
		options.MaxOperations = defaultMaxOperations
	}
	if options.MaxBindings == 0 {
		options.MaxBindings = defaultMaxBindings
	}
	if options.MaxEndpoints == 0 {
		options.MaxEndpoints = defaultMaxEndpoints
	}
	if options.MaxExtensions == 0 {
		options.MaxExtensions = defaultMaxExtensions
	}
	if int64(len(source)) > limit {
		return nil, fmt.Errorf("%w: document bytes exceed %d", ErrLimitExceeded, limit)
	}

	if _, err := xmlwire.Root(source, xmlwire.DecodeOptions{
		MaxBytes: limit, MaxDepth: options.MaxDepth, CharsetReader: xmlwire.CharsetReader,
	}); err != nil {
		if errors.Is(err, xmlwire.ErrPayloadTooLarge) ||
			errors.Is(err, xmlwire.ErrNestingTooDeep) {
			return nil, fmt.Errorf("%w: %v", ErrLimitExceeded, err)
		}
		return nil, fmt.Errorf("wsdl: validate XML: %w", err)
	}

	state := parseState{ctx: ctx, options: options}
	decoder := xml.NewDecoder(&contextReader{ctx: ctx, reader: bytes.NewReader(source)})
	decoder.Strict = true
	decoder.Entity = map[string]string{}
	decoder.CharsetReader = xmlwire.CharsetReader
	for {
		token, err := decoderToken(decoder)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, errors.New("wsdl: document has no root element")
			}
			return nil, fmt.Errorf("wsdl: parse XML: %w", err)
		}
		switch value := token.(type) {
		case nil:
			return nil, errors.New("wsdl: parser returned a nil token")
		case xml.Directive:
			return nil, ErrDTDForbidden
		case xml.StartElement:
			switch value.Name {
			case xml.Name{Space: namespace11, Local: "definitions"}:
				return parseDefinitions11(decoder, value, &state)
			case xml.Name{Space: namespace20, Local: "description"}:
				return parseDescription20(decoder, value, &state)
			default:
				return nil, fmt.Errorf(
					"wsdl: unsupported root element {%s}%s",
					value.Name.Space,
					value.Name.Local,
				)
			}
		}
	}
}

func parseDefinitions11(
	decoder *xml.Decoder,
	start xml.StartElement,
	state *parseState,
) (*Document, error) {
	root, err := readXMLNode(decoder, start, state, 1)
	if err != nil {
		return nil, err
	}
	if err := validateCoreNCNames(root, NamespaceWSDL11); err != nil {
		return nil, err
	}
	if err := enforceComponentLimits(root, NamespaceWSDL11, state.options); err != nil {
		return nil, err
	}
	if err := assignBaseURIs(root, state.options.SystemID); err != nil {
		return nil, err
	}
	definitions, err := decodeDefinitions11(root, state)
	if err != nil {
		return nil, err
	}
	return &Document{
		version: Version11, definitions11: &definitions,
	}, nil
}

func parseDescription20(
	decoder *xml.Decoder,
	start xml.StartElement,
	state *parseState,
) (*Document, error) {
	root, err := readXMLNode(decoder, start, state, 1)
	if err != nil {
		return nil, err
	}
	if err := validateCoreNCNames(root, NamespaceWSDL20); err != nil {
		return nil, err
	}
	if err := enforceComponentLimits(root, NamespaceWSDL20, state.options); err != nil {
		return nil, err
	}
	if err := assignBaseURIs(root, state.options.SystemID); err != nil {
		return nil, err
	}
	description, err := decodeDescription20(state.ctx, root, state.options)
	if err != nil {
		return nil, err
	}
	return &Document{
		version:       Version20,
		description20: &description,
	}, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}
