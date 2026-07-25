package httpdefaults

import . "net/http"

func DotImport() {
	_ = DefaultClient // want `http/no-default-client: http.DefaultClient has shared implicit lifecycle and timeout policy`
	_ = DefaultTransport.(*Transport).Clone()
}
