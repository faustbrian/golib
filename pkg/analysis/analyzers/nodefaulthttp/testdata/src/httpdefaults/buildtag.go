//go:build go1.1

package httpdefaults

import "net/http"

func BuildTaggedDefault() *http.Client {
	return http.DefaultClient // want `http/no-default-client: http.DefaultClient has shared implicit lifecycle and timeout policy`
}
