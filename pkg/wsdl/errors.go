package wsdl

import "errors"

var (
	// ErrLimitExceeded identifies input or graph work beyond a configured bound.
	ErrLimitExceeded = errors.New("wsdl: resource limit exceeded")
	// ErrDTDForbidden identifies an XML DTD or directive in a WSDL input.
	ErrDTDForbidden = errors.New("wsdl: DTD and XML directives are forbidden")
	// ErrDuplicateSymbol identifies a repeated name in one WSDL symbol space.
	ErrDuplicateSymbol = errors.New("wsdl: duplicate symbol")
)
