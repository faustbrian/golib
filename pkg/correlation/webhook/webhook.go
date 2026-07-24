// Package webhook provides an explicit webhook-named HTTP correlation adapter.
package webhook

import (
	"net/http"

	correlation "github.com/faustbrian/golib/pkg/correlation"
	httpcorrelation "github.com/faustbrian/golib/pkg/correlation/http"
)

// Options configure webhook boundary trust and invalid input handling.
type Options = httpcorrelation.Options

// Adapter keeps webhook propagation explicit at send and receive call sites.
type Adapter struct{ middleware *httpcorrelation.Middleware }

// New constructs an explicit webhook correlation adapter.
func New(factory *correlation.Factory, options Options) (*Adapter, error) {
	middleware, err := httpcorrelation.New(factory, options)
	if err != nil {
		return nil, err
	}
	return &Adapter{middleware: middleware}, nil
}

// Send creates and injects a fresh outbound webhook request hop.
func (adapter *Adapter) Send(request *http.Request, parent correlation.Values) (correlation.Values, error) {
	return adapter.middleware.Inject(request, parent)
}

// Wrap receives a webhook through the configured explicit trust boundary.
func (adapter *Adapter) Wrap(next http.Handler) http.Handler {
	return adapter.middleware.Wrap(next)
}
