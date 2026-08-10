package httpsignature

import (
	"errors"

	"github.com/dunglas/httpsfv"
)

var errStructuredFieldParse = errors.New("structured field parser rejected input")

// The Structured Fields dependency has historically panicked on some malformed
// extension syntax. These narrow boundaries convert dependency panics from
// untrusted HTTP fields into ordinary parse failures without hiding panics in
// this package's own semantic validation.
func unmarshalStructuredDictionary(values []string) (dictionary *httpsfv.Dictionary, err error) {
	defer func() {
		if recover() != nil {
			dictionary, err = nil, errStructuredFieldParse
		}
	}()
	return httpsfv.UnmarshalDictionary(values)
}

func unmarshalStructuredList(values []string) (list httpsfv.List, err error) {
	defer func() {
		if recover() != nil {
			list, err = nil, errStructuredFieldParse
		}
	}()
	return httpsfv.UnmarshalList(values)
}

func unmarshalStructuredItem(values []string) (item httpsfv.Item, err error) {
	defer func() {
		if recover() != nil {
			item, err = httpsfv.Item{}, errStructuredFieldParse
		}
	}()
	return httpsfv.UnmarshalItem(values)
}
