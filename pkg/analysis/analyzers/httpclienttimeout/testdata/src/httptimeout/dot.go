//go:build darwin || linux || windows

package httptimeout

import . "net/http"

func dotImport() {
	_ = Client{} // want `http/client-timeout: http.Client must declare a positive Timeout`
}
