package httpother

import "net/http"

func Explicit(client *http.Client) *http.Client {
	return client
}
